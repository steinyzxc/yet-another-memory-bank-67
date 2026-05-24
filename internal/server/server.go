package server

import (
	"io"
	"net/http"
	"time"

	"github.com/alice/mcb/internal/dedup"
	"github.com/alice/mcb/internal/integrations"
	"github.com/alice/mcb/internal/secrets"
	"github.com/alice/mcb/internal/store"
)

const dedupWindowSeconds int64 = 300
const maxRequestBytes int64 = 1 << 20

func New(s *store.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/hooks/post-tool", func(w http.ResponseWriter, r *http.Request) {
		capture(w, r, s, integrations.NormalizeClaudePostTool)
	})
	mux.HandleFunc("/integrations/opencode/tool", func(w http.ResponseWriter, r *http.Request) {
		capture(w, r, s, integrations.NormalizeOpenCodeTool)
	})
	return mux
}

func capture(w http.ResponseWriter, r *http.Request, s *store.Store, normalize func([]byte) (integrations.Event, error)) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	event, err := normalize(raw)
	if err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	hash, err := dedup.HashCanonicalJSON(event.PayloadJSON)
	if err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	payload := []byte(secrets.Redact(string(event.PayloadJSON)))
	_, err = s.InsertObservation(r.Context(), store.ObservationInput{
		Agent:             event.Agent,
		ExternalSessionID: event.ExternalSessionID,
		CWD:               event.CWD,
		TS:                time.Now().UnixMilli(),
		Kind:              event.Kind,
		Tool:              event.Tool,
		PayloadJSON:       payload,
		Hash:              hash,
	}, dedupWindowSeconds)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
