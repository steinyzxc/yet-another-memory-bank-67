package bench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteReportsWritesRawSummaryAndScorecard(t *testing.T) {
	out := t.TempDir()
	result := RunResult{
		BenchName: "coding-life",
		Adapter:   "mcb-bm25",
		Metrics:   AggregateMetrics{PAt5: 0.5, RAt5: 1, HitAt5: 1, MRR: 0.75, NDCGAt10: 0.8, P50LatencyMS: 2, P95LatencyMS: 4},
		Queries:   []QueryResult{{QueryID: "q1", Query: "auth", GoldIDs: []string{"m1"}, RankedIDs: []string{"m1"}, LatencyMS: 2, Metrics: AggregateMetrics{PAt5: 0.2, RAt5: 1, HitAt5: 1, MRR: 1, NDCGAt10: 1}}},
		Reference: AgentMemoryReference("coding-life"),
	}
	if err := WriteReports(out, result); err != nil {
		t.Fatalf("write reports: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(out, "raw.ndjson"))
	if err != nil {
		t.Fatalf("read raw: %v", err)
	}
	if !strings.Contains(string(raw), `"query_id":"q1"`) {
		t.Fatalf("raw = %s", string(raw))
	}
	var summary SummaryReport
	data, err := os.ReadFile(filepath.Join(out, "summary.json"))
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	if err := json.Unmarshal(data, &summary); err != nil || summary.BenchName != "coding-life" {
		t.Fatalf("summary = %+v err=%v", summary, err)
	}
	scorecard, err := os.ReadFile(filepath.Join(out, "scorecard.md"))
	if err != nil {
		t.Fatalf("read scorecard: %v", err)
	}
	text := string(scorecard)
	if !strings.Contains(text, "mcb local run") || !strings.Contains(text, "agentmemory published reference") || !strings.Contains(text, "not same corpus") {
		t.Fatalf("scorecard = %s", text)
	}
}
