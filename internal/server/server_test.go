package server

import (
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
