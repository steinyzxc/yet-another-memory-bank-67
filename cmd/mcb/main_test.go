//go:build sqlite_fts5

package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSupportsAddAndSearchCommands(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"add", "--db", dbPath, "--project", "/repo", "jwt middleware validates bearer tokens"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("add exit code = %d stderr=%s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"search", "--db", dbPath, "--project", "/repo", "jwt middleware"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("search exit code = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "jwt middleware validates bearer tokens") {
		t.Fatalf("search stdout = %q", got)
	}
}
