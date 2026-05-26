package store

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func TestEnsureSessionCreatesNormalizedSession(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	sessionID, err := s.EnsureSession(ctx, "opencode", "raw-123", "/repo", 1000)
	if err != nil {
		t.Fatalf("ensure session: %v", err)
	}
	if sessionID != "opencode:raw-123" {
		t.Fatalf("session id = %q", sessionID)
	}

	got, err := s.Session(ctx, sessionID)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if got.Agent != "opencode" || got.ExternalID != "raw-123" || got.Project != "/repo" {
		t.Fatalf("unexpected session: %+v", got)
	}
}

func TestInsertObservationHandlesConcurrentWrites(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	const n = 100
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			inserted, err := s.InsertObservation(ctx, ObservationInput{
				Agent:             "opencode",
				ExternalSessionID: fmt.Sprintf("concurrent-%03d", i),
				CWD:               "/repo",
				TS:                int64(1000 + i),
				Kind:              "tool_use",
				Tool:              "Read",
				PayloadJSON:       []byte(fmt.Sprintf(`{"i":%d}`, i)),
				Hash:              fmt.Sprintf("hash-%03d", i),
			}, 300)
			if err != nil {
				errs <- err
				return
			}
			if !inserted {
				errs <- fmt.Errorf("observation %d was deduplicated", i)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("insert observation: %v", err)
		}
	}

	var count int
	if err := s.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM observations WHERE cwd = ?`, "/repo").Scan(&count); err != nil {
		t.Fatalf("count observations: %v", err)
	}
	if count != n {
		t.Fatalf("observation count = %d, want %d", count, n)
	}
}

func TestOpenAppliesSchemaVersionMigration(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	var version int
	err = s.readDB.QueryRowContext(ctx, `SELECT version FROM schema_version ORDER BY version DESC LIMIT 1`).Scan(&version)
	if err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != 6 {
		t.Fatalf("version = %d, want 6", version)
	}
}

func TestListSessionsAndEndSession(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	id, err := s.EnsureSession(ctx, "claude-code", "s1", "/repo", 1000)
	if err != nil {
		t.Fatalf("ensure session: %v", err)
	}
	if err := s.EndSession(ctx, id, 2000); err != nil {
		t.Fatalf("end session: %v", err)
	}

	sessions, err := s.ListSessions(ctx, "/repo", 10)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != id || sessions[0].EndedAt != 2000 {
		t.Fatalf("sessions = %+v", sessions)
	}
}

func TestInsertObservationDeduplicatesWithinWindow(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	obs := ObservationInput{
		Agent: "claude-code", ExternalSessionID: "s1", CWD: "/repo", TS: 1000,
		Kind: "tool_use", Tool: "Read", PayloadJSON: []byte(`{"a":1}`), Hash: "same",
	}
	inserted, err := s.InsertObservation(ctx, obs, 300)
	if err != nil || !inserted {
		t.Fatalf("first insert inserted=%v err=%v", inserted, err)
	}
	obs.TS = 1100
	inserted, err = s.InsertObservation(ctx, obs, 300)
	if err != nil {
		t.Fatalf("second insert: %v", err)
	}
	if inserted {
		t.Fatal("second insert should deduplicate")
	}

	count, err := s.ObservationCount(ctx, "claude-code:s1")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	var cwd string
	var payloadLen int64
	var schemaVersion int64
	err = s.readDB.QueryRowContext(ctx, `
SELECT cwd, payload_len, schema_version
FROM observations
WHERE session_id = ?
`, "claude-code:s1").Scan(&cwd, &payloadLen, &schemaVersion)
	if err != nil {
		t.Fatalf("load observation metadata: %v", err)
	}
	if cwd != "/repo" || payloadLen != int64(len([]byte(`{"a":1}`))) || schemaVersion != 1 {
		t.Fatalf("metadata cwd=%q payload_len=%d schema_version=%d", cwd, payloadLen, schemaVersion)
	}
}
