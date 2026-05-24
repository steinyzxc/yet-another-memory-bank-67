package admin

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func TestRunBackupWritesReadableSQLiteCopy(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	seedMemory(t, dbPath, "backup preserves this fact")
	backupPath := filepath.Join(t.TempDir(), "backup.db")
	io := IO{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getwd: func() (string, error) { return "/repo", nil }}

	code := Run(context.Background(), []string{"backup", "--db", dbPath, "--out", backupPath}, io)
	if code != 0 {
		t.Fatalf("backup exit code = %d stderr=%s", code, io.Stderr)
	}

	s, err := store.Open(context.Background(), backupPath)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer s.Close()
	got, err := s.Memory(context.Background(), 1)
	if err != nil {
		t.Fatalf("load backed up memory: %v", err)
	}
	if got.Text != "backup preserves this fact" {
		t.Fatalf("backup memory text = %q", got.Text)
	}
}

func TestRunBackupWritesDatabaseToStdout(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	seedMemory(t, dbPath, "stdout backup fact")
	io := IO{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getwd: func() (string, error) { return "/repo", nil }}

	code := Run(context.Background(), []string{"backup", "--db", dbPath, "--out", "-"}, io)
	if code != 0 {
		t.Fatalf("backup exit code = %d stderr=%s", code, io.Stderr)
	}
	if io.Stderr.(*bytes.Buffer).Len() != 0 {
		t.Fatalf("stderr = %q", io.Stderr)
	}
	if got := io.Stdout.(*bytes.Buffer).Bytes(); len(got) < 100 || !bytes.Contains(got[:100], []byte("SQLite format 3")) {
		t.Fatalf("stdout does not look like sqlite db, len=%d", len(got))
	}
}

func seedMemory(t *testing.T, dbPath, text string) {
	t.Helper()
	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	defer s.Close()
	_, err = s.AddMemory(context.Background(), store.MemoryInput{Project: "/repo", Text: text, CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add seed memory: %v", err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("stat seed db: %v", err)
	}
}
