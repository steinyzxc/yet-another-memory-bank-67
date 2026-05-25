package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestListSessionObservationsDecodesStoredPayloads(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	largePayload := `{"message":"` + strings.Repeat("x", 700) + `"}`
	for _, tc := range []struct {
		hash string
		body string
	}{
		{hash: "small", body: `{"message":"small"}`},
		{hash: "large", body: largePayload},
	} {
		_, err := s.InsertObservation(ctx, ObservationInput{Agent: "claude-code", ExternalSessionID: "s1", CWD: "/repo", TS: 1000, Kind: "tool_use", Tool: "Read", PayloadJSON: []byte(tc.body), Hash: tc.hash}, 300)
		if err != nil {
			t.Fatalf("insert observation %s: %v", tc.hash, err)
		}
	}

	got, err := s.ListSessionObservations(ctx, "claude-code:s1", 10)
	if err != nil {
		t.Fatalf("list observations: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("observations len = %d, want 2", len(got))
	}
	if string(got[0].PayloadJSON) != `{"message":"small"}` || !strings.Contains(string(got[1].PayloadJSON), strings.Repeat("x", 20)) {
		t.Fatalf("decoded payloads = %+v", got)
	}
}

func TestDeleteMemoriesRemovesExplicitIDsOnly(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	deleteID, err := s.AddMemory(ctx, MemoryInput{Project: "/repo", Text: "delete me", CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add delete memory: %v", err)
	}
	keepID, err := s.AddMemory(ctx, MemoryInput{Project: "/repo", Text: "keep me", CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add keep memory: %v", err)
	}

	deleted, err := s.DeleteMemories(ctx, []int64{deleteID})
	if err != nil {
		t.Fatalf("delete memories: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if _, err := s.Memory(ctx, deleteID); err == nil {
		t.Fatalf("deleted memory %d still exists", deleteID)
	}
	if _, err := s.Memory(ctx, keepID); err != nil {
		t.Fatalf("kept memory %d missing: %v", keepID, err)
	}
}

func TestProjectProfileAggregatesProjectData(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	_, _ = s.EnsureSession(ctx, "claude-code", "s1", "/repo", 1000)
	_, _ = s.AddMemory(ctx, MemoryInput{Project: "/repo", Text: "semantic fact", Tier: "semantic", CreatedAt: 1000})
	_, _ = s.AddMemory(ctx, MemoryInput{Project: "/repo", Text: "procedure", Tier: "procedural", CreatedAt: 2000})
	_, _ = s.InsertObservation(ctx, ObservationInput{Agent: "claude-code", ExternalSessionID: "s1", CWD: "/repo", TS: 3000, Kind: "tool_use", Tool: "Read", PayloadJSON: []byte(`{"file":"a.go"}`), Hash: "h1"}, 300)

	profile, err := s.ProjectProfile(ctx, "/repo")
	if err != nil {
		t.Fatalf("project profile: %v", err)
	}
	if profile.MemoryCount != 2 || profile.SessionCount != 1 || profile.ObservationCount != 1 || profile.TopTiers["semantic"] != 1 || profile.TopTools["Read"] != 1 {
		t.Fatalf("profile = %+v", profile)
	}
}
