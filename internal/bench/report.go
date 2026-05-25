package bench

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func WriteReports(outDir string, result RunResult) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create report dir: %w", err)
	}
	if err := writeRaw(filepath.Join(outDir, "raw.ndjson"), result.Queries); err != nil {
		return err
	}
	if err := writeSummary(filepath.Join(outDir, "summary.json"), result); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "scorecard.md"), []byte(renderScorecard(result)), 0o644); err != nil {
		return fmt.Errorf("write scorecard: %w", err)
	}
	return nil
}

func writeRaw(path string, queries []QueryResult) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create raw report: %w", err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, query := range queries {
		data, err := json.Marshal(query)
		if err != nil {
			return fmt.Errorf("marshal raw query: %w", err)
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		if err := w.WriteByte('\n'); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush raw report: %w", err)
	}
	return nil
}

func writeSummary(path string, result RunResult) error {
	summary := SummaryReport{BenchName: result.BenchName, Adapter: result.Adapter, Corpus: result.Corpus.Name, Version: result.Corpus.Version, Dataset: result.Dataset, Metrics: result.Metrics, ByType: result.ByType, Reference: result.Reference}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal summary: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}
	return nil
}

func renderScorecard(result RunResult) string {
	var b strings.Builder
	localPAt5 := fmt.Sprintf("%.3f", result.Metrics.PAt5)
	if result.BenchName == "longmemeval" {
		localPAt5 = "n/a"
	}
	fmt.Fprintf(&b, "# %s Benchmark Scorecard\n\n", result.BenchName)
	fmt.Fprintf(&b, "## mcb local run\n\n")
	fmt.Fprintf(&b, "| Adapter | P@5 | R@5 | R@10 | R@20 | Hit@5 | MRR | NDCG@10 | p50 latency | p95 latency |\n")
	fmt.Fprintf(&b, "|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	fmt.Fprintf(&b, "| %s | %s | %.3f | %.3f | %.3f | %.3f | %.3f | %.3f | %d ms | %d ms |\n\n", result.Adapter, localPAt5, result.Metrics.RAt5, result.Metrics.RAt10, result.Metrics.RAt20, result.Metrics.HitAt5, result.Metrics.MRR, result.Metrics.NDCGAt10, result.Metrics.P50LatencyMS, result.Metrics.P95LatencyMS)
	fmt.Fprintf(&b, "## %s\n\n", result.Reference.Name)
	fmt.Fprintf(&b, "| P@5 | R@5 | R@10 | R@20 | Hit@5 | MRR | NDCG@10 | p50 latency | Notes |\n")
	fmt.Fprintf(&b, "|---:|---:|---:|---:|---|---:|---:|---:|---|\n")
	fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n\n", metricText(result.Reference.PAt5), metricText(result.Reference.RAt5), metricText(result.Reference.RAt10), metricText(result.Reference.RAt20), textOrNA(result.Reference.HitAt5Text), metricText(result.Reference.MRR), metricText(result.Reference.NDCGAt10), latencyText(result.Reference.P50MS), result.Reference.Notes)
	if len(result.ByType) > 0 {
		fmt.Fprintf(&b, "## By Question Type\n\n")
		fmt.Fprintf(&b, "| Type | R@5 | R@10 | R@20 | MRR | NDCG@10 |\n")
		fmt.Fprintf(&b, "|---|---:|---:|---:|---:|---:|\n")
		for _, typ := range sortedBreakdownKeys(result.ByType) {
			metrics := result.ByType[typ]
			fmt.Fprintf(&b, "| %s | %.3f | %.3f | %.3f | %.3f | %.3f |\n", typ, metrics.RAt5, metrics.RAt10, metrics.RAt20, metrics.MRR, metrics.NDCGAt10)
		}
		fmt.Fprintf(&b, "\n")
	}
	fmt.Fprintf(&b, "## Methodology\n\n")
	fmt.Fprintf(&b, "- mcb local run was executed against corpus `%s` version `%s`.\n", result.Corpus.Name, result.Corpus.Version)
	if result.Dataset != nil {
		fmt.Fprintf(&b, "- Dataset `%s`: %d answerable rows, %d evaluated rows, %d bytes, sha256 `%s`.\n", result.Dataset.Name, result.Dataset.Rows, result.Dataset.EvaluatedRows, result.Dataset.Bytes, result.Dataset.SHA256)
		if result.Dataset.Source != "" {
			fmt.Fprintf(&b, "- Dataset source: %s.\n", result.Dataset.Source)
		}
	}
	fmt.Fprintf(&b, "- agentmemory published reference is not a same-run baseline.\n")
	if result.BenchName == "coding-life" {
		fmt.Fprintf(&b, "- not same corpus: this scorecard uses a clean-room corpus, not upstream coding-agent-life-v1.\n")
	} else if result.BenchName == "longmemeval" {
		fmt.Fprintf(&b, "- Methodology: fresh index per question from haystack sessions; abstention types excluded; R@K is recall_any@K.\n")
	}
	fmt.Fprintf(&b, "- No answer generation, no LLM judge, no server-side LLM calls.\n")
	return b.String()
}

func sortedBreakdownKeys(metrics map[string]AggregateMetrics) []string {
	keys := make([]string, 0, len(metrics))
	for key := range metrics {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func metricText(value float64) string {
	if value == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.3f", value)
}

func latencyText(value int64) string {
	if value == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%d ms", value)
}

func textOrNA(value string) string {
	if value == "" {
		return "n/a"
	}
	return value
}
