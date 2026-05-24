package admin

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func TestRunMigrateCreatesDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	io := IO{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getwd: func() (string, error) { return "/repo", nil }}

	code := Run(context.Background(), []string{"migrate", "--db", dbPath}, io)
	if code != 0 {
		t.Fatalf("migrate exit code = %d stderr=%s", code, io.Stderr)
	}
	if got := io.Stdout.(*bytes.Buffer).String(); !strings.Contains(got, "ok") {
		t.Fatalf("stdout = %q", got)
	}
	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open migrated db: %v", err)
	}
	defer s.Close()
}
