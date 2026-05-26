package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func TestOpenCodeToolEndpointStoresObservation(t *testing.T) {
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	h := New(s)

	req := httptest.NewRequest(http.MethodPost, "/integrations/opencode/tool", strings.NewReader(`{"session_id":"o1","cwd":"/repo","tool":"read","input":{"file":"a.go"},"output":{"ok":true}}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	count, err := s.ObservationCount(t.Context(), "opencode:o1")
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

func TestHTTPLoggingIncludesRejectedRequests(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/integrations/opencode/event", nil)
	New(s).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	text := logs.String()
	if !strings.Contains(text, "http request rejected") || !strings.Contains(text, "path=/integrations/opencode/event") || !strings.Contains(text, "status=405") {
		t.Fatalf("logs = %s", text)
	}
}

func TestClaudeAdditionalHookEndpointsStoreObservations(t *testing.T) {
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	h := New(s)

	for _, tc := range []struct {
		name string
		path string
		body string
		kind string
		tool string
	}{
		{name: "pre tool", path: "/hooks/pre-tool", body: `{"session_id":"s1","cwd":"/repo","tool_name":"Read","tool_input":{"file_path":"a.go"}}`, kind: "pre_tool_use", tool: "Read"},
		{name: "post tool failure", path: "/hooks/post-tool-failure", body: `{"session_id":"s1","cwd":"/repo","tool_name":"Read","tool_input":{"file_path":"missing.go"},"tool_response":{"error":"not found"}}`, kind: "tool_error", tool: "Read"},
		{name: "subagent start", path: "/hooks/subagent-start", body: `{"session_id":"s1","cwd":"/repo","subagent_name":"mcb-compactor"}`, kind: "subagent_start"},
		{name: "notification", path: "/hooks/notification", body: `{"session_id":"s1","cwd":"/repo","message":"permission needed"}`, kind: "notification"},
		{name: "task completed", path: "/hooks/task-completed", body: `{"session_id":"s1","cwd":"/repo","task":"tests","status":"completed"}`, kind: "task_completed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}

	observations, err := s.ListSessionObservations(t.Context(), "claude-code:s1", 10)
	if err != nil {
		t.Fatalf("list observations: %v", err)
	}
	if len(observations) != 5 {
		t.Fatalf("observations = %d", len(observations))
	}
	for i, tc := range []struct {
		kind string
		tool string
	}{
		{kind: "pre_tool_use", tool: "Read"},
		{kind: "tool_error", tool: "Read"},
		{kind: "subagent_start"},
		{kind: "notification"},
		{kind: "task_completed"},
	} {
		if observations[i].Kind != tc.kind || observations[i].Tool != tc.tool {
			t.Fatalf("observation[%d] = kind %q tool %q", i, observations[i].Kind, observations[i].Tool)
		}
	}
}

func TestOpenCodeEventEndpointStoresObservation(t *testing.T) {
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/integrations/opencode/event", strings.NewReader(`{"session_id":"o1","cwd":"/repo","kind":"session_status","payload":{"status_type":"idle"}}`))
	New(s).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	observations, err := s.ListSessionObservations(t.Context(), "opencode:o1", 10)
	if err != nil {
		t.Fatalf("list observations: %v", err)
	}
	if len(observations) != 1 || observations[0].Kind != "session_status" {
		t.Fatalf("observations = %+v", observations)
	}
}

func TestReadyzChecksStore(t *testing.T) {
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	h := New(s)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status before close = %d body=%s", rec.Code, rec.Body.String())
	}

	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status after close = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestReadyzChecksConfiguredReadinessProbe(t *testing.T) {
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	h := NewWithOptions(s, Options{ReadinessProbe: func(*http.Request) error { return errNotReady{} }})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestServerMountsMCPInitialize(t *testing.T) {
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`))
	New(s).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"serverInfo"`) || !strings.Contains(rec.Body.String(), `"mcb"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

type errNotReady struct{}

func (errNotReady) Error() string { return "not ready" }

func TestClaudeSessionStartReturnsRecentMemoryContext(t *testing.T) {
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	_, err = s.AddMemory(t.Context(), store.MemoryInput{Project: "/repo", Text: "Use SQLite FTS5 for lexical search", CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/hooks/session-start", strings.NewReader(`{"session_id":"s1","cwd":"/repo"}`))
	New(s).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	context := body.HookSpecificOutput.AdditionalContext
	if !strings.Contains(context, "<mcb-context>") || !strings.Contains(context, "Use SQLite FTS5 for lexical search") {
		t.Fatalf("additionalContext = %q", context)
	}
}

func TestOpenCodeContextReturnsRecentMemoryContext(t *testing.T) {
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	_, err = s.AddMemory(t.Context(), store.MemoryInput{Project: "/repo", Text: "OpenCode should inject project memories", CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/integrations/opencode/context", strings.NewReader(`{"session_id":"o1","cwd":"/repo"}`))
	New(s).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		AdditionalContext string `json:"additional_context"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if !strings.Contains(body.AdditionalContext, "<mcb-context>") || !strings.Contains(body.AdditionalContext, "OpenCode should inject project memories") {
		t.Fatalf("additional_context = %q", body.AdditionalContext)
	}
}

func TestClaudePreCompactStoresObservationAndReturnsContext(t *testing.T) {
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	_, err = s.AddMemory(t.Context(), store.MemoryInput{Project: "/repo", Text: "Preserve compaction context", CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/hooks/pre-compact", strings.NewReader(`{"session_id":"s1","cwd":"/repo","trigger":"manual"}`))
	New(s).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if !strings.Contains(body.HookSpecificOutput.AdditionalContext, "Preserve compaction context") {
		t.Fatalf("additionalContext = %q", body.HookSpecificOutput.AdditionalContext)
	}
	count, err := s.ObservationCount(t.Context(), "claude-code:s1")
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

func TestOpenCodeEnrichReturnsFileMemoryContext(t *testing.T) {
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	_, err = s.AddMemory(t.Context(), store.MemoryInput{Project: "/repo", Text: "a.go uses table-driven tests", CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/integrations/opencode/enrich", strings.NewReader(`{"session_id":"o1","cwd":"/repo","files":["a.go"]}`))
	New(s).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		AdditionalContext string `json:"additional_context"`
		Context           string `json:"context"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if !strings.Contains(body.AdditionalContext, "a.go uses table-driven tests") || body.Context != body.AdditionalContext {
		t.Fatalf("body = %+v", body)
	}
}

func TestUserPromptEndpointsStoreObservations(t *testing.T) {
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	h := New(s)

	for _, tc := range []struct {
		name      string
		path      string
		body      string
		sessionID string
	}{
		{name: "claude", path: "/hooks/user-prompt", body: `{"session_id":"s1","cwd":"/repo","prompt":"remember this"}`, sessionID: "claude-code:s1"},
		{name: "opencode", path: "/integrations/opencode/chat", body: `{"session_id":"o1","cwd":"/repo","message":"remember this too"}`, sessionID: "opencode:o1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			count, err := s.ObservationCount(t.Context(), tc.sessionID)
			if err != nil || count != 1 {
				t.Fatalf("count=%d err=%v", count, err)
			}
		})
	}
}

func TestStopEndpointsEndSessions(t *testing.T) {
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	h := New(s)

	for _, tc := range []struct {
		name      string
		path      string
		body      string
		sessionID string
	}{
		{name: "claude stop", path: "/hooks/stop", body: `{"session_id":"s1","cwd":"/repo"}`, sessionID: "claude-code:s1"},
		{name: "claude session end", path: "/hooks/session-end", body: `{"session_id":"s2","cwd":"/repo"}`, sessionID: "claude-code:s2"},
		{name: "opencode session end", path: "/integrations/opencode/session-end", body: `{"session_id":"o1","cwd":"/repo"}`, sessionID: "opencode:o1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			session, err := s.Session(t.Context(), tc.sessionID)
			if err != nil {
				t.Fatalf("load session: %v", err)
			}
			if session.EndedAt == 0 {
				t.Fatalf("ended_at was not set: %+v", session)
			}
		})
	}
}

func TestSubagentStopIsObservabilityOnly(t *testing.T) {
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/hooks/subagent-stop", strings.NewReader(`{"session_id":"s2","cwd":"/repo"}`))
	New(s).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOpenCodeCompactReturnsNotNeededWhenSessionIsTooSmall(t *testing.T) {
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/integrations/opencode/compact", strings.NewReader(`{"session_id":"o1","cwd":"/repo"}`))
	New(s).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Compact bool   `json:"compact"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if body.Compact || !strings.Contains(body.Reason, "not needed") {
		t.Fatalf("body = %+v", body)
	}
}

func TestOpenCodeToolEndpointReturnsGenericErrors(t *testing.T) {
	validBody := `{"session_id":"o1","cwd":"/repo","tool":"read","input":{"file":"a.go"},"output":{"ok":true}}`

	t.Run("invalid request body", func(t *testing.T) {
		s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "memory.db"))
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		defer s.Close()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/integrations/opencode/tool", strings.NewReader(`{"session_id":`))
		New(s).ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		if got := strings.TrimSpace(rec.Body.String()); got != "invalid request" {
			t.Fatalf("body = %q", got)
		}
	})

	t.Run("oversized request body", func(t *testing.T) {
		s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "memory.db"))
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		defer s.Close()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/integrations/opencode/tool", strings.NewReader(strings.Repeat(" ", (1<<20)+1)))
		New(s).ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		if got := strings.TrimSpace(rec.Body.String()); got != "invalid request" {
			t.Fatalf("body = %q", got)
		}
	})

	t.Run("store insert failure", func(t *testing.T) {
		s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "memory.db"))
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/integrations/opencode/tool", strings.NewReader(validBody))
		New(s).ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		if got := strings.TrimSpace(rec.Body.String()); got != "internal server error" {
			t.Fatalf("body = %q", got)
		}
	})
}

func TestBearerTokenProtectsNonHealthEndpoints(t *testing.T) {
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	h := NewWithOptions(s, Options{BearerToken: "secret"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d", rec.Code)
	}

	body := `{"session_id":"o1","cwd":"/repo","tool":"read","input":{"file":"a.go"},"output":{"ok":true}}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/integrations/opencode/tool", strings.NewReader(body))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/integrations/opencode/tool", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("authenticated status = %d body=%s", rec.Code, rec.Body.String())
	}
}
