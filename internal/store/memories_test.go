package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAddMemoryStoresFact(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	id, err := s.AddMemory(ctx, MemoryInput{
		Project:   "/repo",
		Text:      "Use SQLite FTS5 for lexical BM25 search",
		Tier:      "fact",
		Source:    "manual",
		CreatedAt: 1000,
	})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}
	if id == 0 {
		t.Fatal("memory id should be non-zero")
	}

	got, err := s.Memory(ctx, id)
	if err != nil {
		t.Fatalf("load memory: %v", err)
	}
	if got.Project != "/repo" || got.Text != "Use SQLite FTS5 for lexical BM25 search" || got.Tier != "fact" || got.Source != "manual" {
		t.Fatalf("unexpected memory: %+v", got)
	}
	if got.Importance != 1 || got.CreatedAt != 1000 || got.UpdatedAt != 1000 || got.AccessedAt != 1000 {
		t.Fatalf("unexpected defaults: %+v", got)
	}
}

func TestRecentMemoriesReturnsProjectMemoriesNewestFirst(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	oldID, err := s.AddMemory(ctx, MemoryInput{Project: "/repo", Text: "old project fact", CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add old memory: %v", err)
	}
	newID, err := s.AddMemory(ctx, MemoryInput{Project: "/repo", Text: "new project fact", CreatedAt: 3000})
	if err != nil {
		t.Fatalf("add new memory: %v", err)
	}
	_, err = s.AddMemory(ctx, MemoryInput{Project: "/other", Text: "other project fact", CreatedAt: 4000})
	if err != nil {
		t.Fatalf("add other memory: %v", err)
	}

	got, err := s.RecentMemories(ctx, "/repo", 10)
	if err != nil {
		t.Fatalf("recent memories: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("recent len = %d, want 2: %+v", len(got), got)
	}
	if got[0].ID != newID || got[1].ID != oldID {
		t.Fatalf("recent order = %+v, want newest first", got)
	}

	limited, err := s.RecentMemories(ctx, "/repo", 1)
	if err != nil {
		t.Fatalf("limited recent memories: %v", err)
	}
	if len(limited) != 1 || limited[0].ID != newID {
		t.Fatalf("limited recent = %+v, want newest only", limited)
	}
}
