package admin

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alice/mcb/internal/store"
)

func TestRunSessionsListsSessions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	_, err = s.EnsureSession(context.Background(), "claude-code", "s1", "/repo", 1000)
	if err != nil {
		t.Fatalf("ensure session: %v", err)
	}
	s.Close()

	io := IO{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getwd: func() (string, error) { return "/repo", nil }}
	code := Run(context.Background(), []string{"sessions", "--db", dbPath, "--project", "/repo"}, io)
	if code != 0 {
		t.Fatalf("sessions exit code = %d stderr=%s", code, io.Stderr)
	}
	if got := io.Stdout.(*bytes.Buffer).String(); !strings.Contains(got, "claude-code:s1") || !strings.Contains(got, "/repo") {
		t.Fatalf("sessions stdout = %q", got)
	}
}
