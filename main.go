package main

import (
	"log"
	"net/http"
	"os"

	"firebaseToPostgre/internal/env"
	"firebaseToPostgre/internal/server"
	"firebaseToPostgre/internal/websocket"
)

func main() {
	if err := env.LoadDotEnv(".env", ".env.local"); err != nil {
		log.Printf("dotenv load warning: %v", err)
	}

	addr := ":8082"
	if v := os.Getenv("ADDR"); v != "" {
		addr = v
	}

	hub := websocket.NewHub()
	go hub.Run()

	srv := server.New(hub)

	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}
