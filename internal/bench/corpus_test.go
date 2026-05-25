package bench

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLongMemEvalEntriesSupportsJSONLAndFiltersAbstentions(t *testing.T) {
	dir := t.TempDir()
	dataset := filepath.Join(dir, "longmemeval.jsonl")
	data := `{"question_id":"keep","question_type":"single-session-user","question":"where are notes","answer_session_ids":["s1"],"haystack_session_ids":["s1"],"haystack_sessions":[[{"role":"user","content":"notes are in sqlite"}]]}
{"question_id":"drop","question_type":"multi-session_abs","question":"missing fact","answer_session_ids":["s2"],"haystack_session_ids":["s2"],"haystack_sessions":[[{"role":"user","content":"irrelevant"}]]}
`
	if err := os.WriteFile(dataset, []byte(data), 0o600); err != nil {
		t.Fatalf("write dataset: %v", err)
	}

	entries, err := LoadLongMemEvalEntries(dataset)
	if err != nil {
		t.Fatalf("load entries: %v", err)
	}
	if len(entries) != 1 || entries[0].QuestionID != "keep" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestLoadLongMemEvalEntriesRejectsMismatchedHaystack(t *testing.T) {
	dir := t.TempDir()
	dataset := filepath.Join(dir, "longmemeval.json")
	data := `[{"question_id":"bad","question_type":"single-session-user","question":"where","answer_session_ids":["s1"],"haystack_session_ids":["s1","s2"],"haystack_sessions":[[{"role":"user","content":"notes"}]]}]`
	if err := os.WriteFile(dataset, []byte(data), 0o600); err != nil {
		t.Fatalf("write dataset: %v", err)
	}

	if _, err := LoadLongMemEvalEntries(dataset); err == nil {
		t.Fatal("expected mismatched haystack error")
	}
}
