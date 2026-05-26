package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func TestRunPerfSeedWritesMetadata(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "memory.db")
	out := filepath.Join(dir, "out")
	io := IO{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Now: func() int64 { return 1_000_000 }}
	code := Run(context.Background(), []string{
		"perf-seed",
		"--db", db,
		"--project", "/mcb-perf/dev",
		"--run-id", "smoke",
		"--sessions", "2",
		"--memories", "3",
		"--observations", "4",
		"--out", out,
	}, io)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, io.Stderr)
	}
	stdout := io.Stdout.(*bytes.Buffer).String()
	if !strings.Contains(stdout, "project=/mcb-perf/dev") || !strings.Contains(stdout, "run_id=smoke") || !strings.Contains(stdout, "summaries=2") {
		t.Fatalf("stdout = %s", stdout)
	}
	data, err := os.ReadFile(filepath.Join(out, "seed.json"))
	if err != nil {
		t.Fatalf("read seed metadata: %v", err)
	}
	var result store.PerfSeedResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("parse seed metadata: %v", err)
	}
	if result.Project != "/mcb-perf/dev" || result.RunID != "smoke" || result.Sessions != 2 || result.Memories != 3 || result.Observations != 4 {
		t.Fatalf("result = %+v", result)
	}

	s, err := store.Open(context.Background(), db)
	if err != nil {
		t.Fatalf("open seeded db: %v", err)
	}
	defer s.Close()
	if _, err := s.Session(context.Background(), "opencode:"+store.PerfSessionExternalID("/mcb-perf/dev", "smoke", 0)); err != nil {
		t.Fatalf("load seeded session: %v", err)
	}
}

func TestParsePerfSeedOptionsDefaultsRunID(t *testing.T) {
	io := IO{Now: func() int64 { return 42 }}
	opts, err := parsePerfSeedOptions([]string{"--project", "/mcb-perf/dev", "--sessions=1"}, io)
	if err != nil {
		t.Fatalf("parse perf seed options: %v", err)
	}
	if opts.runID != "perf" || opts.project != "/mcb-perf/dev" || opts.sessions != 1 || opts.memories != 1000 || opts.observations != 10000 {
		t.Fatalf("opts = %+v", opts)
	}
}
