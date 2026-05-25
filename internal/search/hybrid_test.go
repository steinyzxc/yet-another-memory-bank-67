//go:build sqlite_fts5

package search

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

type fakeEmbedder struct{ vec []float32 }

func (f fakeEmbedder) Embed(context.Context, string) ([]float32, error) { return f.vec, nil }
func (f fakeEmbedder) Model() string                                    { return "fake-model" }
func (f fakeEmbedder) Dim() int                                         { return len(f.vec) }

type failingEmbedder struct{}

func (failingEmbedder) Embed(context.Context, string) ([]float32, error) {
	return nil, errors.New("ollama down")
}

type fakeBreaker struct {
	open     bool
	failures int
	success  int
}

func (b *fakeBreaker) Open() bool     { return b.open }
func (b *fakeBreaker) RecordFailure() { b.failures++ }
func (b *fakeBreaker) RecordSuccess() { b.success++ }

func TestHybridReturnsVectorMatchWhenBM25Misses(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	_, err = s.AddMemory(ctx, store.MemoryInput{Project: "/repo", Text: "lexical jwt middleware", CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add lexical memory: %v", err)
	}
	semanticID, err := s.AddMemory(ctx, store.MemoryInput{Project: "/repo", Text: "semantic permissions policy", CreatedAt: 2000})
	if err != nil {
		t.Fatalf("add semantic memory: %v", err)
	}
	if err := s.SaveMemoryEmbedding(ctx, semanticID, "fake-model", []float32{1, 0}); err != nil {
		t.Fatalf("save embedding: %v", err)
	}

	searcher := Searcher{Store: s, Embedder: fakeEmbedder{vec: []float32{1, 0}}, Config: Config{BM25TopK: 10, VectorTopK: 10, FinalTopK: 5, RRFK: 60, MaxPerSession: 3}}
	got, err := searcher.Hybrid(ctx, Query{Text: "roles access", Project: "/repo", Limit: 5})
	if err != nil {
		t.Fatalf("hybrid: %v", err)
	}
	if len(got) == 0 || got[0].Memory.ID != semanticID || got[0].VecRank != 1 {
		t.Fatalf("results = %+v, want semantic vector match first", got)
	}
}

func TestHybridRecordsVectorFailuresAndFallsBackToBM25(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	id, err := s.AddMemory(ctx, store.MemoryInput{Project: "/repo", Text: "jwt middleware", CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}
	breaker := &fakeBreaker{}
	searcher := Searcher{Store: s, Embedder: failingEmbedder{}, CircuitBreaker: breaker, Config: Config{FinalTopK: 5}}

	got, err := searcher.Hybrid(ctx, Query{Text: "jwt", Project: "/repo", Limit: 5})
	if err != nil {
		t.Fatalf("hybrid: %v", err)
	}
	if breaker.failures != 1 || breaker.success != 0 {
		t.Fatalf("breaker = %+v", breaker)
	}
	if len(got) != 1 || got[0].Memory.ID != id || got[0].BM25Rank != 1 {
		t.Fatalf("results = %+v", got)
	}
}

func TestHybridSkipsVectorWhenCircuitBreakerOpen(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	_, err = s.AddMemory(ctx, store.MemoryInput{Project: "/repo", Text: "jwt middleware", CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}
	breaker := &fakeBreaker{open: true}
	searcher := Searcher{Store: s, Embedder: failingEmbedder{}, CircuitBreaker: breaker, Config: Config{FinalTopK: 5}}

	got, err := searcher.Hybrid(ctx, Query{Text: "jwt", Project: "/repo", Limit: 5})
	if err != nil {
		t.Fatalf("hybrid: %v", err)
	}
	if breaker.failures != 0 || len(got) != 1 {
		t.Fatalf("breaker=%+v results=%+v", breaker, got)
	}
}
