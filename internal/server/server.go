package server

import (
	"net/http"

	"firebaseToPostgre/internal/migration"
	"firebaseToPostgre/internal/websocket"
)

// Server is the HTTP server for the migration tool.
type Server struct {
	hub     *websocket.Hub
	manager *migration.Manager
}

// New creates a new Server.
func New(hub *websocket.Hub) *Server {
	mgr := migration.NewManager(hub)
	return &Server{
		hub:     hub,
		manager: mgr,
	}
}

// Routes returns the HTTP handler with routes registered.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/test-firebase", s.handleTestFirebase)
	mux.HandleFunc("/api/test-postgres", s.handleTestPostgres)
	mux.HandleFunc("/api/collections", s.handleCollections)
	mux.HandleFunc("/api/collections/tree", s.handleCollectionsTree)
	mux.HandleFunc("/api/mappings/ai", s.handleAIMappings)
	mux.HandleFunc("/api/migrate", s.handleMigrate)
	mux.HandleFunc("/api/migrate/cancel", s.handleCancel)
	mux.HandleFunc("/api/migrate/status", s.handleStatus)
	mux.HandleFunc("/api/preview", s.handlePreview)
	mux.HandleFunc("/api/schema/infer", s.handleInfer)
	mux.HandleFunc("/ws/progress", s.handleWS)

	mux.Handle("/", http.FileServer(http.Dir("web")))
	return mux
}
