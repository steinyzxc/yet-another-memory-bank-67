//go:build sqlite_fts5

package admin

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAddAndSearchUseSameDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	io := IO{
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		Now:    func() int64 { return 1000 },
		Getwd:  func() (string, error) { return "/repo", nil },
	}

	code := Run(context.Background(), []string{"add", "--db", dbPath, "--project", "/repo", "jwt middleware validates bearer tokens"}, io)
	if code != 0 {
		t.Fatalf("add exit code = %d stderr=%s", code, io.Stderr)
	}
	if got := io.Stdout.(*bytes.Buffer).String(); !strings.Contains(got, "memory_id=") {
		t.Fatalf("add stdout = %q", got)
	}

	io.Stdout.(*bytes.Buffer).Reset()
	io.Stderr.(*bytes.Buffer).Reset()
	code = Run(context.Background(), []string{"search", "--db", dbPath, "--project", "/repo", "jwt middleware"}, io)
	if code != 0 {
		t.Fatalf("search exit code = %d stderr=%s", code, io.Stderr)
	}
	if got := io.Stdout.(*bytes.Buffer).String(); !strings.Contains(got, "jwt middleware validates bearer tokens") {
		t.Fatalf("search stdout = %q", got)
	}
}
