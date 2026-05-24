package admin

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDoctorReportsCorruptConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("[storage\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	io := IO{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getwd: func() (string, error) { return "/repo", nil }}

	code := Run(context.Background(), []string{"doctor", "--config", configPath}, io)
	if code == 0 {
		t.Fatalf("doctor exit code = 0 stdout=%s", io.Stdout)
	}
	if got := io.Stderr.(*bytes.Buffer).String(); !strings.Contains(got, "config") {
		t.Fatalf("stderr = %q", got)
	}
}

func TestRunDoctorReportsMissingDB(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "missing.db")
	io := IO{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getwd: func() (string, error) { return "/repo", nil }}

	code := Run(context.Background(), []string{"doctor", "--db", missingPath}, io)
	if code == 0 {
		t.Fatalf("doctor exit code = 0 stdout=%s", io.Stdout)
	}
	if got := io.Stderr.(*bytes.Buffer).String(); !strings.Contains(got, "missing db") {
		t.Fatalf("stderr = %q", got)
	}
}

func TestRunDoctorReportsOKForValidDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	seedMemory(t, dbPath, "doctor ok fact")
	io := IO{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getwd: func() (string, error) { return "/repo", nil }}

	code := Run(context.Background(), []string{"doctor", "--db", dbPath}, io)
	if code != 0 {
		t.Fatalf("doctor exit code = %d stderr=%s", code, io.Stderr)
	}
	if got := io.Stdout.(*bytes.Buffer).String(); !strings.Contains(got, "ok") {
		t.Fatalf("stdout = %q", got)
	}
}
