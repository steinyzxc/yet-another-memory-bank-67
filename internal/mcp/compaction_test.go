package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func TestMemorySessionSummarySaveCompletesAttempt(t *testing.T) {
	s := openTestStore(t)
	_, _ = s.EnsureSession(context.Background(), "claude-code", "s1", "/repo", 1000)
	if err := s.InsertCompactionAttempt(context.Background(), "claude-code:s1", "requested", "", 2000); err != nil {
		t.Fatalf("insert attempt: %v", err)
	}
	h := New(s, Options{Now: func() int64 { return 3000 }})

	result := callTool(t, h, "memory_session_summary_save", map[string]any{"session_id": "claude-code:s1", "summary": "Saved a concise summary."})
	var body struct {
		Updated bool `json:"updated"`
	}
	mustUnmarshal(t, result, &body)
	if !body.Updated {
		t.Fatalf("summary save result = %+v", body)
	}
	session, err := s.Session(context.Background(), "claude-code:s1")
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if session.Summary != "Saved a concise summary." || session.EndedAt != 3000 {
		t.Fatalf("session = %+v", session)
	}
	fresh, err := s.FreshRequestedCompactionAttempts(context.Background(), "claude-code:s1", 0)
	if err != nil || fresh != 0 {
		t.Fatalf("fresh attempts = %d err=%v", fresh, err)
	}
}

func TestMemorySupersedeTool(t *testing.T) {
	s := openTestStore(t)
	oldID, err := s.AddMemory(context.Background(), store.MemoryInput{Project: "/repo", Text: "old", CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add old: %v", err)
	}
	newID, err := s.AddMemory(context.Background(), store.MemoryInput{Project: "/repo", Text: "new", CreatedAt: 2000})
	if err != nil {
		t.Fatalf("add new: %v", err)
	}
	h := New(s, Options{})
	result := callTool(t, h, "memory_supersede", map[string]any{"old_id": oldID, "new_id": newID})
	if !strings.Contains(string(result), "updated") {
		t.Fatalf("result = %s", string(result))
	}
	old, err := s.Memory(context.Background(), oldID)
	if err != nil {
		t.Fatalf("load old: %v", err)
	}
	if old.SupersededBy != newID {
		t.Fatalf("old = %+v", old)
	}
}
