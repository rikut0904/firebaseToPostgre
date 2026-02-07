package firebase

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/db"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// ConnInfo holds Firebase connection settings.
type ConnInfo struct {
	SourceType      string `json:"sourceType"`
	ProjectID       string `json:"projectId"`
	CredentialsJSON string `json:"credentialsJSON"`
}

// Record represents a single document/node read from Firebase.
type Record struct {
	Key  string                 `json:"key"`
	Data map[string]interface{} `json:"data"`
}

// Cursor tracks the pagination position for batch reads.
type Cursor struct {
	LastKey string `json:"lastKey"`
	Offset  int    `json:"offset"`
}

// Source is the interface for reading data from Firebase.
type Source interface {
	Open(ctx context.Context) error
	Close() error
	EstimateCount(ctx context.Context, collection string) (int64, error)
	ReadBatch(ctx context.Context, collection string, cursor *Cursor, batchSize int) ([]Record, *Cursor, error)
	SampleRecords(ctx context.Context, collection string, limit int) ([]Record, error)
}

// ListCollections lists top-level collections for the given connection.
func ListCollections(ctx context.Context, conn ConnInfo) ([]string, error) {
	switch conn.SourceType {
	case "firestore":
		return listFirestoreCollections(ctx, conn)
	case "rtdb":
		return listRTDBCollections(ctx, conn)
	default:
		return nil, fmt.Errorf("unsupported source type: %s", conn.SourceType)
	}
}

// NewSource creates a Source for the given type ("firestore" or "rtdb").
func NewSource(sourceType, projectID, credentialsJSON string) (Source, error) {
	switch sourceType {
	case "firestore":
		return &firestoreSource{
			projectID:       projectID,
			credentialsJSON: credentialsJSON,
		}, nil
	case "rtdb":
		return &rtdbSource{
			projectID:       projectID,
			credentialsJSON: credentialsJSON,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported source type: %s", sourceType)
	}
}

// ---------------------------------------------------------------------------
// Firestore implementation
// ---------------------------------------------------------------------------

type firestoreSource struct {
	projectID       string
	credentialsJSON string
	client          *firestore.Client
}

func (fs *firestoreSource) Open(ctx context.Context) error {
	opt := option.WithCredentialsJSON([]byte(fs.credentialsJSON))
	client, err := firestore.NewClient(ctx, fs.projectID, opt)
	if err != nil {
		return fmt.Errorf("firestore client: %w", err)
	}
	fs.client = client
	return nil
}

func (fs *firestoreSource) Close() error {
	if fs.client != nil {
		return fs.client.Close()
	}
	return nil
}

func (fs *firestoreSource) EstimateCount(ctx context.Context, collection string) (int64, error) {
	if fs.client == nil {
		return 0, fmt.Errorf("firestore client is not initialized")
	}
	if _, group, ok := parseCollectionGroup(collection); ok {
		agg := fs.client.CollectionGroup(group).NewAggregationQuery().WithCount("count")
		results, err := agg.Get(ctx)
		if err != nil {
			return 0, fmt.Errorf("count aggregation (collection group %s): %w", group, err)
		}
		return readCount(results)
	}

	agg := fs.client.Collection(collection).NewAggregationQuery().WithCount("count")
	results, err := agg.Get(ctx)
	if err != nil {
		return 0, fmt.Errorf("count aggregation: %w", err)
	}
	return readCount(results)
}

func (fs *firestoreSource) ReadBatch(ctx context.Context, collection string, cursor *Cursor, batchSize int) ([]Record, *Cursor, error) {
	if fs.client == nil {
		return nil, nil, fmt.Errorf("firestore client is not initialized")
	}
	parentName, groupName, isGroup := parseCollectionGroup(collection)
	var query firestore.Query
	if isGroup {
		query = fs.client.CollectionGroup(groupName).OrderBy(firestore.DocumentID, firestore.Asc).Limit(batchSize)
		if cursor != nil && cursor.LastKey != "" {
			query = fs.client.CollectionGroup(groupName).OrderBy(firestore.DocumentID, firestore.Asc).StartAfter(fs.client.Doc(cursor.LastKey)).Limit(batchSize)
		}
	} else {
		query = fs.client.Collection(collection).OrderBy(firestore.DocumentID, firestore.Asc).Limit(batchSize)
		if cursor != nil && cursor.LastKey != "" {
			query = fs.client.Collection(collection).OrderBy(firestore.DocumentID, firestore.Asc).StartAfter(cursor.LastKey).Limit(batchSize)
		}
	}

	iter := query.Documents(ctx)
	defer iter.Stop()

	var records []Record
	var lastKey string
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read document: %w", err)
		}
		data := doc.Data()
		data["_id"] = doc.Ref.ID
		if isGroup {
			if pid := parentIDFromPath(doc.Ref.Path, parentName); pid != "" {
				data[parentName+"_id"] = pid
			}
		}
		records = append(records, Record{
			Key:  doc.Ref.ID,
			Data: data,
		})
		if isGroup {
			lastKey = doc.Ref.Path
		} else {
			lastKey = doc.Ref.ID
		}
	}

	if len(records) == 0 {
		return nil, nil, nil
	}
	return records, &Cursor{LastKey: lastKey}, nil
}

func (fs *firestoreSource) SampleRecords(ctx context.Context, collection string, limit int) ([]Record, error) {
	if fs.client == nil {
		return nil, fmt.Errorf("firestore client is not initialized")
	}
	parentName, groupName, isGroup := parseCollectionGroup(collection)
	var iter *firestore.DocumentIterator
	if isGroup {
		iter = fs.client.CollectionGroup(groupName).Limit(limit).Documents(ctx)
	} else {
		iter = fs.client.Collection(collection).Limit(limit).Documents(ctx)
	}
	defer iter.Stop()

	var records []Record
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("sample document: %w", err)
		}
		data := doc.Data()
		data["_id"] = doc.Ref.ID
		if isGroup {
			if pid := parentIDFromPath(doc.Ref.Path, parentName); pid != "" {
				data[parentName+"_id"] = pid
			}
		}
		records = append(records, Record{
			Key:  doc.Ref.ID,
			Data: data,
		})
	}
	return records, nil
}

func listFirestoreCollections(ctx context.Context, conn ConnInfo) ([]string, error) {
	opt := option.WithCredentialsJSON([]byte(conn.CredentialsJSON))
	client, err := firestore.NewClient(ctx, conn.ProjectID, opt)
	if err != nil {
		return nil, fmt.Errorf("firestore client: %w", err)
	}
	defer client.Close()

	iter := client.Collections(ctx)
	var names []string
	for {
		col, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list collections: %w", err)
		}
		names = append(names, col.ID)
	}
	return names, nil
}

// ListSubcollections lists subcollections under a parent collection (Firestore only).
func ListSubcollections(ctx context.Context, conn ConnInfo, parentCollection string, sample int) ([]string, error) {
	if conn.SourceType != "firestore" {
		return []string{}, nil
	}
	if sample <= 0 {
		sample = 20
	}

	opt := option.WithCredentialsJSON([]byte(conn.CredentialsJSON))
	client, err := firestore.NewClient(ctx, conn.ProjectID, opt)
	if err != nil {
		return nil, fmt.Errorf("firestore client: %w", err)
	}
	defer client.Close()

	iter := client.Collection(parentCollection).Limit(sample).Documents(ctx)
	defer iter.Stop()

	seen := make(map[string]struct{})
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list documents: %w", err)
		}
		subIter := doc.Ref.Collections(ctx)
		for {
			sub, err := subIter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("list subcollections: %w", err)
			}
			seen[sub.ID] = struct{}{}
		}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// ---------------------------------------------------------------------------
// Realtime Database implementation
// ---------------------------------------------------------------------------

type rtdbSource struct {
	projectID       string
	credentialsJSON string
	client          *db.Client
}

func (rs *rtdbSource) Open(ctx context.Context) error {
	opt := option.WithCredentialsJSON([]byte(rs.credentialsJSON))
	app, err := firebase.NewApp(ctx, &firebase.Config{
		DatabaseURL: fmt.Sprintf("https://%s-default-rtdb.firebaseio.com", rs.projectID),
		ProjectID:   rs.projectID,
	}, opt)
	if err != nil {
		return fmt.Errorf("firebase app: %w", err)
	}
	client, err := app.Database(ctx)
	if err != nil {
		return fmt.Errorf("rtdb client: %w", err)
	}
	rs.client = client
	return nil
}

func (rs *rtdbSource) Close() error {
	return nil
}

func (rs *rtdbSource) EstimateCount(ctx context.Context, collection string) (int64, error) {
	if _, _, ok := parseCollectionGroup(collection); ok {
		return 0, fmt.Errorf("collection group is not supported for rtdb")
	}
	url := fmt.Sprintf("https://%s-default-rtdb.firebaseio.com/%s.json?shallow=true", rs.projectID, collection)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	var keys map[string]interface{}
	if err := json.Unmarshal(body, &keys); err != nil {
		return 0, nil
	}
	return int64(len(keys)), nil
}

func (rs *rtdbSource) ReadBatch(ctx context.Context, collection string, cursor *Cursor, batchSize int) ([]Record, *Cursor, error) {
	if _, _, ok := parseCollectionGroup(collection); ok {
		return nil, nil, fmt.Errorf("collection group is not supported for rtdb")
	}
	ref := rs.client.NewRef(collection)
	query := ref.OrderByKey().LimitToFirst(batchSize + 1)
	if cursor != nil && cursor.LastKey != "" {
		query = ref.OrderByKey().StartAt(cursor.LastKey).LimitToFirst(batchSize + 1)
	}

	var raw map[string]interface{}
	if err := query.Get(ctx, &raw); err != nil {
		return nil, nil, fmt.Errorf("read rtdb: %w", err)
	}

	if len(raw) == 0 {
		return nil, nil, nil
	}

	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	if cursor != nil && cursor.LastKey != "" && len(keys) > 0 && keys[0] == cursor.LastKey {
		keys = keys[1:]
	}

	if len(keys) > batchSize {
		keys = keys[:batchSize]
	}

	records := make([]Record, len(keys))
	var wg sync.WaitGroup
	for i, k := range keys {
		i, k := i, k
		v := raw[k]
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := Record{Key: k}
			switch val := v.(type) {
			case map[string]interface{}:
				val["_id"] = k
				rec.Data = val
			default:
				rec.Data = map[string]interface{}{"_id": k, "_value": v}
			}
			records[i] = rec
		}()
	}
	wg.Wait()

	if len(records) == 0 {
		return nil, nil, nil
	}
	lastKey := records[len(records)-1].Key
	return records, &Cursor{LastKey: lastKey}, nil
}

func (rs *rtdbSource) SampleRecords(ctx context.Context, collection string, limit int) ([]Record, error) {
	if _, _, ok := parseCollectionGroup(collection); ok {
		return nil, fmt.Errorf("collection group is not supported for rtdb")
	}
	ref := rs.client.NewRef(collection)
	query := ref.OrderByKey().LimitToFirst(limit)

	var raw map[string]interface{}
	if err := query.Get(ctx, &raw); err != nil {
		return nil, fmt.Errorf("sample rtdb: %w", err)
	}

	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	records := make([]Record, 0, len(keys))
	for _, k := range keys {
		v := raw[k]
		rec := Record{Key: k}
		switch val := v.(type) {
		case map[string]interface{}:
			val["_id"] = k
			rec.Data = val
		default:
			rec.Data = map[string]interface{}{"_id": k, "_value": v}
		}
		records = append(records, rec)
	}
	return records, nil
}

func listRTDBCollections(ctx context.Context, conn ConnInfo) ([]string, error) {
	url := fmt.Sprintf("https://%s-default-rtdb.firebaseio.com/.json?shallow=true", conn.ProjectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var keys map[string]interface{}
	if err := json.Unmarshal(body, &keys); err != nil {
		return nil, nil
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	return names, nil
}

func parseCollectionGroup(collection string) (parentName string, groupName string, ok bool) {
	parts := strings.Split(collection, "/*/")
	if len(parts) != 2 {
		return "", "", false
	}
	parent := strings.TrimSpace(parts[0])
	group := strings.TrimSpace(parts[1])
	if parent == "" || group == "" {
		return "", "", false
	}
	return parent, group, true
}

func parentIDFromPath(path, parentName string) string {
	parts := strings.Split(path, "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == parentName && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func readCount(results map[string]interface{}) (int64, error) {
	count, ok := results["count"]
	if !ok {
		return 0, nil
	}
	switch v := count.(type) {
	case int64:
		return v, nil
	case float64:
		return int64(v), nil
	}
	return 0, nil
}

// CollectionPath joins path segments for nested collections.
func CollectionPath(parts ...string) string {
	return strings.Join(parts, "/")
}
