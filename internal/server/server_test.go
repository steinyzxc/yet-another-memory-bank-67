package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alice/mcb/internal/store"
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
