//go:build sqlite_fts5

package bench

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func TestBM25AdapterSearchesBenchMemories(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	_, err = s.AddMemory(ctx, store.MemoryInput{Project: BenchProject, Text: "JWT middleware validates bearer tokens", Source: "bench:m-auth", CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}
	got, err := (BM25Adapter{Store: s, Project: BenchProject}).Search(ctx, "bearer tokens", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || got[0].ID != "m-auth" {
		t.Fatalf("results = %+v", got)
	}
}
