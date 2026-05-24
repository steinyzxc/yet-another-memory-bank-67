//go:build sqlite_fts5

package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSearchMemoriesBM25FindsLexicalMatchesInProject(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	firstID, err := s.AddMemory(ctx, MemoryInput{Project: "/repo", Text: "jwt middleware validates bearer tokens", CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add first memory: %v", err)
	}
	_, err = s.AddMemory(ctx, MemoryInput{Project: "/repo", Text: "русский поиск находит точные токены", CreatedAt: 2000})
	if err != nil {
		t.Fatalf("add russian memory: %v", err)
	}
	_, err = s.AddMemory(ctx, MemoryInput{Project: "/other", Text: "jwt middleware belongs to another project", CreatedAt: 3000})
	if err != nil {
		t.Fatalf("add other memory: %v", err)
	}

	results, err := s.SearchMemories(ctx, MemorySearch{Project: "/repo", Query: "jwt middleware", Limit: 10})
	if err != nil {
		t.Fatalf("search memories: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1: %+v", len(results), results)
	}
	if results[0].ID != firstID || results[0].Project != "/repo" {
		t.Fatalf("unexpected first result: %+v", results[0])
	}

	russian, err := s.SearchMemories(ctx, MemorySearch{Project: "/repo", Query: "русский токены", Limit: 10})
	if err != nil {
		t.Fatalf("search russian memories: %v", err)
	}
	if len(russian) != 1 || russian[0].Text != "русский поиск находит точные токены" {
		t.Fatalf("unexpected russian result: %+v", russian)
	}
}
