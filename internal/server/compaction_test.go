package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func TestStopHookRequestsCompactorWhenSessionNeedsCompaction(t *testing.T) {
	s := openServerStore(t)
	insertNObservations(t, s, "claude-code", "s1", "/repo", 5)
	h := NewWithOptions(s, Options{Compaction: CompactionOptions{Mode: "subagent", MinObservations: 5, MaxBlockAttempts: 2, AttemptTTLSeconds: 600, SubagentName: "mcb-compactor"}, Now: func() int64 { return 10_000 }})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/hooks/stop", strings.NewReader(`{"session_id":"s1","cwd":"/repo"}`))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if body.Decision != "block" || !strings.Contains(body.Reason, "mcb-compactor") || !strings.Contains(body.Reason, "memory_session_summary_save") {
		t.Fatalf("body = %+v", body)
	}
	count, err := s.FreshRequestedCompactionAttempts(t.Context(), "claude-code:s1", 0)
	if err != nil || count != 1 {
		t.Fatalf("attempts = %d err=%v", count, err)
	}
}

func TestStopHookSkipsActiveHookAndMaxAttempts(t *testing.T) {
	s := openServerStore(t)
	insertNObservations(t, s, "claude-code", "s1", "/repo", 5)
	opts := Options{Compaction: CompactionOptions{Mode: "subagent", MinObservations: 5, MaxBlockAttempts: 1, AttemptTTLSeconds: 600, SubagentName: "mcb-compactor"}, Now: func() int64 { return 10_000 }}
	h := NewWithOptions(s, opts)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/hooks/stop", strings.NewReader(`{"session_id":"s1","cwd":"/repo","stop_hook_active":true}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("active status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := s.InsertCompactionAttempt(t.Context(), "claude-code:s1", "requested", "", 9500); err != nil {
		t.Fatalf("insert attempt: %v", err)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/hooks/stop", strings.NewReader(`{"session_id":"s1","cwd":"/repo"}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("max attempts status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOpenCodeCompactReturnsPrompt(t *testing.T) {
	s := openServerStore(t)
	insertNObservations(t, s, "opencode", "o1", "/repo", 5)
	h := NewWithOptions(s, Options{Compaction: CompactionOptions{Mode: "subagent", MinObservations: 5, MaxBlockAttempts: 2, AttemptTTLSeconds: 600, SubagentName: "mcb-compactor"}, Now: func() int64 { return 10_000 }})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/integrations/opencode/compact", strings.NewReader(`{"session_id":"o1","cwd":"/repo"}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Compact bool   `json:"compact"`
		Prompt  string `json:"prompt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if !body.Compact || !strings.Contains(body.Prompt, "mcb-compactor") || !strings.Contains(body.Prompt, "opencode:o1") {
		t.Fatalf("body = %+v", body)
	}
}

func TestContextInjectIncludesSessionSummaryAndCompactorHint(t *testing.T) {
	s := openServerStore(t)
	_, _ = s.EnsureSession(t.Context(), "claude-code", "s1", "/repo", 1000)
	if err := s.SaveSessionSummary(t.Context(), "claude-code:s1", "Implemented phase four.", 2000); err != nil {
		t.Fatalf("save summary: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/hooks/session-start", strings.NewReader(`{"session_id":"s2","cwd":"/repo"}`))
	NewWithOptions(s, Options{Compaction: CompactionOptions{Mode: "subagent", SubagentName: "mcb-compactor"}}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Implemented phase four") || !strings.Contains(rec.Body.String(), "mcb-compactor") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func openServerStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func insertNObservations(t *testing.T, s *store.Store, agent, externalID, cwd string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		_, err := s.InsertObservation(t.Context(), store.ObservationInput{Agent: agent, ExternalSessionID: externalID, CWD: cwd, TS: int64(1000 + i), Kind: "tool_use", Tool: "Read", PayloadJSON: []byte(`{"ok":true}`), Hash: string(rune('a' + i))}, 0)
		if err != nil {
			t.Fatalf("insert observation %d: %v", i, err)
		}
	}
}
