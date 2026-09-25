package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"mcp-runtime/pkg/mcpdefaults"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = strconv.Itoa(mcpdefaults.MCPServerPort)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		log.Fatal("doctor smoke server: PORT must be a valid TCP port")
	}
	handler := http.NewServeMux()
	handler.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	handler.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", portNumber),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(fmt.Errorf("doctor smoke server: %w", err))
	}
}
