package main

import (
	"encoding/json"
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /accounts/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("upstream received %s %s", r.Method, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accountNumber": "1234567890",
			"holder":        map[string]any{"email": "jane.doe@example.com"},
			"ssn":           "111-22-3333",
			"region":        "US",
			"status":        "active",
		})
	})
	log.Fatal(http.ListenAndServe(":8080", mux))
}
