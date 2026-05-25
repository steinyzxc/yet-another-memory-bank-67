package admin

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func TestRunImportJSONLImportsClaudeTranscriptIdempotently(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")
	jsonlPath := filepath.Join(dir, "transcript.jsonl")
	data := strings.Join([]string{
		`{"uuid":"e1","sessionId":"s1","cwd":"/repo","timestamp":"2026-05-26T00:00:00Z","type":"user","message":{"role":"user","content":"remember auth"}}`,
		`{"uuid":"e2","sessionId":"s1","cwd":"/repo","timestamp":"2026-05-26T00:00:01Z","type":"tool_use","tool_name":"Read","tool_input":{"file_path":"a.go"},"tool_response":{"ok":true}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(jsonlPath, []byte(data), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	io := IO{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getwd: func() (string, error) { return "/repo", nil }, Now: func() int64 { return 1000 }}
	for i := 0; i < 2; i++ {
		io.Stdout.(*bytes.Buffer).Reset()
		code := Run(context.Background(), []string{"import-jsonl", "--db", dbPath, jsonlPath}, io)
		if code != 0 {
			t.Fatalf("import exit code = %d stderr=%s", code, io.Stderr)
		}
	}
	if got := io.Stdout.(*bytes.Buffer).String(); !strings.Contains(got, "imported=0") {
		t.Fatalf("second import stdout = %q", got)
	}

	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	observations, err := s.ListSessionObservations(context.Background(), "claude-code:s1", 10)
	if err != nil {
		t.Fatalf("list observations: %v", err)
	}
	if len(observations) != 2 || observations[0].Kind != "user_message" || observations[1].Tool != "Read" {
		t.Fatalf("observations = %+v", observations)
	}
}

func TestRunImportJSONLClassifiesNestedClaudeToolBlocks(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")
	jsonlPath := filepath.Join(dir, "transcript.jsonl")
	data := strings.Join([]string{
		`{"uuid":"a1","sessionId":"s2","cwd":"/repo","timestamp":"2026-05-26T00:00:00Z","type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"reading"},{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"main.go"}}]}}`,
		`{"uuid":"u1","sessionId":"s2","cwd":"/repo","timestamp":"2026-05-26T00:00:01Z","type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(jsonlPath, []byte(data), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	io := IO{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getwd: func() (string, error) { return "/repo", nil }, Now: func() int64 { return 1000 }}
	code := Run(context.Background(), []string{"import-jsonl", "--db", dbPath, jsonlPath}, io)
	if code != 0 {
		t.Fatalf("import exit code = %d stderr=%s", code, io.Stderr)
	}

	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	observations, err := s.ListSessionObservations(context.Background(), "claude-code:s2", 10)
	if err != nil {
		t.Fatalf("list observations: %v", err)
	}
	if len(observations) != 2 {
		t.Fatalf("observations = %+v", observations)
	}
	if observations[0].Kind != "tool_use" || observations[0].Tool != "Read" {
		t.Fatalf("tool use observation = %+v", observations[0])
	}
	if observations[1].Kind != "tool_result" {
		t.Fatalf("tool result observation = %+v", observations[1])
	}
}
