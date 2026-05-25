package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/dedup"
	"github.com/steinyzxc/yet-another-memory-bank-67/internal/integrations"
	internalmcp "github.com/steinyzxc/yet-another-memory-bank-67/internal/mcp"
	"github.com/steinyzxc/yet-another-memory-bank-67/internal/secrets"
	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

const dedupWindowSeconds int64 = 300
const maxRequestBytes int64 = 1 << 20

type Options struct {
	BearerToken        string
	DedupWindowSeconds int64
	SessionStartTopN   int
	ReadinessProbe     func(*http.Request) error
	MCPOptions         internalmcp.Options
	Compaction         CompactionOptions
	Now                func() int64
}

type CompactionOptions struct {
	Mode              string
	MinObservations   int
	MaxBlockAttempts  int
	AttemptTTLSeconds int64
	SubagentName      string
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
	opts.Compaction = normalizeCompaction(opts.Compaction)
	if opts.Now == nil {
		opts.Now = func() int64 { return time.Now().UnixMilli() }
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
		if opts.ReadinessProbe != nil {
			if err := opts.ReadinessProbe(r); err != nil {
				http.Error(w, "not ready", http.StatusServiceUnavailable)
				return
			}
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
		claudeStop(w, r, s, opts)
	})
	mux.HandleFunc("/hooks/subagent-stop", func(w http.ResponseWriter, r *http.Request) {
		subagentStop(w, r)
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
		opencodeCompact(w, r, s, opts)
	})
	mux.Handle("/mcp", internalmcp.New(s, opts.MCPOptions))
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
	if _, err := s.EnsureSession(r.Context(), "claude-code", in.SessionID, in.CWD, opts.Now()); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	memories, err := s.RecentMemories(r.Context(), in.CWD, opts.SessionStartTopN)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	summaries, err := s.RecentSessionSummaries(r.Context(), in.CWD, 3)
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
			AdditionalContext: renderMemoryContext(memories, summaries, compactorHint("claude-code", opts.Compaction)),
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
	if _, err := s.EnsureSession(r.Context(), "opencode", in.SessionID, in.CWD, opts.Now()); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	memories, err := s.RecentMemories(r.Context(), in.CWD, opts.SessionStartTopN)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	summaries, err := s.RecentSessionSummaries(r.Context(), in.CWD, 3)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		AdditionalContext string `json:"additional_context"`
	}{
		AdditionalContext: renderMemoryContext(memories, summaries, compactorHint("opencode", opts.Compaction)),
	})
}

func claudeStop(w http.ResponseWriter, r *http.Request, s *store.Store, opts Options) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		SessionID      string `json:"session_id"`
		CWD            string `json:"cwd"`
		StopHookActive bool   `json:"stop_hook_active"`
	}
	if err := decodeJSON(w, r, &in); err != nil || in.SessionID == "" || in.CWD == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	now := opts.Now()
	sessionID, err := s.EnsureSession(r.Context(), "claude-code", in.SessionID, in.CWD, now)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	decision, err := requestCompaction(r.Context(), s, sessionID, in.CWD, "claude-code", in.StopHookActive, opts.Compaction, now)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if decision == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}{Decision: "block", Reason: decision})
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

func subagentStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		SessionID string `json:"session_id"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	slog.Info("subagent stop", "session_id", in.SessionID)
	w.WriteHeader(http.StatusNoContent)
}

func opencodeCompact(w http.ResponseWriter, r *http.Request, s *store.Store, opts Options) {
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
	now := opts.Now()
	sessionID, err := s.EnsureSession(r.Context(), "opencode", in.SessionID, in.CWD, now)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	prompt, err := requestCompaction(r.Context(), s, sessionID, in.CWD, "opencode", false, opts.Compaction, now)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if prompt == "" {
		writeJSON(w, http.StatusOK, struct {
			Compact bool   `json:"compact"`
			Reason  string `json:"reason"`
		}{Compact: false, Reason: "compaction not needed"})
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Compact bool   `json:"compact"`
		Prompt  string `json:"prompt"`
	}{Compact: true, Prompt: prompt})
}

func requestCompaction(ctx context.Context, s *store.Store, sessionID, project, agent string, stopHookActive bool, cfg CompactionOptions, now int64) (string, error) {
	if err := s.EndSession(ctx, sessionID, now); err != nil {
		return "", err
	}
	if stopHookActive || cfg.Mode == "disabled" || cfg.Mode == "manual" {
		return "", nil
	}
	needs, err := s.SessionNeedsCompaction(ctx, sessionID, cfg.MinObservations)
	if err != nil || !needs {
		return "", err
	}
	cutoff := now - cfg.AttemptTTLSeconds*1000
	if err := s.ExpireCompactionAttempts(ctx, sessionID, cutoff); err != nil {
		return "", err
	}
	fresh, err := s.FreshRequestedCompactionAttempts(ctx, sessionID, cutoff)
	if err != nil {
		return "", err
	}
	if fresh >= cfg.MaxBlockAttempts {
		if err := s.InsertCompactionAttempt(ctx, sessionID, "skipped", "max attempts", now); err != nil {
			return "", err
		}
		return "", nil
	}
	if err := s.InsertCompactionAttempt(ctx, sessionID, "requested", "", now); err != nil {
		return "", err
	}
	cwds, err := s.SessionCWDs(ctx, sessionID, 10)
	if err != nil {
		return "", err
	}
	return compactorPrompt(agent, sessionID, project, cwds, cfg.SubagentName), nil
}

func normalizeCompaction(cfg CompactionOptions) CompactionOptions {
	if cfg.Mode == "" {
		cfg.Mode = "subagent"
	}
	if cfg.MinObservations <= 0 {
		cfg.MinObservations = 5
	}
	if cfg.MaxBlockAttempts <= 0 {
		cfg.MaxBlockAttempts = 2
	}
	if cfg.AttemptTTLSeconds <= 0 {
		cfg.AttemptTTLSeconds = 600
	}
	if cfg.SubagentName == "" {
		cfg.SubagentName = "mcb-compactor"
	}
	return cfg
}

func compactorPrompt(agent, sessionID, project string, cwds []string, subagent string) string {
	data, _ := json.Marshal(cwds)
	prompt := fmt.Sprintf("session_id=%s project=%s cwds=%s - read observations via mcp__mcb__memory_session_observations, deduplicate against existing memories via mcp__mcb__memory_search, save one summary via mcp__mcb__memory_session_summary_save, then save 3-7 facts via mcp__mcb__memory_save with session_id set.", sessionID, project, string(data))
	if agent == "claude-code" {
		return fmt.Sprintf("Before ending, dispatch the %s subagent via the Task tool with subagent_type=%q and this exact prompt: %q Do not ask the user; dispatch immediately. After the subagent reports completion, stop.", subagent, subagent, prompt)
	}
	return fmt.Sprintf("Run the %s subagent with this prompt: %s", subagent, prompt)
}

func compactorHint(agent string, cfg CompactionOptions) string {
	if cfg.Mode != "subagent" {
		return ""
	}
	if agent == "claude-code" {
		return fmt.Sprintf("If a Stop hook returns a block decision referencing %s, dispatch it without asking confirmation.", cfg.SubagentName)
	}
	return fmt.Sprintf("If mcb plugin requests %s, run that subagent without asking confirmation.", cfg.SubagentName)
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

func renderMemoryContext(memories []store.Memory, summaries []store.Session, hint string) string {
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
	if len(summaries) > 0 {
		b.WriteString("\n## Recent session summaries\n")
		for _, session := range summaries {
			b.WriteString("- ")
			b.WriteString(strings.ReplaceAll(session.Summary, "\n", " "))
			b.WriteByte('\n')
		}
	}
	if hint != "" {
		b.WriteString("\n## Compaction\n")
		b.WriteString(hint)
		b.WriteByte('\n')
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
