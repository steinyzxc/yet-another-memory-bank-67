package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := s.Ping(r.Context()); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/hooks/post-tool", func(w http.ResponseWriter, r *http.Request) {
		capture(w, r, s, integrations.NormalizeClaudePostTool)
	})
	mux.HandleFunc("/hooks/session-start", func(w http.ResponseWriter, r *http.Request) {
		claudeSessionStart(w, r, s)
	})
	mux.HandleFunc("/integrations/opencode/tool", func(w http.ResponseWriter, r *http.Request) {
		capture(w, r, s, integrations.NormalizeOpenCodeTool)
	})
	mux.HandleFunc("/integrations/opencode/context", func(w http.ResponseWriter, r *http.Request) {
		opencodeContext(w, r, s)
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

func claudeSessionStart(w http.ResponseWriter, r *http.Request, s *store.Store) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		SessionID string `json:"session_id"`
		CWD       string `json:"cwd"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if in.SessionID == "" || in.CWD == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if _, err := s.EnsureSession(r.Context(), "claude-code", in.SessionID, in.CWD, time.Now().UnixMilli()); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	memories, err := s.RecentMemories(r.Context(), in.CWD, 8)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}{
		HookSpecificOutput: struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		}{
			HookEventName:     "SessionStart",
			AdditionalContext: renderMemoryContext(memories),
		},
	})
}

func opencodeContext(w http.ResponseWriter, r *http.Request, s *store.Store) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		SessionID string `json:"session_id"`
		CWD       string `json:"cwd"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if in.SessionID == "" || in.CWD == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if _, err := s.EnsureSession(r.Context(), "opencode", in.SessionID, in.CWD, time.Now().UnixMilli()); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	memories, err := s.RecentMemories(r.Context(), in.CWD, 8)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		AdditionalContext string `json:"additional_context"`
	}{
		AdditionalContext: renderMemoryContext(memories),
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("read json body: %w", err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("parse json body: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func renderMemoryContext(memories []store.Memory) string {
	var b strings.Builder
	b.WriteString("<mcb-context>\n")
	b.WriteString("## Recent memories\n")
	if len(memories) == 0 {
		b.WriteString("No stored memories for this project yet.\n")
	} else {
		for _, memory := range memories {
			text := strings.ReplaceAll(memory.Text, "\n", " ")
			b.WriteString("- ")
			b.WriteString(text)
			b.WriteByte('\n')
		}
	}
	b.WriteString("</mcb-context>")
	return b.String()
}
