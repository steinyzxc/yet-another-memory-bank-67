package bench

import (
	"math"
	"sort"
)

func PrecisionAt(ranked, gold []string, k int) float64 {
	if k <= 0 || len(ranked) == 0 || len(gold) == 0 {
		return 0
	}
	if k > len(ranked) {
		k = len(ranked)
	}
	return float64(hitsAt(ranked, gold, k)) / float64(k)
}

func RecallAt(ranked, gold []string, k int) float64 {
	if k <= 0 || len(ranked) == 0 || len(gold) == 0 {
		return 0
	}
	return float64(hitsAt(ranked, gold, k)) / float64(len(set(gold)))
}

func HitAt(ranked, gold []string, k int) float64 {
	if hitsAt(ranked, gold, k) > 0 {
		return 1
	}
	return 0
}

func MRR(ranked, gold []string) float64 {
	goldSet := set(gold)
	if len(goldSet) == 0 {
		return 0
	}
	for i, id := range ranked {
		if goldSet[id] {
			return 1 / float64(i+1)
		}
	}
	return 0
}

func NDCGAt(ranked, gold []string, k int) float64 {
	if k <= 0 || len(ranked) == 0 || len(gold) == 0 {
		return 0
	}
	if k > len(ranked) {
		k = len(ranked)
	}
	goldSet := set(gold)
	var dcg float64
	for i := 0; i < k; i++ {
		if goldSet[ranked[i]] {
			dcg += 1 / math.Log2(float64(i+2))
		}
	}
	ideal := len(goldSet)
	if ideal > k {
		ideal = k
	}
	var idcg float64
	for i := 0; i < ideal; i++ {
		idcg += 1 / math.Log2(float64(i+2))
	}
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

func aggregate(results []QueryResult) AggregateMetrics {
	if len(results) == 0 {
		return AggregateMetrics{}
	}
	var out AggregateMetrics
	latencies := make([]int64, 0, len(results))
	for _, result := range results {
		out.PAt5 += result.Metrics.PAt5
		out.RAt5 += result.Metrics.RAt5
		out.RAt10 += result.Metrics.RAt10
		out.RAt20 += result.Metrics.RAt20
		out.HitAt5 += result.Metrics.HitAt5
		out.MRR += result.Metrics.MRR
		out.NDCGAt10 += result.Metrics.NDCGAt10
		latencies = append(latencies, result.LatencyMS)
	}
	n := float64(len(results))
	out.PAt5 /= n
	out.RAt5 /= n
	out.RAt10 /= n
	out.RAt20 /= n
	out.HitAt5 /= n
	out.MRR /= n
	out.NDCGAt10 /= n
	out.P50LatencyMS = percentile(latencies, 0.50)
	out.P95LatencyMS = percentile(latencies, 0.95)
	return out
}

func aggregateByType(results []QueryResult) map[string]AggregateMetrics {
	groups := map[string][]QueryResult{}
	for _, result := range results {
		if result.Type == "" {
			continue
		}
		groups[result.Type] = append(groups[result.Type], result)
	}
	if len(groups) == 0 {
		return nil
	}
	out := make(map[string]AggregateMetrics, len(groups))
	for _, typ := range sortedKeys(groups) {
		out[typ] = aggregate(groups[typ])
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func hitsAt(ranked, gold []string, k int) int {
	if k > len(ranked) {
		k = len(ranked)
	}
	goldSet := set(gold)
	hits := 0
	seen := map[string]bool{}
	for i := 0; i < k; i++ {
		id := ranked[i]
		if goldSet[id] && !seen[id] {
			hits++
			seen[id] = true
		}
	}
	return hits
}

func set(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		if item != "" {
			out[item] = true
		}
	}
	return out
}

func percentile(values []int64, p float64) int64 {
	if len(values) == 0 {
		return 0
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	idx := int(math.Ceil(p*float64(len(values)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(values) {
		idx = len(values) - 1
	}
	return values[idx]
}
