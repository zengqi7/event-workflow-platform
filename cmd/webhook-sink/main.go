package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

type receivedEvent struct {
	ReceivedAt time.Time      `json:"received_at"`
	Body       map[string]any `json:"body"`
}

type sinkStore struct {
	mu      sync.Mutex
	records []receivedEvent
}

func (s *sinkStore) append(body map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.records = append(s.records, receivedEvent{
		ReceivedAt: time.Now().UTC(),
		Body:       body,
	})
}

func (s *sinkStore) snapshot() []receivedEvent {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]receivedEvent, len(s.records))
	copy(out, s.records)
	return out
}

func main() {
	store := &sinkStore{}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/receive", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"message": "method not allowed"})
			return
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid json body"})
			return
		}

		store.append(body)
		writeJSON(w, http.StatusOK, map[string]string{"status": "received"})
	})
	mux.HandleFunc("/received", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"count":   len(store.snapshot()),
			"records": store.snapshot(),
		})
	})

	server := &http.Server{
		Addr:              ":18081",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("webhook sink listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
