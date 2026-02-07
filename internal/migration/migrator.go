package migration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"firebaseToPostgre/internal/database"
	"firebaseToPostgre/internal/firebase"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrator performs Firebase -> PostgreSQL migrations.
type Migrator struct {
	progress *Progress
	logger   *log.Logger
}

// NewMigrator creates a new migrator.
func NewMigrator(progress *Progress, logger *log.Logger) *Migrator {
	if logger == nil {
		logger = log.Default()
	}
	return &Migrator{
		progress: progress,
		logger:   logger,
	}
}

// Run executes the migration.
func (m *Migrator) Run(ctx context.Context, req Request) error {
	if len(req.Mappings) == 0 {
		return errors.New("mappings are required")
	}
	if req.Options.BatchSize <= 0 {
		req.Options.BatchSize = 500
	}
	if req.Options.Workers <= 0 {
		req.Options.Workers = 4
	}
	if req.Options.ErrorStrategy == "" {
		req.Options.ErrorStrategy = "continue"
	}

	src, err := firebase.NewSource(req.Firebase.SourceType, req.Firebase.ProjectID, req.Firebase.CredentialsJSON)
	if err != nil {
		return err
	}
	if err := src.Open(ctx); err != nil {
		return fmt.Errorf("open firebase: %w", err)
	}
	defer src.Close()

	var pool *pgxpool.Pool
	if !req.Options.DryRun {
		pool, err = database.Connect(ctx, req.Destination)
		if err != nil {
			return fmt.Errorf("connect postgres: %w", err)
		}
		defer pool.Close()
	}

	if !req.Options.DryRun {
		if err := dropPublicObjects(ctx, pool); err != nil {
			return fmt.Errorf("drop public objects: %w", err)
		}
	}

	m.progress.SetPhase("running")

	for _, mapping := range req.Mappings {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := m.runMapping(ctx, src, pool, mapping, req.Options); err != nil {
			if req.Options.ErrorStrategy == "stop" {
				return err
			}
			m.logger.Printf("mapping %s failed: %v", mapping.Name, err)
		}
	}

	return nil
}

func (m *Migrator) runMapping(ctx context.Context, src firebase.Source, pool *pgxpool.Pool, mapping CollectionMapping, opt Options) error {
	m.progress.UpdateTable(mapping.Name, func(ts *TableStatus) {
		ts.Name = mapping.Name
		ts.SourceCollection = mapping.SourceCollection
		ts.TargetTable = mapping.TargetTable
		ts.Phase = "preparing"
	})

	total, err := src.EstimateCount(ctx, mapping.SourceCollection)
	if err != nil {
		m.logger.Printf("estimate count failed for %s: %v", mapping.Name, err)
		m.progress.Log("estimate count failed: " + err.Error())
	}
	m.progress.UpdateTable(mapping.Name, func(ts *TableStatus) {
		ts.Total = total
	})

	if mapping.CreateTable && !opt.DryRun {
		if err := ensureTable(ctx, pool, mapping); err != nil {
			m.progress.UpdateTable(mapping.Name, func(ts *TableStatus) {
				ts.Phase = "error"
				ts.Errors++
				ts.Message = err.Error()
			})
			m.progress.AddFailed(mapping.Name, err.Error())
			return err
		}
	}

	pipelineRef := &atomic.Value{}
	pipelineRef.Store(newPipeline(mapping.Columns))
	targetColumns := pipelineRef.Load().(*pipeline).TargetColumns()
	var inferOnce sync.Once

	batchCh := make(chan []firebase.Record, opt.Workers)
	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	var readCount int64
	var writtenCount int64
	for i := 0; i < opt.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range batchCh {
				select {
				case <-ctx.Done():
					return
				default:
				}

				rows, skipped, convErrs := pipelineRef.Load().(*pipeline).TransformBatch(batch)
				if skipped > 0 {
					m.progress.UpdateTable(mapping.Name, func(ts *TableStatus) {
						ts.Skipped += skipped
						ts.Phase = "transforming"
					})
				}
				if convErrs > 0 {
					m.progress.UpdateTable(mapping.Name, func(ts *TableStatus) {
						ts.Errors += convErrs
						ts.Phase = "transforming"
					})
					inferOnce.Do(func() {
						newCols, err := inferColumns(ctx, src, mapping.SourceCollection, 20)
						if err != nil {
							m.logger.Printf("infer columns failed for %s: %v", mapping.Name, err)
							return
						}
						merged := mergeColumns(mapping.Columns, newCols)
						pipelineRef.Store(newPipeline(merged))
						targetColumns = pipelineRef.Load().(*pipeline).TargetColumns()
						m.progress.Log("re-inferred column types for " + mapping.Name)
					})
					if opt.ErrorStrategy == "stop" {
						select {
						case errCh <- fmt.Errorf("transform errors in %s", mapping.Name):
						default:
						}
						cancel()
						return
					}
				}

				if opt.DryRun {
					m.progress.UpdateTable(mapping.Name, func(ts *TableStatus) {
						ts.Written += int64(len(rows))
						ts.Phase = "dry-run"
					})
					atomic.AddInt64(&writtenCount, int64(len(rows)))
					continue
				}

				if len(rows) == 0 {
					continue
				}

				written, err := writeBatch(ctx, pool, mapping.TargetTable, targetColumns, rows)
				if err != nil {
					m.progress.UpdateTable(mapping.Name, func(ts *TableStatus) {
						ts.Errors++
						ts.Phase = "writing"
						ts.Message = err.Error()
					})
					if opt.ErrorStrategy == "stop" {
						select {
						case errCh <- err:
						default:
						}
						cancel()
						return
					}
					// エラー時でもバッチは消費済みとして次へ進む
					continue
				}

				m.progress.UpdateTable(mapping.Name, func(ts *TableStatus) {
					ts.Written += written
					ts.Phase = "writing"
				})
				atomic.AddInt64(&writtenCount, written)
			}
		}()
	}

	var cursor *firebase.Cursor
	consecutiveReadErrors := 0
	for {
		select {
		case <-ctx.Done():
			close(batchCh)
			wg.Wait()
			return ctx.Err()
		default:
		}

		records, newCursor, err := src.ReadBatch(ctx, mapping.SourceCollection, cursor, opt.BatchSize)
		if err != nil {
			consecutiveReadErrors++
			m.progress.UpdateTable(mapping.Name, func(ts *TableStatus) {
				ts.Errors++
				ts.Phase = "reading"
				ts.Message = err.Error()
			})
			if opt.ErrorStrategy == "stop" {
				close(batchCh)
				wg.Wait()
				return err
			}
			if atomic.LoadInt64(&readCount) > 0 && atomic.LoadInt64(&readCount) == atomic.LoadInt64(&writtenCount) {
				close(batchCh)
				wg.Wait()
				return nil
			}
			if consecutiveReadErrors >= 3 {
				close(batchCh)
				wg.Wait()
				return fmt.Errorf("read batch failed repeatedly: %w", err)
			}
			continue
		}
		consecutiveReadErrors = 0

		if len(records) == 0 {
			break
		}

		cursor = newCursor
		m.progress.UpdateTable(mapping.Name, func(ts *TableStatus) {
			ts.Read += int64(len(records))
			ts.Phase = "reading"
		})
		atomic.AddInt64(&readCount, int64(len(records)))

		select {
		case batchCh <- records:
		case err := <-errCh:
			close(batchCh)
			wg.Wait()
			return err
		case <-ctx.Done():
			close(batchCh)
			wg.Wait()
			return ctx.Err()
		}
	}

	close(batchCh)
	wg.Wait()

	select {
	case err := <-errCh:
		m.progress.AddFailed(mapping.Name, err.Error())
		return err
	default:
	}

	m.progress.UpdateTable(mapping.Name, func(ts *TableStatus) {
		ts.Phase = "done"
	})
	return nil
}

func ensureTable(ctx context.Context, pool *pgxpool.Pool, mapping CollectionMapping) error {
	if pool == nil {
		return errors.New("postgres connection is required")
	}
	ddl, err := buildDDL(mapping)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, ddl)
	if err != nil {
		return fmt.Errorf("create table %s: %w", mapping.TargetTable, err)
	}
	return nil
}

func buildDDL(mapping CollectionMapping) (string, error) {
	if mapping.TargetTable == "" {
		return "", errors.New("target table is required")
	}
	if len(mapping.Columns) == 0 {
		return "", errors.New("columns are required")
	}

	var b strings.Builder
	b.WriteString("CREATE TABLE IF NOT EXISTS ")
	b.WriteString(quoteComposite(mapping.TargetTable))
	b.WriteString(" (\n")

	var pks []string
	for i, col := range mapping.Columns {
		b.WriteString("  ")
		b.WriteString(database.QuoteIdent(col.Target))
		b.WriteString(" ")
		b.WriteString(col.Type)
		if !col.Nullable {
			b.WriteString(" NOT NULL")
		}
		if col.PrimaryKey {
			pks = append(pks, database.QuoteIdent(col.Target))
		}
		if i < len(mapping.Columns)-1 || len(pks) > 0 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}

	if len(pks) > 0 {
		b.WriteString("  PRIMARY KEY (")
		b.WriteString(strings.Join(pks, ", "))
		b.WriteString(")\n")
	}

	b.WriteString(");")
	return b.String(), nil
}

func quoteComposite(name string) string {
	parts := strings.Split(name, ".")
	quoted := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		quoted = append(quoted, database.QuoteIdent(p))
	}
	return strings.Join(quoted, ".")
}

func writeBatch(ctx context.Context, pool *pgxpool.Pool, table string, columns []string, rows [][]interface{}) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	identifier := pgx.Identifier(splitIdent(table))
	count, err := pool.CopyFrom(ctx, identifier, columns, pgx.CopyFromRows(rows))
	if err == nil {
		return count, nil
	}
	return insertRowByRow(ctx, pool, table, columns, rows)
}

func insertRowByRow(ctx context.Context, pool *pgxpool.Pool, table string, columns []string, rows [][]interface{}) (int64, error) {
	quotedCols := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	for i, c := range columns {
		quotedCols[i] = database.QuoteIdent(c)
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}

	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT DO NOTHING",
		quoteComposite(table),
		strings.Join(quotedCols, ", "),
		strings.Join(placeholders, ", "),
	)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var written int64
	for _, row := range rows {
		tag, err := tx.Exec(ctx, sql, row...)
		if err != nil {
			continue
		}
		written += tag.RowsAffected()
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit tx: %w", err)
	}
	return written, nil
}

func splitIdent(name string) []string {
	parts := strings.Split(name, ".")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return []string{name}
	}
	return out
}

func dropPublicObjects(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("postgres connection is required")
	}
	sql := `
DO $$
DECLARE
  r RECORD;
BEGIN
  FOR r IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' LOOP
    EXECUTE 'DROP TABLE IF EXISTS public.' || quote_ident(r.tablename) || ' CASCADE';
  END LOOP;
  FOR r IN SELECT sequencename FROM pg_sequences WHERE schemaname = 'public' LOOP
    EXECUTE 'DROP SEQUENCE IF EXISTS public.' || quote_ident(r.sequencename) || ' CASCADE';
  END LOOP;
  FOR r IN SELECT table_name FROM information_schema.views WHERE table_schema = 'public' LOOP
    EXECUTE 'DROP VIEW IF EXISTS public.' || quote_ident(r.table_name) || ' CASCADE';
  END LOOP;
  FOR r IN SELECT matviewname FROM pg_matviews WHERE schemaname = 'public' LOOP
    EXECUTE 'DROP MATERIALIZED VIEW IF EXISTS public.' || quote_ident(r.matviewname) || ' CASCADE';
  END LOOP;
END $$;
`
	_, err := pool.Exec(ctx, sql)
	return err
}

// ---------------------------------------------------------------------------
// Transformation helpers
// ---------------------------------------------------------------------------

type pipeline struct {
	columns []ColumnMapping
}

func newPipeline(columns []ColumnMapping) *pipeline {
	return &pipeline{columns: columns}
}

func (p *pipeline) TargetColumns() []string {
	cols := make([]string, len(p.columns))
	for i, c := range p.columns {
		cols[i] = c.Target
	}
	return cols
}

func (p *pipeline) Transform(record firebase.Record) ([]interface{}, error) {
	flat := FlattenMap(record.Data, "_")
	row := make([]interface{}, len(p.columns))

	for i, col := range p.columns {
		val, ok := flat[col.Source]
		if col.Source == "_key" || col.Source == "_id" {
			if record.Key != "" {
				val = record.Key
				ok = true
			} else if v, exists := record.Data["_id"]; exists {
				val = v
				ok = true
			}
		}

		if !ok || val == nil {
			if col.Nullable {
				row[i] = nil
				continue
			}
			return nil, fmt.Errorf("required field %q not found in record %q", col.Source, record.Key)
		}

		converted, err := convertValue(val, col.Type)
		if err != nil {
			if col.Nullable {
				row[i] = nil
				continue
			}
			return nil, fmt.Errorf("convert field %q to %s: %w", col.Source, col.Type, err)
		}
		row[i] = converted
	}
	return row, nil
}

func (p *pipeline) TransformBatch(records []firebase.Record) ([][]interface{}, int64, int64) {
	rows := make([][]interface{}, 0, len(records))
	var skipped int64
	var errors int64
	for _, rec := range records {
		row, err := p.Transform(rec)
		if err != nil {
			skipped++
			errors++
			continue
		}
		rows = append(rows, row)
	}
	return rows, skipped, errors
}

// FlattenMap recursively flattens a nested map using sep as the key separator.
func FlattenMap(data map[string]interface{}, sep string) map[string]interface{} {
	result := make(map[string]interface{})
	flattenRecursive(data, "", sep, result)
	return result
}

func flattenRecursive(data map[string]interface{}, prefix, sep string, result map[string]interface{}) {
	for k, v := range data {
		key := k
		if prefix != "" {
			key = prefix + sep + k
		}

		switch val := v.(type) {
		case map[string]interface{}:
			flattenRecursive(val, key, sep, result)
			result[key] = v
		default:
			result[key] = val
		}
	}
}

// InferPGType infers a PostgreSQL type from a Go value.
func InferPGType(value interface{}) string {
	if value == nil {
		return "TEXT"
	}
	switch v := value.(type) {
	case bool:
		return "BOOLEAN"
	case float64:
		if v == float64(int64(v)) {
			return "BIGINT"
		}
		return "DOUBLE PRECISION"
	case float32:
		return "REAL"
	case int, int32, int64:
		return "BIGINT"
	case string:
		if _, err := time.Parse(time.RFC3339, v); err == nil {
			return "TIMESTAMPTZ"
		}
		if len(v) > 255 {
			return "TEXT"
		}
		return "TEXT"
	case []interface{}:
		return "JSONB"
	case map[string]interface{}:
		return "JSONB"
	default:
		return "TEXT"
	}
}

func convertValue(val interface{}, pgType string) (interface{}, error) {
	upper := strings.ToUpper(pgType)

	switch {
	case upper == "BOOLEAN" || upper == "BOOL":
		return toBool(val)
	case upper == "BIGINT" || upper == "INT" || upper == "INTEGER" || upper == "INT4" || upper == "INT8" || upper == "SMALLINT":
		return toInt64(val)
	case upper == "REAL" || upper == "FLOAT4":
		return toFloat64(val)
	case upper == "DOUBLE PRECISION" || upper == "FLOAT8" || upper == "NUMERIC" || upper == "DECIMAL":
		return toFloat64(val)
	case upper == "TEXT" || upper == "VARCHAR" || strings.HasPrefix(upper, "VARCHAR(") || strings.HasPrefix(upper, "CHAR("):
		return toString(val), nil
	case upper == "TIMESTAMPTZ" || upper == "TIMESTAMP" || upper == "TIMESTAMP WITH TIME ZONE" || upper == "TIMESTAMP WITHOUT TIME ZONE":
		return toTimestamp(val)
	case upper == "JSONB" || upper == "JSON":
		return toJSON(val)
	case upper == "UUID":
		return toString(val), nil
	case upper == "BYTEA":
		return toString(val), nil
	default:
		return toString(val), nil
	}
}

func toBool(val interface{}) (bool, error) {
	switch v := val.(type) {
	case bool:
		return v, nil
	case float64:
		return v != 0, nil
	case string:
		switch strings.ToLower(v) {
		case "true", "1", "yes":
			return true, nil
		case "false", "0", "no", "":
			return false, nil
		}
	}
	return false, fmt.Errorf("cannot convert %T to bool", val)
}

func toInt64(val interface{}) (int64, error) {
	switch v := val.(type) {
	case float64:
		return int64(v), nil
	case float32:
		return int64(v), nil
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case int32:
		return int64(v), nil
	case string:
		var i int64
		_, err := fmt.Sscanf(v, "%d", &i)
		return i, err
	case bool:
		if v {
			return 1, nil
		}
		return 0, nil
	}
	return 0, fmt.Errorf("cannot convert %T to int64", val)
}

func toFloat64(val interface{}) (float64, error) {
	switch v := val.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		var f float64
		_, err := fmt.Sscanf(v, "%f", &f)
		return f, err
	}
	return 0, fmt.Errorf("cannot convert %T to float64", val)
}

func toString(val interface{}) string {
	switch v := val.(type) {
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toTimestamp(val interface{}) (interface{}, error) {
	switch v := val.(type) {
	case string:
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t, nil
		}
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return t, nil
		}
		if t, err := time.Parse("2006-01-02 15:04:05", v); err == nil {
			return t, nil
		}
		if t, err := time.Parse("2006-01-02", v); err == nil {
			return t, nil
		}
		return v, nil
	case float64:
		return time.Unix(int64(v), 0), nil
	case int64:
		return time.Unix(v, 0), nil
	case map[string]interface{}:
		if sec, ok := v["_seconds"]; ok {
			s, _ := toInt64(sec)
			var ns int64
			if nsVal, ok := v["_nanoseconds"]; ok {
				ns, _ = toInt64(nsVal)
			}
			return time.Unix(s, ns), nil
		}
	}
	return nil, fmt.Errorf("cannot convert %T to timestamp", val)
}

func toJSON(val interface{}) (string, error) {
	b, err := json.Marshal(val)
	if err != nil {
		return "", fmt.Errorf("marshal to JSON: %w", err)
	}
	return string(b), nil
}

func inferColumns(ctx context.Context, src firebase.Source, collection string, sampleSize int) ([]ColumnMapping, error) {
	if sampleSize <= 0 {
		sampleSize = 20
	}
	records, err := src.SampleRecords(ctx, collection, sampleSize)
	if err != nil {
		return nil, err
	}
	fieldTypes := make(map[string]string)
	hasID := false
	for _, rec := range records {
		flat := FlattenMap(rec.Data, "_")
		for k, v := range flat {
			if k == "_id" || k == "id" {
				hasID = true
				continue
			}
			if _, exists := fieldTypes[k]; !exists {
				fieldTypes[k] = InferPGType(v)
			}
		}
	}
	columns := make([]ColumnMapping, 0, len(fieldTypes)+1)
	if !hasID {
		columns = append(columns, ColumnMapping{
			Source:     "_id",
			Target:     "id",
			Type:       "TEXT",
			PrimaryKey: true,
			Nullable:   false,
		})
	}
	for field, pgType := range fieldTypes {
		columns = append(columns, ColumnMapping{
			Source:     field,
			Target:     field,
			Type:       pgType,
			PrimaryKey: false,
			Nullable:   true,
		})
	}
	return columns, nil
}

func mergeColumns(base, inferred []ColumnMapping) []ColumnMapping {
	inferMap := make(map[string]ColumnMapping, len(inferred))
	for _, col := range inferred {
		inferMap[col.Source] = col
	}
	out := make([]ColumnMapping, 0, len(base))
	for _, col := range base {
		if inf, ok := inferMap[col.Source]; ok {
			col.Type = inf.Type
		}
		out = append(out, col)
	}
	return out
}
