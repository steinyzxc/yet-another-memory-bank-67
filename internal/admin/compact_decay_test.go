package admin

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func TestRunCompactPrintsSessionPrompt(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	_, _ = s.EnsureSession(context.Background(), "claude-code", "s1", "/repo", 1000)
	s.Close()

	io := IO{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getwd: func() (string, error) { return "/repo", nil }}
	code := Run(context.Background(), []string{"compact", "--db", dbPath, "--session", "claude-code:s1", "--agent", "claude-code"}, io)
	if code != 0 {
		t.Fatalf("compact exit = %d stderr=%s", code, io.Stderr)
	}
	stdout := io.Stdout.(*bytes.Buffer).String()
	if !strings.Contains(stdout, "mcb-compactor") || !strings.Contains(stdout, "claude-code:s1") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestRunDecayDecaysMemories(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	id, err := s.AddMemory(context.Background(), store.MemoryInput{Project: "/repo", Text: "old", Importance: 0.1, CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}
	s.Close()

	io := IO{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Now: func() int64 { return 1000 + 90*24*60*60*1000 }, Getwd: func() (string, error) { return "/repo", nil }}
	code := Run(context.Background(), []string{"decay", "--db", dbPath, "--tau-days", "1", "--min-importance", "0.05"}, io)
	if code != 0 {
		t.Fatalf("decay exit = %d stderr=%s", code, io.Stderr)
	}
	s, err = store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer s.Close()
	if _, err := s.Memory(context.Background(), id); err == nil {
		t.Fatalf("memory %d still exists after decay", id)
	}
}
