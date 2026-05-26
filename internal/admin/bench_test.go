package admin

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/bench"
)

func TestRunBenchCodingLifeWritesReports(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bench")
	io := IO{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getwd: func() (string, error) { return "/repo", nil }, Now: func() int64 { return 1000 }}
	code := Run(context.Background(), []string{"bench", "coding-life", "--out", out}, io)
	if code != 0 {
		t.Fatalf("bench exit code = %d stderr=%s", code, io.Stderr)
	}
	if !strings.Contains(io.Stdout.(*bytes.Buffer).String(), "bench=coding-life") {
		t.Fatalf("stdout = %s", io.Stdout)
	}
	for _, name := range []string{"raw.ndjson", "summary.json", "scorecard.md", "sandbox.db"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
}

func TestRunBenchLongMemEvalRequiresDataset(t *testing.T) {
	io := IO{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getwd: func() (string, error) { return "/repo", nil }}
	code := Run(context.Background(), []string{"bench", "longmemeval", "--out", t.TempDir()}, io)
	if code != 1 || !strings.Contains(io.Stderr.(*bytes.Buffer).String(), "--dataset") {
		t.Fatalf("code=%d stderr=%s", code, io.Stderr)
	}
}

func TestRunBenchLongMemEvalUsesUserDataset(t *testing.T) {
	dir := t.TempDir()
	dataset := filepath.Join(dir, "dataset.json")
	data := `[{"question_id":"q1","question_type":"single-session-user","question":"where are project notes stored","answer_session_ids":["s1"],"haystack_session_ids":["s1","s2"],"haystack_sessions":[[{"role":"user","content":"Alice stores project notes in SQLite"}],[{"role":"user","content":"Bob likes tea"}]]}]`
	if err := os.WriteFile(dataset, []byte(data), 0o600); err != nil {
		t.Fatalf("write dataset: %v", err)
	}
	out := filepath.Join(dir, "out")
	io := IO{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getwd: func() (string, error) { return "/repo", nil }, Now: func() int64 { return 1000 }}
	code := Run(context.Background(), []string{"bench", "longmemeval", "--dataset", dataset, "--out", out, "--limit=1"}, io)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, io.Stderr)
	}
	result, err := bench.LoadLongMemEvalEntries(dataset)
	if err != nil || len(result) != 1 || result[0].QuestionID != "q1" {
		t.Fatalf("load dataset = %+v err=%v", result, err)
	}
}

func TestRunBenchRejectsInvalidLimit(t *testing.T) {
	io := IO{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getwd: func() (string, error) { return "/repo", nil }}
	code := Run(context.Background(), []string{"bench", "longmemeval", "--limit=abc"}, io)
	if code != 2 || !strings.Contains(io.Stderr.(*bytes.Buffer).String(), "invalid --limit") {
		t.Fatalf("code=%d stderr=%s", code, io.Stderr)
	}
}

func TestParseBenchPerfOptions(t *testing.T) {
	opts, rest, err := parseBenchOptions([]string{
		"--url", "http://127.0.0.1:3411",
		"--project=/mcb-perf/dev",
		"--run-id", "run-a",
		"--requests=25",
		"--concurrency", "1,10,50",
		"--groups=capture,mcp",
		"--fail-on-budget",
		"--out", "/tmp/out",
	})
	if err != nil || len(rest) != 0 {
		t.Fatalf("opts=%+v rest=%+v err=%v", opts, rest, err)
	}
	if opts.url != "http://127.0.0.1:3411" || opts.project != "/mcb-perf/dev" || opts.runID != "run-a" || opts.requests != 25 || opts.outDir != "/tmp/out" || !opts.failOnBudget {
		t.Fatalf("opts = %+v", opts)
	}
	if got := strings.Join(opts.groups, ","); got != "capture,mcp" {
		t.Fatalf("groups = %q", got)
	}
	if len(opts.concurrency) != 3 || opts.concurrency[0] != 1 || opts.concurrency[1] != 10 || opts.concurrency[2] != 50 {
		t.Fatalf("concurrency = %+v", opts.concurrency)
	}
}
