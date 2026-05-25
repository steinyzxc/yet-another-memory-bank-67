package search

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

type Embedder interface {
	Embed(context.Context, string) ([]float32, error)
}

type embedderModel interface {
	Model() string
	Dim() int
}

type CircuitBreaker interface {
	Open() bool
	RecordFailure()
	RecordSuccess()
}

type Config struct {
	BM25TopK      int
	VectorTopK    int
	FinalTopK     int
	RRFK          int
	MaxPerSession int
	Model         string
	Dim           int
	Now           func() int64
	SkipTouch     bool
}

type Query struct {
	Text    string
	Project string
	Limit   int
}

type Result struct {
	Memory   store.Memory
	Score    float64
	BM25Rank int
	VecRank  int
}

type Searcher struct {
	Store          *store.Store
	Embedder       Embedder
	CircuitBreaker CircuitBreaker
	Config         Config
}

func (s Searcher) Hybrid(ctx context.Context, q Query) ([]Result, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("search store is nil")
	}
	cfg := s.config(q.Limit)
	bm25, bm25Err := s.Store.SearchMemories(ctx, store.MemorySearch{Project: q.Project, Query: q.Text, Limit: cfg.BM25TopK})
	if bm25Err != nil && !errors.Is(bm25Err, store.ErrFTS5Unavailable) {
		return nil, bm25Err
	}

	vector, vectorErr := s.vectorResults(ctx, q, cfg)
	if vectorErr != nil {
		if bm25Err != nil {
			return nil, vectorErr
		}
		vector = nil
	}
	rankedBM25 := make([]Ranked, 0, len(bm25))
	memories := make(map[int64]store.Memory, len(bm25)+len(vector))
	for i, memory := range bm25 {
		rankedBM25 = append(rankedBM25, Ranked{ID: memory.ID, SessionID: memory.SessionID, BM25Rank: i + 1})
		memories[memory.ID] = memory
	}
	rankedVector := make([]Ranked, 0, len(vector))
	for i, item := range vector {
		rankedVector = append(rankedVector, Ranked{ID: item.Memory.ID, SessionID: item.Memory.SessionID, VecRank: i + 1})
		memories[item.Memory.ID] = item.Memory
	}
	fused := FuseRRF(rankedBM25, rankedVector, cfg.RRFK, cfg.FinalTopK, cfg.MaxPerSession)
	results := make([]Result, 0, len(fused))
	ids := make([]int64, 0, len(fused))
	for _, item := range fused {
		memory := memories[item.ID]
		memory.Score = item.Score
		results = append(results, Result{Memory: memory, Score: item.Score, BM25Rank: item.BM25Rank, VecRank: item.VecRank})
		ids = append(ids, item.ID)
	}
	if !cfg.SkipTouch {
		if err := s.Store.TouchMemories(ctx, ids, cfg.now()); err != nil {
			return nil, err
		}
	}
	return results, nil
}

func (s Searcher) vectorResults(ctx context.Context, q Query, cfg Config) ([]store.VectorCandidate, error) {
	if s.Embedder == nil {
		return nil, nil
	}
	if s.CircuitBreaker != nil && s.CircuitBreaker.Open() {
		return nil, nil
	}
	queryVec, err := s.Embedder.Embed(ctx, q.Text)
	if err != nil {
		if s.CircuitBreaker != nil {
			s.CircuitBreaker.RecordFailure()
		}
		return nil, err
	}
	if s.CircuitBreaker != nil {
		s.CircuitBreaker.RecordSuccess()
	}
	model := cfg.Model
	dim := cfg.Dim
	if info, ok := s.Embedder.(embedderModel); ok {
		if model == "" {
			model = info.Model()
		}
		if dim <= 0 {
			dim = info.Dim()
		}
	}
	if dim <= 0 {
		dim = len(queryVec)
	}
	if model == "" {
		return nil, fmt.Errorf("embedding model is empty")
	}
	if dim != len(queryVec) {
		return nil, fmt.Errorf("query embedding dimension = %d, want %d", len(queryVec), dim)
	}
	candidates, err := s.Store.VectorCandidates(ctx, store.VectorSearch{Project: q.Project, Model: model, Dim: dim})
	if err != nil {
		return nil, err
	}
	type scored struct {
		candidate store.VectorCandidate
		score     float64
	}
	scoredCandidates := make([]scored, 0, len(candidates))
	for _, candidate := range candidates {
		scoredCandidates = append(scoredCandidates, scored{candidate: candidate, score: cosine(queryVec, candidate.Vector)})
	}
	sort.SliceStable(scoredCandidates, func(i, j int) bool {
		if scoredCandidates[i].score == scoredCandidates[j].score {
			return scoredCandidates[i].candidate.Memory.ID < scoredCandidates[j].candidate.Memory.ID
		}
		return scoredCandidates[i].score > scoredCandidates[j].score
	})
	limit := cfg.VectorTopK
	if limit > len(scoredCandidates) {
		limit = len(scoredCandidates)
	}
	results := make([]store.VectorCandidate, 0, limit)
	for i := 0; i < limit; i++ {
		candidate := scoredCandidates[i].candidate
		candidate.Memory.Score = scoredCandidates[i].score
		results = append(results, candidate)
	}
	return results, nil
}

func (s Searcher) config(limit int) Config {
	cfg := s.Config
	if cfg.BM25TopK <= 0 {
		cfg.BM25TopK = 50
	}
	if cfg.VectorTopK <= 0 {
		cfg.VectorTopK = 50
	}
	if cfg.FinalTopK <= 0 {
		cfg.FinalTopK = 10
	}
	if limit > 0 && limit < cfg.FinalTopK {
		cfg.FinalTopK = limit
	}
	if cfg.RRFK <= 0 {
		cfg.RRFK = 60
	}
	if cfg.MaxPerSession <= 0 {
		cfg.MaxPerSession = 3
	}
	return cfg
}

func (c Config) now() int64 {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now().UnixMilli()
}

func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		af := float64(a[i])
		bf := float64(b[i])
		dot += af * bf
		normA += af * af
		normB += bf * bf
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
