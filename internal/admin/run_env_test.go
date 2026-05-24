package admin

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func TestRunAddUsesConfiguredStorageDBPath(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("MCB_STORAGE_DB_PATH", dbPath)
	io := IO{
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		Now:    func() int64 { return 1000 },
		Getwd:  func() (string, error) { return "/repo", nil },
	}

	code := Run(context.Background(), []string{"add", "--project", "/repo", "configured db path works"}, io)
	if code != 0 {
		t.Fatalf("add exit code = %d stderr=%s", code, io.Stderr)
	}

	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open configured db: %v", err)
	}
	defer s.Close()
	got, err := s.Memory(context.Background(), 1)
	if err != nil {
		t.Fatalf("load memory: %v", err)
	}
	if got.Text != "configured db path works" {
		t.Fatalf("memory text = %q", got.Text)
	}
}
