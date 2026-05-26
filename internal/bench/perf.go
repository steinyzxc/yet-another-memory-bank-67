package bench

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

const PerfBenchmarkVersion = "v1"

type PerfOptions struct {
	URL          string
	Project      string
	RunID        string
	OutDir       string
	Concurrency  []int
	Requests     int
	Groups       []string
	FailOnBudget bool
	Now          func() int64
	Client       *http.Client
}

type PerfResult struct {
	Version      string              `json:"version"`
	URL          string              `json:"url"`
	Project      string              `json:"project"`
	RunID        string              `json:"run_id"`
	Requests     int                 `json:"requests_per_endpoint"`
	Concurrency  []int               `json:"concurrency"`
	Groups       []string            `json:"groups"`
	GeneratedAt  int64               `json:"generated_at"`
	Samples      []PerfSample        `json:"-"`
	Summaries    []PerfEndpointStats `json:"summaries"`
	BudgetMisses []string            `json:"budget_misses,omitempty"`
}

type PerfSample struct {
	Group         string  `json:"group"`
	Name          string  `json:"name"`
	Method        string  `json:"method"`
	Path          string  `json:"path"`
	Concurrency   int     `json:"concurrency"`
	Status        int     `json:"status"`
	LatencyMS     float64 `json:"latency_ms"`
	ResponseBytes int     `json:"response_bytes"`
	Error         string  `json:"error,omitempty"`
}

type PerfEndpointStats struct {
	Group               string      `json:"group"`
	Name                string      `json:"name"`
	Method              string      `json:"method"`
	Path                string      `json:"path"`
	Concurrency         int         `json:"concurrency"`
	Requests            int         `json:"requests"`
	Errors              int         `json:"errors"`
	StatusCounts        map[int]int `json:"status_counts"`
	P50MS               float64     `json:"p50_ms"`
	P90MS               float64     `json:"p90_ms"`
	P95MS               float64     `json:"p95_ms"`
	P99MS               float64     `json:"p99_ms"`
	MaxMS               float64     `json:"max_ms"`
	RPS                 float64     `json:"rps"`
	ResponseBytesP50    int         `json:"response_bytes_p50"`
	ResponseBytesP95    int         `json:"response_bytes_p95"`
	Budget              PerfBudget  `json:"budget"`
	BudgetStatus        string      `json:"budget_status"`
	BudgetMissReasons   []string    `json:"budget_miss_reasons,omitempty"`
	ElapsedMilliseconds int64       `json:"elapsed_ms"`
}

type PerfBudget struct {
	P95MS float64 `json:"p95_ms"`
}

type perfEndpoint struct {
	Group string
	Name  string
	Path  string
	Body  func(project, runID string, i int) any
}

func RunPerf(ctx context.Context, opts PerfOptions) (PerfResult, error) {
	if opts.URL == "" {
		opts.URL = "http://127.0.0.1:3411"
	}
	opts.URL = strings.TrimRight(opts.URL, "/")
	if opts.Project == "" {
		return PerfResult{}, fmt.Errorf("perf benchmark requires --project")
	}
	if opts.RunID == "" {
		opts.RunID = "perf"
	}
	if opts.Requests <= 0 {
		opts.Requests = 100
	}
	if len(opts.Concurrency) == 0 {
		opts.Concurrency = []int{1, 10}
	}
	if len(opts.Groups) == 0 {
		opts.Groups = []string{"capture", "context", "compaction", "replay", "mcp"}
	}
	if opts.OutDir == "" {
		opts.OutDir = filepath.Join("benchmark-results", "perf-"+time.Now().Format("20060102-150405"))
	}
	if opts.Now == nil {
		opts.Now = func() int64 { return time.Now().UnixMilli() }
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	endpoints := perfEndpoints(opts.Groups)
	if len(endpoints) == 0 {
		return PerfResult{}, fmt.Errorf("no perf benchmark groups selected")
	}

	var samples []PerfSample
	for _, endpoint := range endpoints {
		for _, concurrency := range opts.Concurrency {
			if concurrency <= 0 {
				return PerfResult{}, fmt.Errorf("invalid concurrency %d", concurrency)
			}
			runSamples := runPerfEndpoint(ctx, client, opts.URL, opts.Project, opts.RunID, endpoint, concurrency, opts.Requests)
			samples = append(samples, runSamples...)
		}
	}
	result := PerfResult{Version: PerfBenchmarkVersion, URL: opts.URL, Project: opts.Project, RunID: opts.RunID, Requests: opts.Requests, Concurrency: opts.Concurrency, Groups: opts.Groups, GeneratedAt: opts.Now(), Samples: samples, Summaries: summarizePerf(samples)}
	result.BudgetMisses = perfBudgetMisses(result.Summaries)
	if err := WritePerfReports(opts.OutDir, result); err != nil {
		return PerfResult{}, err
	}
	if opts.FailOnBudget && len(result.BudgetMisses) > 0 {
		return result, fmt.Errorf("perf budget missed: %s", strings.Join(result.BudgetMisses, "; "))
	}
	return result, nil
}

func perfEndpoints(groups []string) []perfEndpoint {
	selected := map[string]bool{}
	for _, group := range groups {
		selected[strings.TrimSpace(group)] = true
	}
	all := []perfEndpoint{
		{Group: "capture", Name: "opencode_event", Path: "/integrations/opencode/event", Body: opencodeEventBody},
		{Group: "capture", Name: "opencode_tool", Path: "/integrations/opencode/tool", Body: opencodeToolBody},
		{Group: "capture", Name: "opencode_chat", Path: "/integrations/opencode/chat", Body: opencodeChatBody},
		{Group: "capture", Name: "claude_post_tool", Path: "/hooks/post-tool", Body: claudePostToolBody},
		{Group: "capture", Name: "claude_user_prompt", Path: "/hooks/user-prompt", Body: claudeUserPromptBody},
		{Group: "context", Name: "opencode_context", Path: "/integrations/opencode/context", Body: sessionBody},
		{Group: "context", Name: "opencode_enrich", Path: "/integrations/opencode/enrich", Body: enrichBody},
		{Group: "context", Name: "claude_session_start", Path: "/hooks/session-start", Body: sessionBody},
		{Group: "compaction", Name: "opencode_compact", Path: "/integrations/opencode/compact", Body: sessionBody},
		{Group: "compaction", Name: "claude_stop", Path: "/hooks/stop", Body: claudeStopBody},
		{Group: "replay", Name: "replay_session", Path: "/integrations/replay/session", Body: replayBody},
		{Group: "mcp", Name: "memory_search", Path: "/mcp", Body: mcpMemorySearchBody},
		{Group: "mcp", Name: "memory_recall", Path: "/mcp", Body: mcpMemoryRecallBody},
		{Group: "mcp", Name: "memory_save", Path: "/mcp", Body: mcpMemorySaveBody},
		{Group: "mcp", Name: "memory_session_observations", Path: "/mcp", Body: mcpMemorySessionObservationsBody},
	}
	out := make([]perfEndpoint, 0, len(all))
	for _, endpoint := range all {
		if selected[endpoint.Group] {
			out = append(out, endpoint)
		}
	}
	return out
}

func runPerfEndpoint(ctx context.Context, client *http.Client, baseURL, project, runID string, endpoint perfEndpoint, concurrency, requests int) []PerfSample {
	samples := make([]PerfSample, requests)
	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				samples[i] = executePerfRequest(ctx, client, baseURL, project, runID, endpoint, concurrency, i)
			}
		}()
	}
	for i := 0; i < requests; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return samples
}

func executePerfRequest(ctx context.Context, client *http.Client, baseURL, project, runID string, endpoint perfEndpoint, concurrency, i int) PerfSample {
	sample := PerfSample{Group: endpoint.Group, Name: endpoint.Name, Method: http.MethodPost, Path: endpoint.Path, Concurrency: concurrency}
	body, err := json.Marshal(endpoint.Body(project, runID, i))
	if err != nil {
		sample.Error = err.Error()
		return sample
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+endpoint.Path, bytes.NewReader(body))
	if err != nil {
		sample.Error = err.Error()
		return sample
	}
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	resp, err := client.Do(req)
	sample.LatencyMS = float64(time.Since(start).Microseconds()) / 1000
	if err != nil {
		sample.Error = err.Error()
		return sample
	}
	defer resp.Body.Close()
	sample.Status = resp.StatusCode
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		sample.Error = err.Error()
		return sample
	}
	sample.ResponseBytes = len(data)
	return sample
}

func summarizePerf(samples []PerfSample) []PerfEndpointStats {
	groups := map[string][]PerfSample{}
	for _, sample := range samples {
		key := fmt.Sprintf("%s\x00%s\x00%d", sample.Group, sample.Name, sample.Concurrency)
		groups[key] = append(groups[key], sample)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]PerfEndpointStats, 0, len(keys))
	for _, key := range keys {
		items := groups[key]
		out = append(out, summarizePerfGroup(items))
	}
	return out
}

func summarizePerfGroup(samples []PerfSample) PerfEndpointStats {
	stats := PerfEndpointStats{Group: samples[0].Group, Name: samples[0].Name, Method: samples[0].Method, Path: samples[0].Path, Concurrency: samples[0].Concurrency, Requests: len(samples), StatusCounts: map[int]int{}, Budget: PerfBudget{P95MS: perfBudgetP95(samples[0].Group)}}
	latencies := make([]float64, 0, len(samples))
	responseBytes := make([]int, 0, len(samples))
	var totalLatency float64
	for _, sample := range samples {
		if sample.Error != "" || sample.Status >= 500 || sample.Status == 0 {
			stats.Errors++
		}
		stats.StatusCounts[sample.Status]++
		latencies = append(latencies, sample.LatencyMS)
		responseBytes = append(responseBytes, sample.ResponseBytes)
		if sample.LatencyMS > stats.MaxMS {
			stats.MaxMS = sample.LatencyMS
		}
		totalLatency += sample.LatencyMS
	}
	stats.P50MS = percentileFloat(latencies, 0.50)
	stats.P90MS = percentileFloat(latencies, 0.90)
	stats.P95MS = percentileFloat(latencies, 0.95)
	stats.P99MS = percentileFloat(latencies, 0.99)
	stats.ResponseBytesP50 = percentileInt(responseBytes, 0.50)
	stats.ResponseBytesP95 = percentileInt(responseBytes, 0.95)
	if totalLatency > 0 {
		stats.RPS = float64(len(samples)) / (totalLatency / 1000 / float64(max(1, stats.Concurrency)))
	}
	stats.ElapsedMilliseconds = int64(totalLatency / float64(max(1, stats.Concurrency)))
	stats.BudgetStatus = "ok"
	if stats.P95MS > stats.Budget.P95MS {
		stats.BudgetStatus = "miss"
		stats.BudgetMissReasons = append(stats.BudgetMissReasons, fmt.Sprintf("p95 %.3fms > %.3fms", stats.P95MS, stats.Budget.P95MS))
	}
	if stats.Errors > 0 {
		stats.BudgetStatus = "miss"
		stats.BudgetMissReasons = append(stats.BudgetMissReasons, fmt.Sprintf("errors %d", stats.Errors))
	}
	return stats
}

func perfBudgetP95(group string) float64 {
	switch group {
	case "capture":
		return 100
	case "context":
		return 150
	case "compaction":
		return 150
	case "replay":
		return 250
	case "mcp":
		return 500
	default:
		return 250
	}
}

func perfBudgetMisses(summaries []PerfEndpointStats) []string {
	var misses []string
	for _, summary := range summaries {
		if summary.BudgetStatus == "miss" {
			misses = append(misses, fmt.Sprintf("%s/%s@%d: %s", summary.Group, summary.Name, summary.Concurrency, strings.Join(summary.BudgetMissReasons, ", ")))
		}
	}
	return misses
}

func WritePerfReports(outDir string, result PerfResult) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create perf report dir: %w", err)
	}
	if err := writePerfRaw(filepath.Join(outDir, "raw.ndjson"), result.Samples); err != nil {
		return err
	}
	if err := writePerfSummary(filepath.Join(outDir, "summary.json"), result); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "scorecard.md"), []byte(renderPerfScorecard(result)), 0o644); err != nil {
		return fmt.Errorf("write perf scorecard: %w", err)
	}
	return nil
}

func writePerfRaw(path string, samples []PerfSample) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create perf raw report: %w", err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, sample := range samples {
		data, err := json.Marshal(sample)
		if err != nil {
			return fmt.Errorf("marshal perf sample: %w", err)
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		if err := w.WriteByte('\n'); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush perf raw report: %w", err)
	}
	return nil
}

func writePerfSummary(path string, result PerfResult) error {
	result.Samples = nil
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal perf summary: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write perf summary: %w", err)
	}
	return nil
}

func renderPerfScorecard(result PerfResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# mcb Live Performance Benchmark\n\n")
	fmt.Fprintf(&b, "- url: `%s`\n", result.URL)
	fmt.Fprintf(&b, "- project: `%s`\n", result.Project)
	fmt.Fprintf(&b, "- run_id: `%s`\n", result.RunID)
	fmt.Fprintf(&b, "- requests per endpoint: `%d`\n\n", result.Requests)
	fmt.Fprintf(&b, "| Group | Endpoint | Concurrency | Requests | Errors | p50 | p90 | p95 | p99 | Max | RPS | Budget |\n")
	fmt.Fprintf(&b, "|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|\n")
	for _, summary := range result.Summaries {
		budget := "ok"
		if summary.BudgetStatus == "miss" {
			budget = "miss: " + strings.Join(summary.BudgetMissReasons, ", ")
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %.3f ms | %.3f ms | %.3f ms | %.3f ms | %.3f ms | %.1f | %s |\n", summary.Group, summary.Name, summary.Concurrency, summary.Requests, summary.Errors, summary.P50MS, summary.P90MS, summary.P95MS, summary.P99MS, summary.MaxMS, summary.RPS, budget)
	}
	if len(result.BudgetMisses) > 0 {
		fmt.Fprintf(&b, "\n## Budget Misses\n\n")
		for _, miss := range result.BudgetMisses {
			fmt.Fprintf(&b, "- %s\n", miss)
		}
	}
	return b.String()
}

func percentileFloat(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

func percentileInt(values []int, p float64) int {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int(nil), values...)
	sort.Ints(sorted)
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

func perfSessionID(project, runID string, i int) string {
	return store.PerfSessionExternalID(project, runID, i%100)
}

func perfLiveSessionID(project, runID, kind string, i int) string {
	return fmt.Sprintf("%s-%s-%06d", store.PerfSessionExternalID(project, runID, 0), kind, i)
}

func sessionBody(project, runID string, i int) any {
	return map[string]any{"session_id": perfSessionID(project, runID, i), "cwd": project}
}
func replayBody(project, runID string, i int) any {
	return map[string]any{"session_id": "opencode:" + perfSessionID(project, runID, i), "limit": 1000}
}
func enrichBody(project, runID string, i int) any {
	return map[string]any{"session_id": perfSessionID(project, runID, i), "cwd": project, "files": []string{"README.md", "internal/server/server.go", fmt.Sprintf("src/module_%03d.go", i%200)}}
}
func opencodeEventBody(project, runID string, i int) any {
	return map[string]any{"session_id": perfLiveSessionID(project, runID, "event", i), "cwd": project, "kind": "session_status", "payload": map[string]any{"status_type": "idle", "i": i}}
}
func opencodeToolBody(project, runID string, i int) any {
	return map[string]any{"session_id": perfLiveSessionID(project, runID, "tool", i), "cwd": project, "tool": "Read", "input": map[string]any{"file": "README.md"}, "output": map[string]any{"ok": true, "i": i}}
}
func opencodeChatBody(project, runID string, i int) any {
	return map[string]any{"session_id": perfLiveSessionID(project, runID, "chat", i), "cwd": project, "message": fmt.Sprintf("perf chat message %d about jwt sqlite docker", i)}
}
func claudePostToolBody(project, runID string, i int) any {
	return map[string]any{"session_id": perfLiveSessionID(project, runID, "claude-tool", i), "cwd": project, "tool_name": "Read", "tool_input": map[string]any{"file_path": "README.md"}, "tool_response": map[string]any{"ok": true}}
}
func claudeUserPromptBody(project, runID string, i int) any {
	return map[string]any{"session_id": perfLiveSessionID(project, runID, "claude-prompt", i), "cwd": project, "prompt": fmt.Sprintf("perf prompt %d about memory", i)}
}
func claudeStopBody(project, runID string, i int) any {
	return map[string]any{"session_id": perfSessionID(project, runID, i), "cwd": project, "stop_hook_active": false}
}

func mcpCall(name string, arguments map[string]any, i int) any {
	return map[string]any{"jsonrpc": "2.0", "id": i + 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": arguments}}
}
func mcpMemorySearchBody(project, runID string, i int) any {
	return mcpCall("memory_search", map[string]any{"project": project, "query": "jwt sqlite docker ollama", "limit": 10}, i)
}
func mcpMemoryRecallBody(project, runID string, i int) any {
	return mcpCall("memory_recall", map[string]any{"project": project, "query": "docker compose embeddings", "limit": 10}, i)
}
func mcpMemorySaveBody(project, runID string, i int) any {
	return mcpCall("memory_save", map[string]any{"project": project, "text": fmt.Sprintf("perf live saved memory %06d: benchmark fact for jwt sqlite docker", i), "tier": "semantic"}, i)
}
func mcpMemorySessionObservationsBody(project, runID string, i int) any {
	return mcpCall("memory_session_observations", map[string]any{"session_id": "opencode:" + perfSessionID(project, runID, i), "limit": 100}, i)
}
