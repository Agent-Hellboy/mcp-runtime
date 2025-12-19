package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

type info struct {
	Message string            `json:"message"`
	Env     map[string]string `json:"env"`
}

func handler(w http.ResponseWriter, r *http.Request) {
	// Collect a small subset of env vars for visibility.
	env := map[string]string{}
	for _, key := range []string{"ENV_VAR_1", "ENV_VAR_2"} {
		if val, ok := os.LookupEnv(key); ok {
			env[key] = val
		}
	}

	resp := info{
		Message: "mcp example server is up",
		Env:     env,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func main() {
	http.HandleFunc("/", handler)
	port := "8088"
	log.Printf("listening on :%s", port)
	server := &http.Server{
		Addr:              ":" + port,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}
