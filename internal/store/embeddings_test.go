package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMemoryEmbeddingsRoundTripAndFilterModelDim(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	id, err := s.AddMemory(ctx, MemoryInput{Project: "/repo", Text: "stored vector", CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}
	if err := s.SaveMemoryEmbedding(ctx, id, "model-a", []float32{1, 0.5}); err != nil {
		t.Fatalf("save embedding: %v", err)
	}

	candidates, err := s.VectorCandidates(ctx, VectorSearch{Project: "/repo", Model: "model-a", Dim: 2})
	if err != nil {
		t.Fatalf("vector candidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Memory.ID != id || len(candidates[0].Vector) != 2 || candidates[0].Vector[1] != 0.5 {
		t.Fatalf("candidates = %+v", candidates)
	}

	wrongModel, err := s.VectorCandidates(ctx, VectorSearch{Project: "/repo", Model: "model-b", Dim: 2})
	if err != nil {
		t.Fatalf("wrong model candidates: %v", err)
	}
	if len(wrongModel) != 0 {
		t.Fatalf("wrong model candidates = %+v", wrongModel)
	}
}

func TestMissingEmbeddingsReturnsOnlyMemoriesWithoutCurrentModel(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	missingID, err := s.AddMemory(ctx, MemoryInput{Project: "/repo", Text: "needs embedding", CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add missing memory: %v", err)
	}
	presentID, err := s.AddMemory(ctx, MemoryInput{Project: "/repo", Text: "has embedding", CreatedAt: 2000})
	if err != nil {
		t.Fatalf("add present memory: %v", err)
	}
	if err := s.SaveMemoryEmbedding(ctx, presentID, "model-a", []float32{1, 0}); err != nil {
		t.Fatalf("save embedding: %v", err)
	}

	got, err := s.MissingEmbeddings(ctx, MissingEmbeddingSearch{Project: "/repo", Model: "model-a", Dim: 2, Limit: 10})
	if err != nil {
		t.Fatalf("missing embeddings: %v", err)
	}
	if len(got) != 1 || got[0].ID != missingID {
		t.Fatalf("missing = %+v, want id %d", got, missingID)
	}
}
