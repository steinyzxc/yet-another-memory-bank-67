package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestUpdateMemoryEditsFields(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	id, err := s.AddMemory(ctx, MemoryInput{Project: "/repo", Text: "old", Tier: "semantic", Importance: 0.3, CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}

	if err := s.UpdateMemory(ctx, MemoryUpdate{ID: id, Text: strPtr("new"), Tier: strPtr("procedural"), Importance: floatPtr(0.9), UpdatedAt: 5000}); err != nil {
		t.Fatalf("update memory: %v", err)
	}
	memory, err := s.Memory(ctx, id)
	if err != nil {
		t.Fatalf("load memory: %v", err)
	}
	if memory.Text != "new" || memory.Tier != "procedural" || memory.Importance != 0.9 || memory.UpdatedAt != 5000 {
		t.Fatalf("memory = %+v", memory)
	}
}

func TestProjectProfilesReturnsAllProjects(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	_, _ = s.AddMemory(ctx, MemoryInput{Project: "/repo", Text: "repo", CreatedAt: 1000})
	_, _ = s.AddMemory(ctx, MemoryInput{Project: "/other", Text: "other", CreatedAt: 2000})

	profiles, err := s.ProjectProfiles(ctx)
	if err != nil {
		t.Fatalf("project profiles: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("profiles = %+v", profiles)
	}
}

func strPtr(v string) *string     { return &v }
func floatPtr(v float64) *float64 { return &v }
