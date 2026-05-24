package store

import (
	"context"
	"path/filepath"
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
	if version != 1 {
		t.Fatalf("version = %d, want 1", version)
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
