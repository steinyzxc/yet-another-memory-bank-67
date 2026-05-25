package search

import "sort"

type Ranked struct {
	ID        int64
	SessionID string
	Score     float64
	BM25Rank  int
	VecRank   int
}

func FuseRRF(bm25, vector []Ranked, rrfK, finalK, maxPerSession int) []Ranked {
	if rrfK <= 0 {
		rrfK = 60
	}
	if finalK <= 0 {
		finalK = 10
	}
	if maxPerSession <= 0 {
		maxPerSession = 3
	}
	byID := make(map[int64]Ranked)
	for i, item := range bm25 {
		rank := i + 1
		merged := byID[item.ID]
		merged.ID = item.ID
		merged.SessionID = item.SessionID
		merged.BM25Rank = rank
		merged.Score += 1 / float64(rrfK+rank)
		byID[item.ID] = merged
	}
	for i, item := range vector {
		rank := i + 1
		merged := byID[item.ID]
		merged.ID = item.ID
		if merged.SessionID == "" {
			merged.SessionID = item.SessionID
		}
		merged.VecRank = rank
		merged.Score += 1 / float64(rrfK+rank)
		byID[item.ID] = merged
	}
	items := make([]Ranked, 0, len(byID))
	for _, item := range byID {
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Score == items[j].Score {
			return items[i].ID < items[j].ID
		}
		return items[i].Score > items[j].Score
	})
	perSession := make(map[string]int)
	results := make([]Ranked, 0, finalK)
	for _, item := range items {
		if item.SessionID != "" && perSession[item.SessionID] >= maxPerSession {
			continue
		}
		results = append(results, item)
		if item.SessionID != "" {
			perSession[item.SessionID]++
		}
		if len(results) >= finalK {
			break
		}
	}
	return results
}
