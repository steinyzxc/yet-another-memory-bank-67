package bench

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func TestRunPerfAgainstHTTPServerWritesReports(t *testing.T) {
	project := "/mcb-perf/test"
	runID := "run-a"
	seen := map[string]bool{}
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/integrations/replay/session" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			SessionID string `json:"session_id"`
			Limit     int    `json:"limit"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Limit != 1000 || !strings.HasPrefix(body.SessionID, "opencode:perf-") || !strings.Contains(body.SessionID, "-session-") {
			t.Fatalf("body = %+v", body)
		}
		mu.Lock()
		seen[body.SessionID] = true
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"events":[]}`))
	}))
	defer server.Close()

	out := t.TempDir()
	result, err := RunPerf(context.Background(), PerfOptions{
		URL:         server.URL + "/",
		Project:     project,
		RunID:       runID,
		OutDir:      out,
		Groups:      []string{"replay"},
		Requests:    3,
		Concurrency: []int{2},
		Now:         func() int64 { return 1234 },
	})
	if err != nil {
		t.Fatalf("run perf: %v", err)
	}
	if result.URL != server.URL || result.Project != project || result.RunID != runID || len(result.Samples) != 3 || len(result.Summaries) != 1 {
		t.Fatalf("result = %+v", result)
	}
	for i := 0; i < 3; i++ {
		want := "opencode:" + store.PerfSessionExternalID(project, runID, i)
		if !seen[want] {
			t.Fatalf("missing request for %s in %+v", want, seen)
		}
	}
	for _, name := range []string{"raw.ndjson", "summary.json", "scorecard.md"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(out, "raw.ndjson"))
	if err != nil {
		t.Fatalf("read raw: %v", err)
	}
	if strings.Count(string(raw), "\n") != 3 {
		t.Fatalf("raw = %s", raw)
	}
	var summary PerfResult
	data, err := os.ReadFile(filepath.Join(out, "summary.json"))
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	if err := json.Unmarshal(data, &summary); err != nil || summary.RunID != runID || summary.GeneratedAt != 1234 {
		t.Fatalf("summary = %+v err=%v", summary, err)
	}
	scorecard, err := os.ReadFile(filepath.Join(out, "scorecard.md"))
	if err != nil {
		t.Fatalf("read scorecard: %v", err)
	}
	if !strings.Contains(string(scorecard), "run_id") || !strings.Contains(string(scorecard), "replay_session") {
		t.Fatalf("scorecard = %s", scorecard)
	}
}

func TestSummarizePerfGroupIsDeterministic(t *testing.T) {
	samples := []PerfSample{
		{Group: "capture", Name: "event", Method: http.MethodPost, Path: "/event", Concurrency: 2, Status: 204, LatencyMS: 1, ResponseBytes: 10},
		{Group: "capture", Name: "event", Method: http.MethodPost, Path: "/event", Concurrency: 2, Status: 204, LatencyMS: 2, ResponseBytes: 20},
		{Group: "capture", Name: "event", Method: http.MethodPost, Path: "/event", Concurrency: 2, Status: 204, LatencyMS: 3, ResponseBytes: 30},
		{Group: "capture", Name: "event", Method: http.MethodPost, Path: "/event", Concurrency: 2, Status: 500, LatencyMS: 101, ResponseBytes: 40},
		{Group: "capture", Name: "event", Method: http.MethodPost, Path: "/event", Concurrency: 2, Status: 204, LatencyMS: 200, ResponseBytes: 50},
	}
	stats := summarizePerfGroup(samples)
	if stats.Requests != 5 || stats.Errors != 1 || stats.StatusCounts[204] != 4 || stats.StatusCounts[500] != 1 {
		t.Fatalf("stats counts = %+v", stats)
	}
	if stats.P50MS != 3 || stats.P90MS != 101 || stats.P95MS != 101 || stats.P99MS != 101 || stats.MaxMS != 200 {
		t.Fatalf("latencies = %+v", stats)
	}
	if stats.ResponseBytesP50 != 30 || stats.ResponseBytesP95 != 40 {
		t.Fatalf("response byte percentiles = %+v", stats)
	}
	if stats.BudgetStatus != "miss" || len(stats.BudgetMissReasons) != 2 {
		t.Fatalf("budget = %+v", stats)
	}
}
