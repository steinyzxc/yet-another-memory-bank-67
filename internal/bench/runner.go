package bench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func RunCodingLife(ctx context.Context, opts RunOptions) (RunResult, error) {
	return runCorpus(ctx, CodingLifeCorpus(), opts)
}

func RunLongMemEval(ctx context.Context, opts RunOptions) (RunResult, error) {
	entries, err := LoadLongMemEvalEntries(opts.Dataset)
	if err != nil {
		return RunResult{}, err
	}
	metadata, err := loadDatasetMetadata(opts.Dataset, len(entries))
	if err != nil {
		return RunResult{}, err
	}
	if opts.Limit > 0 && opts.Limit < len(entries) {
		entries = entries[:opts.Limit]
	}
	metadata.EvaluatedRows = len(entries)
	return runLongMemEvalEntries(ctx, entries, opts, metadata)
}

func runLongMemEvalEntries(ctx context.Context, entries []LongMemEvalEntry, opts RunOptions, metadata *DatasetMetadata) (RunResult, error) {
	if opts.OutDir == "" {
		opts.OutDir = filepath.Join("benchmark-results", time.Now().Format("20060102-150405"))
	}
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return RunResult{}, fmt.Errorf("create output dir: %w", err)
	}
	workDir, err := os.MkdirTemp(opts.OutDir, "longmemeval-work-")
	if err != nil {
		return RunResult{}, fmt.Errorf("create longmemeval work dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	queries := make([]QueryResult, 0, len(entries))
	for i, entry := range entries {
		s, err := store.Open(ctx, filepath.Join(workDir, fmt.Sprintf("q-%04d.db", i)))
		if err != nil {
			return RunResult{}, fmt.Errorf("open question sandbox: %w", err)
		}
		if err := seedLongMemEvalEntry(ctx, s, entry); err != nil {
			s.Close()
			return RunResult{}, err
		}
		adapter := BM25Adapter{Store: s, Project: BenchProject}
		start := time.Now()
		found, err := adapter.Search(ctx, entry.Question, 20)
		s.Close()
		if err != nil {
			return RunResult{}, fmt.Errorf("query %s: %w", entry.QuestionID, err)
		}
		ranked := make([]string, 0, len(found))
		for _, result := range found {
			ranked = append(ranked, result.ID)
		}
		latency := time.Since(start).Milliseconds()
		metrics := AggregateMetrics{RAt5: HitAt(ranked, entry.AnswerSessionIDs, 5), RAt10: HitAt(ranked, entry.AnswerSessionIDs, 10), RAt20: HitAt(ranked, entry.AnswerSessionIDs, 20), HitAt5: HitAt(ranked, entry.AnswerSessionIDs, 5), MRR: MRR(ranked, entry.AnswerSessionIDs), NDCGAt10: NDCGAt(ranked, entry.AnswerSessionIDs, 10), P50LatencyMS: latency, P95LatencyMS: latency}
		queries = append(queries, QueryResult{QueryID: entry.QuestionID, Query: entry.Question, Type: entry.QuestionType, GoldIDs: entry.AnswerSessionIDs, RankedIDs: ranked, LatencyMS: latency, Metrics: metrics})
	}
	corpus := Corpus{Name: "longmemeval-s-user-supplied", Version: "native", Notes: "Native LongMemEval-S JSON. Fresh index per question.", Queries: make([]QueryFixture, 0, len(entries))}
	result := RunResult{BenchName: "longmemeval", Adapter: "mcb-bm25", Corpus: corpus, Dataset: metadata, Queries: queries, Metrics: aggregate(queries), ByType: aggregateByType(queries), Reference: AgentMemoryReference("longmemeval")}
	if err := WriteReports(opts.OutDir, result); err != nil {
		return RunResult{}, err
	}
	return result, nil
}

func loadDatasetMetadata(path string, rows int) (*DatasetMetadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open dataset for metadata: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return nil, fmt.Errorf("hash dataset: %w", err)
	}
	name := filepath.Base(path)
	metadata := &DatasetMetadata{Name: name, SHA256: hex.EncodeToString(hash.Sum(nil)), Bytes: size, Rows: rows, EvaluatedRows: rows}
	if name == "longmemeval_s_cleaned.json" {
		metadata.Source = "https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned/resolve/main/longmemeval_s_cleaned.json"
	}
	return metadata, nil
}

func seedLongMemEvalEntry(ctx context.Context, s *store.Store, entry LongMemEvalEntry) error {
	for i, sessionID := range entry.HaystackSessionIDs {
		text := flattenLongMemEvalSession(entry.HaystackSessions[i])
		_, err := s.AddMemory(ctx, store.MemoryInput{Project: BenchProject, Text: text, Tier: "episodic", Source: "bench:" + sessionID, CreatedAt: int64(i + 1)})
		if err != nil {
			return fmt.Errorf("seed longmemeval session %s: %w", sessionID, err)
		}
	}
	return nil
}

func flattenLongMemEvalSession(turns []LMETurn) string {
	parts := make([]string, 0, len(turns))
	for _, turn := range turns {
		parts = append(parts, fmt.Sprintf("%s: %s", turn.Role, turn.Content))
	}
	return strings.Join(parts, "\n")
}

func runCorpus(ctx context.Context, corpus Corpus, opts RunOptions) (RunResult, error) {
	if opts.OutDir == "" {
		opts.OutDir = filepath.Join("benchmark-results", time.Now().Format("20060102-150405"))
	}
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return RunResult{}, fmt.Errorf("create output dir: %w", err)
	}
	dbPath := filepath.Join(opts.OutDir, "sandbox.db")
	s, err := store.Open(ctx, dbPath)
	if err != nil {
		return RunResult{}, fmt.Errorf("open sandbox db: %w", err)
	}
	defer s.Close()
	if err := seedCorpus(ctx, s, corpus, opts.now()); err != nil {
		return RunResult{}, err
	}
	adapter := BM25Adapter{Store: s, Project: BenchProject}
	result, err := runQueries(ctx, corpus, adapter)
	if err != nil {
		return RunResult{}, err
	}
	result.Reference = AgentMemoryReference(result.BenchName)
	if err := WriteReports(opts.OutDir, result); err != nil {
		return RunResult{}, err
	}
	return result, nil
}

func seedCorpus(ctx context.Context, s *store.Store, corpus Corpus, now int64) error {
	for i, memory := range corpus.Memories {
		tier := memory.Tier
		if tier == "" {
			tier = "semantic"
		}
		_, err := s.AddMemory(ctx, store.MemoryInput{Project: BenchProject, Text: memory.Text, Tier: tier, Source: "bench:" + memory.ID, CreatedAt: now + int64(i)})
		if err != nil {
			return fmt.Errorf("seed memory %s: %w", memory.ID, err)
		}
	}
	return nil
}

func runQueries(ctx context.Context, corpus Corpus, adapter Adapter) (RunResult, error) {
	results := make([]QueryResult, 0, len(corpus.Queries))
	for _, query := range corpus.Queries {
		start := time.Now()
		found, err := adapter.Search(ctx, query.Text, 10)
		if err != nil {
			return RunResult{}, fmt.Errorf("query %s: %w", query.ID, err)
		}
		ranked := make([]string, 0, len(found))
		for _, item := range found {
			ranked = append(ranked, item.ID)
		}
		metrics := AggregateMetrics{PAt5: PrecisionAt(ranked, query.GoldIDs, 5), RAt5: RecallAt(ranked, query.GoldIDs, 5), RAt10: RecallAt(ranked, query.GoldIDs, 10), RAt20: RecallAt(ranked, query.GoldIDs, 20), HitAt5: HitAt(ranked, query.GoldIDs, 5), MRR: MRR(ranked, query.GoldIDs), NDCGAt10: NDCGAt(ranked, query.GoldIDs, 10)}
		latency := time.Since(start).Milliseconds()
		metrics.P50LatencyMS = latency
		metrics.P95LatencyMS = latency
		results = append(results, QueryResult{QueryID: query.ID, Query: query.Text, Type: query.Type, GoldIDs: query.GoldIDs, RankedIDs: ranked, LatencyMS: latency, Metrics: metrics})
	}
	name := "coding-life"
	if corpus.Name != "coding-life-cleanroom" {
		name = corpus.Name
	}
	return RunResult{BenchName: name, Adapter: adapter.Name(), Corpus: corpus, Queries: results, Metrics: aggregate(results), ByType: aggregateByType(results), Reference: AgentMemoryReference(name)}, nil
}

func (o RunOptions) now() int64 {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now().UnixMilli()
}
