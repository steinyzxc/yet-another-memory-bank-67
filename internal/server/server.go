package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/dedup"
	"github.com/steinyzxc/yet-another-memory-bank-67/internal/integrations"
	"github.com/steinyzxc/yet-another-memory-bank-67/internal/secrets"
	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

const dedupWindowSeconds int64 = 300
const maxRequestBytes int64 = 1 << 20

type Options struct {
	BearerToken        string
	DedupWindowSeconds int64
	SessionStartTopN   int
}

func New(s *store.Store) http.Handler {
	return NewWithOptions(s, Options{})
}

func NewWithOptions(s *store.Store, opts Options) http.Handler {
	if opts.DedupWindowSeconds <= 0 {
		opts.DedupWindowSeconds = dedupWindowSeconds
	}
	if opts.SessionStartTopN <= 0 {
		opts.SessionStartTopN = 8
	}
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
		capture(w, r, s, opts, integrations.NormalizeClaudePostTool)
	})
	mux.HandleFunc("/hooks/user-prompt", func(w http.ResponseWriter, r *http.Request) {
		capture(w, r, s, opts, integrations.NormalizeClaudeUserPrompt)
	})
	mux.HandleFunc("/hooks/stop", func(w http.ResponseWriter, r *http.Request) {
		endSession(w, r, s, "claude-code")
	})
	mux.HandleFunc("/hooks/subagent-stop", func(w http.ResponseWriter, r *http.Request) {
		endSession(w, r, s, "claude-code")
	})
	mux.HandleFunc("/hooks/session-start", func(w http.ResponseWriter, r *http.Request) {
		claudeSessionStart(w, r, s, opts)
	})
	mux.HandleFunc("/integrations/opencode/tool", func(w http.ResponseWriter, r *http.Request) {
		capture(w, r, s, opts, integrations.NormalizeOpenCodeTool)
	})
	mux.HandleFunc("/integrations/opencode/chat", func(w http.ResponseWriter, r *http.Request) {
		capture(w, r, s, opts, integrations.NormalizeOpenCodeChat)
	})
	mux.HandleFunc("/integrations/opencode/context", func(w http.ResponseWriter, r *http.Request) {
		opencodeContext(w, r, s, opts)
	})
	mux.HandleFunc("/integrations/opencode/compact", func(w http.ResponseWriter, r *http.Request) {
		opencodeCompact(w, r, s)
	})
	return recoverMiddleware(authMiddleware(loggingMiddleware(mux), opts.BearerToken))
}

func capture(w http.ResponseWriter, r *http.Request, s *store.Store, opts Options, normalize func([]byte) (integrations.Event, error)) {
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
	}, opts.DedupWindowSeconds)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func claudeSessionStart(w http.ResponseWriter, r *http.Request, s *store.Store, opts Options) {
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
	memories, err := s.RecentMemories(r.Context(), in.CWD, opts.SessionStartTopN)
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

func opencodeContext(w http.ResponseWriter, r *http.Request, s *store.Store, opts Options) {
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
	memories, err := s.RecentMemories(r.Context(), in.CWD, opts.SessionStartTopN)
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

func endSession(w http.ResponseWriter, r *http.Request, s *store.Store, agent string) {
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
	now := time.Now().UnixMilli()
	sessionID, err := s.EnsureSession(r.Context(), agent, in.SessionID, in.CWD, now)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if err := s.EndSession(r.Context(), sessionID, now); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func opencodeCompact(w http.ResponseWriter, r *http.Request, s *store.Store) {
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
	now := time.Now().UnixMilli()
	sessionID, err := s.EnsureSession(r.Context(), "opencode", in.SessionID, in.CWD, now)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if err := s.EndSession(r.Context(), sessionID, now); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Compact bool   `json:"compact"`
		Reason  string `json:"reason"`
	}{Compact: false, Reason: "phase 1 compaction is disabled"})
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

func authMiddleware(next http.Handler, bearerToken string) http.Handler {
	if bearerToken == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+bearerToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Debug("http request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(start).Milliseconds())
	})
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("http panic", "path", r.URL.Path, "panic", recovered)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
