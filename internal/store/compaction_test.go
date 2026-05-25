package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCompactionAttemptsExpireAndComplete(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	_, _ = s.EnsureSession(ctx, "claude-code", "s1", "/repo", 1000)

	if err := s.InsertCompactionAttempt(ctx, "claude-code:s1", "requested", "", 1000); err != nil {
		t.Fatalf("insert old attempt: %v", err)
	}
	if err := s.ExpireCompactionAttempts(ctx, "claude-code:s1", 5000); err != nil {
		t.Fatalf("expire attempts: %v", err)
	}
	count, err := s.FreshRequestedCompactionAttempts(ctx, "claude-code:s1", 5000)
	if err != nil {
		t.Fatalf("fresh attempts: %v", err)
	}
	if count != 0 {
		t.Fatalf("fresh attempts after expiry = %d", count)
	}
	if err := s.InsertCompactionAttempt(ctx, "claude-code:s1", "requested", "", 6000); err != nil {
		t.Fatalf("insert fresh attempt: %v", err)
	}
	if err := s.MarkCompactionCompleted(ctx, "claude-code:s1", 7000); err != nil {
		t.Fatalf("complete attempt: %v", err)
	}
	count, err = s.FreshRequestedCompactionAttempts(ctx, "claude-code:s1", 5000)
	if err != nil {
		t.Fatalf("fresh attempts after complete: %v", err)
	}
	if count != 0 {
		t.Fatalf("fresh attempts after complete = %d", count)
	}
}

func TestSessionNeedsCompactionAndCWDs(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	for i, cwd := range []string{"/repo", "/repo/pkg", "/repo"} {
		_, err := s.InsertObservation(ctx, ObservationInput{Agent: "claude-code", ExternalSessionID: "s1", CWD: cwd, TS: int64(1000 + i), Kind: "tool_use", Tool: "Read", PayloadJSON: []byte(`{"ok":true}`), Hash: string(rune('a' + i))}, 0)
		if err != nil {
			t.Fatalf("insert observation: %v", err)
		}
	}
	needs, err := s.SessionNeedsCompaction(ctx, "claude-code:s1", 3)
	if err != nil {
		t.Fatalf("needs compaction: %v", err)
	}
	if !needs {
		t.Fatalf("session should need compaction")
	}
	cwds, err := s.SessionCWDs(ctx, "claude-code:s1", 10)
	if err != nil {
		t.Fatalf("session cwds: %v", err)
	}
	if len(cwds) != 2 || cwds[0] != "/repo" || cwds[1] != "/repo/pkg" {
		t.Fatalf("cwds = %+v", cwds)
	}
	if err := s.SaveSessionSummary(ctx, "claude-code:s1", "summary", 5000); err != nil {
		t.Fatalf("save summary: %v", err)
	}
	needs, err = s.SessionNeedsCompaction(ctx, "claude-code:s1", 3)
	if err != nil {
		t.Fatalf("needs after summary: %v", err)
	}
	if needs {
		t.Fatalf("summarized session should not need compaction")
	}
}

func TestDecayMemoriesAndSupersede(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	oldID, err := s.AddMemory(ctx, MemoryInput{Project: "/repo", Text: "old fact", Tier: "semantic", Importance: 0.1, CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add old: %v", err)
	}
	newID, err := s.AddMemory(ctx, MemoryInput{Project: "/repo", Text: "new fact", Tier: "semantic", Importance: 1, CreatedAt: 2000})
	if err != nil {
		t.Fatalf("add new: %v", err)
	}
	if err := s.SupersedeMemory(ctx, oldID, newID); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	old, err := s.Memory(ctx, oldID)
	if err != nil {
		t.Fatalf("load old: %v", err)
	}
	if old.SupersededBy != newID {
		t.Fatalf("superseded_by = %d, want %d", old.SupersededBy, newID)
	}
	deleted, err := s.DecayMemories(ctx, DecayOptions{Now: 1000 + 90*24*60*60*1000, TauDays: 1, MinImportance: 0.05})
	if err != nil {
		t.Fatalf("decay: %v", err)
	}
	if deleted == 0 {
		t.Fatalf("decay deleted = 0")
	}
}
