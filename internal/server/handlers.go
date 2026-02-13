package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"firebaseToPostgre/internal/ai"
	"firebaseToPostgre/internal/database"
	"firebaseToPostgre/internal/firebase"
	"firebaseToPostgre/internal/migration"
	ws "firebaseToPostgre/internal/websocket"

	gws "github.com/gorilla/websocket"
)

var upgrader = gws.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (s *Server) handleTestFirebase(w http.ResponseWriter, r *http.Request) {
	var req firebase.ConnInfo
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	src, err := firebase.NewSource(req.SourceType, req.ProjectID, req.CredentialsJSON)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := src.Open(ctx); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}
	defer src.Close()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Firebase connection successful",
	})
}

func (s *Server) handleTestPostgres(w http.ResponseWriter, r *http.Request) {
	var req database.ConnInfo
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	pool, err := database.Connect(ctx, req)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}
	defer pool.Close()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "PostgreSQL connection successful",
	})
}

func (s *Server) handleCollections(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Firebase firebase.ConnInfo `json:"firebase"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	collections, err := firebase.ListCollections(ctx, req.Firebase)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "list collections: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"collections": collections,
	})
}

func (s *Server) handleCollectionsTree(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Firebase   firebase.ConnInfo `json:"firebase"`
		SampleSize int               `json:"sampleSize"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.SampleSize <= 0 {
		req.SampleSize = 20
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	log.Printf("collections tree list collections start: project=%s source=%s", req.Firebase.ProjectID, req.Firebase.SourceType)
	collections, err := firebase.ListCollections(ctx, req.Firebase)
	if err != nil {
		log.Printf("collections tree list collections failed: project=%s source=%s err=%v", req.Firebase.ProjectID, req.Firebase.SourceType, err)
		jsonError(w, http.StatusInternalServerError, "list collections: "+err.Error())
		return
	}
	log.Printf("collections tree list collections done: project=%s count=%d", req.Firebase.ProjectID, len(collections))

	subMap := make(map[string][]string)
	for _, parent := range collections {
		start := time.Now()
		subs, err := firebase.ListSubcollections(ctx, req.Firebase, parent, req.SampleSize)
		if err != nil {
			log.Printf("collections tree list subcollections failed: project=%s collection=%s sample=%d elapsed=%s err=%v", req.Firebase.ProjectID, parent, req.SampleSize, time.Since(start), err)
			jsonError(w, http.StatusInternalServerError, "list subcollections: "+err.Error())
			return
		}
		log.Printf("collections tree list subcollections done: project=%s collection=%s sample=%d elapsed=%s subcount=%d", req.Firebase.ProjectID, parent, req.SampleSize, time.Since(start), len(subs))
		if len(subs) > 0 {
			subMap[parent] = subs
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"collections":    collections,
		"subcollections": subMap,
	})
}

func (s *Server) handleAIMappings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Firebase       firebase.ConnInfo `json:"firebase"`
		SampleSize     int               `json:"sampleSize"`
		MaxCollections int               `json:"maxCollections"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.SampleSize <= 0 {
		req.SampleSize = 5
	}
	if req.MaxCollections <= 0 {
		req.MaxCollections = 20
	}

	totalTimeout := durationFromEnv("AI_MAPPINGS_TIMEOUT_SEC", 300*time.Second)
	ctx, cancel := context.WithTimeout(r.Context(), totalTimeout)
	defer cancel()

	log.Printf("ai mappings list collections start: project=%s source=%s", req.Firebase.ProjectID, req.Firebase.SourceType)
	collections, err := firebase.ListCollections(ctx, req.Firebase)
	if err != nil {
		log.Printf("ai mappings list collections failed: project=%s source=%s err=%v", req.Firebase.ProjectID, req.Firebase.SourceType, err)
		jsonError(w, http.StatusInternalServerError, "list collections: "+err.Error())
		return
	}
	if len(collections) > req.MaxCollections {
		collections = collections[:req.MaxCollections]
	}
	log.Printf("ai mappings list collections done: project=%s count=%d max=%d", req.Firebase.ProjectID, len(collections), req.MaxCollections)

	subMap := make(map[string][]string)
	for _, parent := range collections {
		start := time.Now()
		subs, err := firebase.ListSubcollections(ctx, req.Firebase, parent, 20)
		if err != nil {
			log.Printf("ai mappings list subcollections failed: project=%s collection=%s sample=%d elapsed=%s err=%v", req.Firebase.ProjectID, parent, 20, time.Since(start), err)
			jsonError(w, http.StatusInternalServerError, "list subcollections: "+err.Error())
			return
		}
		log.Printf("ai mappings list subcollections done: project=%s collection=%s sample=%d elapsed=%s subcount=%d", req.Firebase.ProjectID, parent, 20, time.Since(start), len(subs))
		if len(subs) > 0 {
			subMap[parent] = subs
		}
	}

	src, err := firebase.NewSource(req.Firebase.SourceType, req.Firebase.ProjectID, req.Firebase.CredentialsJSON)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "create source: "+err.Error())
		return
	}
	if err := src.Open(ctx); err != nil {
		jsonError(w, http.StatusInternalServerError, "open source: "+err.Error())
		return
	}
	defer src.Close()

	hints := ai.MappingHints{
		ProjectID:      req.Firebase.ProjectID,
		Collections:    collections,
		Subcollections: subMap,
	}

	for _, col := range collections {
		records, err := src.SampleRecords(ctx, col, req.SampleSize)
		if err != nil {
			continue
		}
		fieldTypes := make(map[string]string)
		for _, rec := range records {
			flat := migration.FlattenMap(rec.Data, "_")
			for k, v := range flat {
				if _, exists := fieldTypes[k]; !exists {
					fieldTypes[k] = migration.InferPGType(v)
				}
			}
		}
		hints.Hints = append(hints.Hints, ai.MappingHint{
			Collection:  col,
			TargetTable: "public." + col,
			Fields:      fieldTypes,
		})
	}

	for parent, subs := range subMap {
		for _, sub := range subs {
			group := parent + "/*/" + sub
			records, err := src.SampleRecords(ctx, group, req.SampleSize)
			if err != nil {
				continue
			}
			fieldTypes := make(map[string]string)
			for _, rec := range records {
				flat := migration.FlattenMap(rec.Data, "_")
				for k, v := range flat {
					if _, exists := fieldTypes[k]; !exists {
						fieldTypes[k] = migration.InferPGType(v)
					}
				}
			}
			hints.Hints = append(hints.Hints, ai.MappingHint{
				Collection:       group,
				TargetTable:      "public." + parent + "_" + sub,
				ParentCollection: parent,
				Fields:           fieldTypes,
			})
		}
	}

	aiTimeout := durationFromEnv("OPENAI_TIMEOUT_SEC", 180*time.Second)
	aiCtx, aiCancel := context.WithTimeout(r.Context(), aiTimeout)
	defer aiCancel()
	log.Printf("ai mappings openai call start: project=%s timeout=%s", req.Firebase.ProjectID, aiTimeout)
	aiStart := time.Now()
	mappings, err := ai.GenerateMappings(aiCtx, ai.GenerateInput{Hints: hints})
	if err != nil {
		log.Printf("ai mappings openai call failed: project=%s elapsed=%s err=%v", req.Firebase.ProjectID, time.Since(aiStart), err)
		jsonError(w, http.StatusInternalServerError, "openai: "+err.Error())
		return
	}
	log.Printf("ai mappings openai call done: project=%s elapsed=%s", req.Firebase.ProjectID, time.Since(aiStart))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"mappings": mappings,
	})
}

func durationFromEnv(name string, fallback time.Duration) time.Duration {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return time.Duration(n) * time.Second
}

func (s *Server) handleMigrate(w http.ResponseWriter, r *http.Request) {
	var req migration.Request
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if err := s.manager.Start(req); err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "started",
		"message": "migration started",
	})
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	s.manager.Cancel()
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "cancelling",
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.manager.Status())
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Firebase   firebase.ConnInfo `json:"firebase"`
		Collection string            `json:"collection"`
		Limit      int               `json:"limit"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Collection == "" {
		jsonError(w, http.StatusBadRequest, "collection is required")
		return
	}
	if req.Limit <= 0 {
		req.Limit = 5
	}
	if req.Limit > 100 {
		req.Limit = 100
	}

	src, err := firebase.NewSource(req.Firebase.SourceType, req.Firebase.ProjectID, req.Firebase.CredentialsJSON)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	if err := src.Open(ctx); err != nil {
		jsonError(w, http.StatusInternalServerError, "open source: "+err.Error())
		return
	}
	defer src.Close()

	records, err := src.SampleRecords(ctx, req.Collection, req.Limit)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "read records: "+err.Error())
		return
	}

	items := make([]map[string]interface{}, 0, len(records))
	for _, rec := range records {
		items = append(items, rec.Data)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"collection": req.Collection,
		"count":      len(items),
		"records":    items,
	})
}

func (s *Server) handleInfer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Firebase   firebase.ConnInfo `json:"firebase"`
		Collection string            `json:"collection"`
		SampleSize int               `json:"sampleSize"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Collection == "" {
		jsonError(w, http.StatusBadRequest, "collection is required")
		return
	}
	if req.SampleSize <= 0 {
		req.SampleSize = 10
	}

	src, err := firebase.NewSource(req.Firebase.SourceType, req.Firebase.ProjectID, req.Firebase.CredentialsJSON)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	if err := src.Open(ctx); err != nil {
		jsonError(w, http.StatusInternalServerError, "open source: "+err.Error())
		return
	}
	defer src.Close()

	records, err := src.SampleRecords(ctx, req.Collection, req.SampleSize)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "read samples: "+err.Error())
		return
	}

	fieldTypes := make(map[string]string)
	hasID := false
	for _, rec := range records {
		flat := migration.FlattenMap(rec.Data, "_")
		for k, v := range flat {
			if k == "_id" || k == "id" {
				hasID = true
				continue
			}
			if _, exists := fieldTypes[k]; !exists {
				fieldTypes[k] = migration.InferPGType(v)
			}
		}
	}

	columns := make([]migration.ColumnMapping, 0, len(fieldTypes))
	for field, pgType := range fieldTypes {
		col := migration.ColumnMapping{
			Source:     field,
			Target:     field,
			Type:       pgType,
			PrimaryKey: false,
			Nullable:   true,
		}
		columns = append(columns, col)
	}
	if !hasID {
		columns = append([]migration.ColumnMapping{
			{
				Source:     "_id",
				Target:     "id",
				Type:       "TEXT",
				PrimaryKey: true,
				Nullable:   false,
			},
		}, columns...)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"collection": req.Collection,
		"fields":     fieldTypes,
		"columns":    columns,
	})
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	client := ws.NewClient(s.hub, conn)
	s.hub.Register(client)

	go client.WritePump()
	client.ReadPump()
}

// writeJSON sends a JSON response.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// readJSON decodes a JSON request body.
func readJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// jsonError sends a JSON error response.
func jsonError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
