package bench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func TestRunCodingLifeUsesSandboxDatabase(t *testing.T) {
	ctx := context.Background()
	defaultDB := filepath.Join(t.TempDir(), "default.db")
	defaultStore, err := store.Open(ctx, defaultDB)
	if err != nil {
		t.Fatalf("open default store: %v", err)
	}
	_, err = defaultStore.AddMemory(ctx, store.MemoryInput{Project: "/repo", Text: "do not touch", CreatedAt: 1000})
	if err != nil {
		t.Fatalf("seed default store: %v", err)
	}
	defaultStore.Close()

	out := t.TempDir()
	result, err := RunCodingLife(ctx, RunOptions{OutDir: out, Now: func() int64 { return 10000 }})
	if err != nil {
		t.Fatalf("run coding life: %v", err)
	}
	if result.BenchName != "coding-life" || len(result.Queries) == 0 {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(out, "sandbox.db")); err != nil {
		t.Fatalf("sandbox db missing: %v", err)
	}

	reopened, err := store.Open(ctx, defaultDB)
	if err != nil {
		t.Fatalf("reopen default store: %v", err)
	}
	defer reopened.Close()
	memories, err := reopened.RecentMemories(ctx, "/repo", 10)
	if err != nil {
		t.Fatalf("recent memories: %v", err)
	}
	if len(memories) != 1 || memories[0].Text != "do not touch" {
		t.Fatalf("default db was touched: %+v", memories)
	}
}

func TestRunLongMemEvalUsesNativeDataset(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dataset := filepath.Join(dir, "longmemeval.json")
	data := `[
	{"question_id":"q1","question_type":"single-session-user","question":"where are notes stored","answer_session_ids":["s1"],"haystack_session_ids":["s1","s2"],"haystack_sessions":[[{"role":"user","content":"project notes are stored in sqlite"}],[{"role":"user","content":"deploys use docker compose"}]]},
	{"question_id":"q2","question_type":"multi-session","question":"what uses docker compose","answer_session_ids":["s4"],"haystack_session_ids":["s3","s4"],"haystack_sessions":[[{"role":"user","content":"sqlite backs local search"}],[{"role":"assistant","content":"docker compose starts ollama"}]]}
	]`
	if err := os.WriteFile(dataset, []byte(data), 0o600); err != nil {
		t.Fatalf("write dataset: %v", err)
	}

	result, err := RunLongMemEval(ctx, RunOptions{OutDir: filepath.Join(dir, "out"), Dataset: dataset, Limit: 1})
	if err != nil {
		t.Fatalf("run longmemeval: %v", err)
	}
	if result.BenchName != "longmemeval" || len(result.Queries) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if result.Metrics.RAt5 != 1 || result.Metrics.RAt20 != 1 || result.Metrics.MRR != 1 {
		t.Fatalf("metrics = %+v", result.Metrics)
	}
	if _, ok := result.ByType["single-session-user"]; !ok {
		t.Fatalf("missing type breakdown: %+v", result.ByType)
	}

	summaryData, err := os.ReadFile(filepath.Join(dir, "out", "summary.json"))
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	var summary SummaryReport
	if err := json.Unmarshal(summaryData, &summary); err != nil {
		t.Fatalf("parse summary: %v", err)
	}
	if summary.Reference.NDCGAt10 == 0 || summary.ByType["single-session-user"].RAt20 != 1 || summary.Dataset == nil || summary.Dataset.Rows != 2 || summary.Dataset.EvaluatedRows != 1 {
		t.Fatalf("summary = %+v", summary)
	}
}
