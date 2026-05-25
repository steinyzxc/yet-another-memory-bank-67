//go:build sqlite_fts5

package mcp

import (
	"context"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func TestMemoryRecallTouchesResultsButSearchDoesNot(t *testing.T) {
	s := openTestStore(t)
	id, err := s.AddMemory(context.Background(), store.MemoryInput{Project: "/repo", Text: "jwt middleware validates bearer tokens", CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}
	h := New(s, Options{Now: func() int64 { return 9000 }})

	searchResult := callTool(t, h, "memory_search", map[string]any{"query": "jwt middleware", "project": "/repo", "limit": 5})
	assertMemoryResult(t, searchResult, id)
	afterSearch, err := s.Memory(context.Background(), id)
	if err != nil {
		t.Fatalf("load after search: %v", err)
	}
	if afterSearch.AccessedAt != 1000 {
		t.Fatalf("memory_search touched accessed_at: %+v", afterSearch)
	}

	recallResult := callTool(t, h, "memory_recall", map[string]any{"query": "jwt middleware", "project": "/repo", "limit": 5})
	assertMemoryResult(t, recallResult, id)
	afterRecall, err := s.Memory(context.Background(), id)
	if err != nil {
		t.Fatalf("load after recall: %v", err)
	}
	if afterRecall.AccessedAt != 9000 {
		t.Fatalf("memory_recall did not touch accessed_at: %+v", afterRecall)
	}
}

func TestMemoryForgetDryRunRequiresExplicitDeleteConfirmation(t *testing.T) {
	s := openTestStore(t)
	id, err := s.AddMemory(context.Background(), store.MemoryInput{Project: "/repo", Text: "forget this jwt detail", CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}
	h := New(s, Options{})

	dryRunResult := callTool(t, h, "memory_forget", map[string]any{"query": "jwt detail", "project": "/repo", "dry_run": true})
	assertMemoryResult(t, dryRunResult, id)
	if _, err := s.Memory(context.Background(), id); err != nil {
		t.Fatalf("dry-run deleted memory: %v", err)
	}

	resp := postRPC(t, h, rpc(t, "tools/call", map[string]any{"name": "memory_forget", "arguments": map[string]any{"ids": []int64{id}}}))
	if resp.Error == nil {
		t.Fatalf("delete without confirm succeeded: %s", string(resp.Result))
	}
	if _, err := s.Memory(context.Background(), id); err != nil {
		t.Fatalf("unconfirmed delete removed memory: %v", err)
	}
}

func assertMemoryResult(t *testing.T, data []byte, id int64) {
	t.Helper()
	var result struct {
		Memories []struct {
			ID int64 `json:"id"`
		} `json:"memories"`
		Candidates []struct {
			ID int64 `json:"id"`
		} `json:"candidates"`
	}
	mustUnmarshal(t, data, &result)
	memories := result.Memories
	if len(memories) == 0 {
		memories = result.Candidates
	}
	if len(memories) == 0 || memories[0].ID != id {
		t.Fatalf("result = %+v, want first id %d", result, id)
	}
}
