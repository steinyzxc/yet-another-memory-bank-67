package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedPerfDataCreatesScopedDataAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	if _, err := s.AddMemory(ctx, MemoryInput{Project: "/other", Text: "untouched memory", CreatedAt: 100}); err != nil {
		t.Fatalf("seed other memory: %v", err)
	}

	opts := PerfSeedOptions{Project: "/mcb-perf/test", RunID: "run-a", Sessions: 3, Memories: 5, Observations: 11, Now: 1_000_000}
	result, err := s.SeedPerfData(ctx, opts)
	if err != nil {
		t.Fatalf("seed perf data: %v", err)
	}
	if result.Project != opts.Project || result.RunID != opts.RunID || result.Sessions != 3 || result.Memories != 5 || result.Observations != 11 || result.Summaries != 3 {
		t.Fatalf("result = %+v", result)
	}

	assertPerfCounts(t, s, opts.Project, opts.RunID, 3, 5, 11)
	assertCount(t, s, `SELECT COUNT(*) FROM memories WHERE project = ?`, []any{"/other"}, 1)

	externalID := PerfSessionExternalID(opts.Project, opts.RunID, 0)
	if !strings.HasPrefix(externalID, "perf-") || !strings.Contains(externalID, "-session-000001") {
		t.Fatalf("external id = %q", externalID)
	}
	session, err := s.Session(ctx, "opencode:"+externalID)
	if err != nil {
		t.Fatalf("load perf session: %v", err)
	}
	if session.Project != opts.Project || session.NObs != 4 || session.Summary == "" {
		t.Fatalf("session = %+v", session)
	}

	opts.Observations = 6
	if _, err := s.SeedPerfData(ctx, opts); err != nil {
		t.Fatalf("reseed perf data: %v", err)
	}
	assertPerfCounts(t, s, opts.Project, opts.RunID, 3, 5, 6)

	opts.RunID = "run-b"
	opts.Sessions = 2
	opts.Memories = 1
	opts.Observations = 2
	if _, err := s.SeedPerfData(ctx, opts); err != nil {
		t.Fatalf("seed second run: %v", err)
	}
	assertPerfCounts(t, s, opts.Project, "run-a", 3, 5, 6)
	assertPerfCounts(t, s, opts.Project, "run-b", 2, 1, 2)
}

func assertPerfCounts(t *testing.T, s *Store, project, runID string, sessions, memories, observations int) {
	t.Helper()
	pattern := perfSessionExternalIDPrefix(project, runID) + "%"
	assertCount(t, s, `SELECT COUNT(*) FROM sessions WHERE project = ? AND agent = 'opencode' AND external_id LIKE ?`, []any{project, pattern}, sessions)
	assertCount(t, s, `SELECT COUNT(*) FROM memories WHERE project = ? AND source = ?`, []any{project, "perf-seed:" + runID}, memories)
	assertCount(t, s, `SELECT COUNT(*) FROM observations WHERE cwd = ? AND session_id IN (SELECT id FROM sessions WHERE project = ? AND external_id LIKE ?)`, []any{project, project, pattern}, observations)
}

func assertCount(t *testing.T, s *Store, query string, args []any, want int) {
	t.Helper()
	var got int
	if err := s.readDB.QueryRowContext(context.Background(), query, args...).Scan(&got); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if got != want {
		t.Fatalf("count = %d, want %d for %s", got, want, query)
	}
}
