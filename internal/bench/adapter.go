package bench

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

type Adapter interface {
	Name() string
	Search(context.Context, string, int) ([]SearchResult, error)
}

type BM25Adapter struct {
	Store   *store.Store
	Project string
}

func (a BM25Adapter) Name() string { return "mcb-bm25" }

func (a BM25Adapter) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	memories, err := a.Store.SearchMemories(ctx, store.MemorySearch{Project: a.Project, Query: query, Limit: limit})
	if err != nil {
		if !errors.Is(err, store.ErrFTS5Unavailable) {
			return nil, err
		}
		memories, err = lexicalFallback(ctx, a.Store, a.Project, query, limit)
		if err != nil {
			return nil, err
		}
	}
	if len(memories) == 0 {
		var fallbackErr error
		memories, fallbackErr = lexicalFallback(ctx, a.Store, a.Project, query, limit)
		if fallbackErr != nil {
			return nil, fallbackErr
		}
	}
	results := make([]SearchResult, 0, len(memories))
	for _, memory := range memories {
		results = append(results, SearchResult{ID: memoryFixtureID(memory), Text: memory.Text, Score: memory.Score})
	}
	return results, nil
}

func lexicalFallback(ctx context.Context, s *store.Store, project, query string, limit int) ([]store.Memory, error) {
	if limit <= 0 {
		limit = 10
	}
	memories, err := s.RecentMemories(ctx, project, 1000)
	if err != nil {
		return nil, err
	}
	qTokens := tokenSet(query)
	type scored struct {
		memory store.Memory
		score  int
	}
	var scoredMemories []scored
	for _, memory := range memories {
		score := 0
		mTokens := tokenSet(memory.Text)
		for token := range qTokens {
			if mTokens[token] {
				score++
			}
		}
		if score > 0 {
			memory.Score = float64(score)
			scoredMemories = append(scoredMemories, scored{memory: memory, score: score})
		}
	}
	sort.SliceStable(scoredMemories, func(i, j int) bool {
		if scoredMemories[i].score == scoredMemories[j].score {
			return scoredMemories[i].memory.ID < scoredMemories[j].memory.ID
		}
		return scoredMemories[i].score > scoredMemories[j].score
	})
	if len(scoredMemories) > limit {
		scoredMemories = scoredMemories[:limit]
	}
	out := make([]store.Memory, 0, len(scoredMemories))
	for _, item := range scoredMemories {
		out = append(out, item.memory)
	}
	return out, nil
}

func memoryFixtureID(memory store.Memory) string {
	if strings.HasPrefix(memory.Source, "bench:") {
		return strings.TrimPrefix(memory.Source, "bench:")
	}
	return strings.TrimPrefix(strings.TrimSpace(memory.Text), "bench:")
}

func tokenSet(text string) map[string]bool {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r))
	})
	out := map[string]bool{}
	for _, field := range fields {
		if field != "" {
			out[field] = true
		}
	}
	return out
}
