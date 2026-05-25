package bench

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func CodingLifeCorpus() Corpus {
	return Corpus{
		Name:    "coding-life-cleanroom",
		Version: "v1",
		Notes:   "Clean-room coding-agent retrieval corpus. It is not copied from agentmemory coding-agent-life-v1.",
		Memories: []MemoryFixture{
			{ID: "auth-jwt", Text: "JWT auth uses jose middleware in src/middleware/auth.ts and validates bearer tokens in auth.test.ts", Tier: "semantic"},
			{ID: "rate-limit", Text: "Rate limiting is enforced per API key with a token bucket in src/middleware/rate_limit.ts", Tier: "semantic"},
			{ID: "db-n-plus-one", Text: "N+1 query regression was fixed by preloading project memberships before rendering dashboard rows", Tier: "semantic"},
			{ID: "sqlite-fts", Text: "Use SQLite FTS5 BM25 lexical search for local memory recall before optional vector fusion", Tier: "procedural"},
			{ID: "docker-ollama", Text: "Docker compose Ollama overlay reaches embeddings at http://ollama:11434 with nomic-embed-text", Tier: "semantic"},
			{ID: "compactor", Text: "mcb compactor must call memory_search for deduplication, save one summary, then save durable facts with session_id", Tier: "procedural"},
		},
		Queries: []QueryFixture{
			{ID: "q-auth", Text: "where is bearer token auth validated", Type: "file_fact", GoldIDs: []string{"auth-jwt"}},
			{ID: "q-rate", Text: "api key token bucket rate limiting", Type: "decision", GoldIDs: []string{"rate-limit"}},
			{ID: "q-db", Text: "dashboard membership n plus one query", Type: "bug", GoldIDs: []string{"db-n-plus-one"}},
			{ID: "q-search", Text: "local bm25 memory recall", Type: "architecture", GoldIDs: []string{"sqlite-fts"}},
			{ID: "q-ollama", Text: "ollama docker embedding url", Type: "deployment", GoldIDs: []string{"docker-ollama"}},
			{ID: "q-compact", Text: "compactor dedup session_id facts summary", Type: "workflow", GoldIDs: []string{"compactor"}},
		},
	}
}

type LongMemEvalEntry struct {
	QuestionID         string      `json:"question_id"`
	QuestionType       string      `json:"question_type"`
	Question           string      `json:"question"`
	AnswerSessionIDs   []string    `json:"answer_session_ids"`
	HaystackSessionIDs []string    `json:"haystack_session_ids"`
	HaystackSessions   [][]LMETurn `json:"haystack_sessions"`
}

type LMETurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func LoadLongMemEvalEntries(path string) ([]LongMemEvalEntry, error) {
	if path == "" {
		return nil, fmt.Errorf("longmemeval requires --dataset PATH")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read longmemeval dataset: %w", err)
	}
	entries, err := parseLongMemEvalEntries(data)
	if err != nil {
		return nil, fmt.Errorf("parse longmemeval dataset: %w", err)
	}
	filtered := entries[:0]
	for _, entry := range entries {
		if isAbstentionType(entry.QuestionType) {
			continue
		}
		if entry.QuestionID == "" || entry.Question == "" || len(entry.AnswerSessionIDs) == 0 {
			return nil, fmt.Errorf("longmemeval row %q is missing question or gold sessions", entry.QuestionID)
		}
		if len(entry.HaystackSessionIDs) != len(entry.HaystackSessions) {
			return nil, fmt.Errorf("longmemeval row %s: haystack_session_ids and haystack_sessions length mismatch", entry.QuestionID)
		}
		filtered = append(filtered, entry)
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("longmemeval dataset has no answerable rows after filtering abstentions")
	}
	return filtered, nil
}

func parseLongMemEvalEntries(data []byte) ([]LongMemEvalEntry, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty dataset")
	}
	var entries []LongMemEvalEntry
	if trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &entries); err != nil {
			return nil, err
		}
		return entries, nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(trimmed))
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var entry LongMemEvalEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func isAbstentionType(value string) bool {
	return strings.HasSuffix(value, "_abs")
}

func AgentMemoryReference(benchName string) Reference {
	switch benchName {
	case "coding-life", "coding-life-cleanroom":
		return Reference{Name: "agentmemory published reference", PAt5: 0.578, RAt5: 0.967, HitAt5Text: "15/15", P50MS: 14, Notes: "Published upstream coding-agent-life-v1 result. This mcb run uses a clean-room corpus, not the same corpus."}
	case "longmemeval", "longmemeval-s-user-supplied":
		return Reference{Name: "agentmemory BM25-only published reference", RAt5: 0.862, RAt10: 0.946, RAt20: 0.986, MRR: 0.715, NDCGAt10: 0.730, Notes: "Same-adapter published LongMemEval-S reference. BM25+Vector reference: R@5 0.952, R@10 0.986, R@20 0.994, MRR 0.882, NDCG@10 0.879."}
	default:
		return Reference{Name: "agentmemory published reference", Notes: "No published reference configured for this benchmark."}
	}
}
