// Command smokeapp is a minimal upstream used only by the Envoy smoke test.
// It returns a fixed account payload containing sensitive fields so the
// sidecar's masking can be observed end to end.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
)

func main() {
	addr := flag.String("addr", ":18080", "listen address")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /accounts/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"accountNumber": "1234567890",
			"holder":        map[string]any{"email": "jane.doe@example.com"},
			"ssn":           "111-22-3333",
			"region":        "US",
			"status":        "active",
		})
	})
	mux.HandleFunc("POST /payments", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":     "P-9001",
			"status": "accepted",
		})
	})
	log.Printf("smokeapp listening on %s", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
