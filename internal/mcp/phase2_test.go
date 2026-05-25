package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func TestPhase2MCPToolsResourcesAndPrompts(t *testing.T) {
	s := openTestStore(t)
	_, _ = s.InsertObservation(context.Background(), store.ObservationInput{Agent: "claude-code", ExternalSessionID: "s1", CWD: "/repo", TS: 1000, Kind: "tool_use", Tool: "Read", PayloadJSON: []byte(`{"file_path":"a.go"}`), Hash: "h1"}, 300)
	memoryID, err := s.AddMemory(context.Background(), store.MemoryInput{Project: "/repo", Text: "a.go uses table-driven tests", SessionID: "claude-code:s1", CreatedAt: 1100})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}
	h := New(s, Options{DefaultProject: "/repo"})

	for _, name := range []string{"memory_timeline", "memory_file_history", "memory_patterns", "memory_export", "memory_audit", "memory_verify"} {
		t.Run(name, func(t *testing.T) {
			args := map[string]any{"project": "/repo", "limit": 10}
			if name == "memory_file_history" {
				args["files"] = []string{"a.go"}
			}
			if name == "memory_verify" {
				args = map[string]any{"id": memoryID}
			}
			result := callTool(t, h, name, args)
			if !strings.Contains(string(result), "a.go") && !strings.Contains(string(result), "memory") && !strings.Contains(string(result), "audit") {
				t.Fatalf("%s result = %s", name, string(result))
			}
		})
	}

	resourcesResp := postRPC(t, h, rpc(t, "resources/list", nil))
	if resourcesResp.Error != nil {
		t.Fatalf("resources/list error = %+v", resourcesResp.Error)
	}
	for _, uri := range []string{"mcb://memories/latest", "mcb://sessions/latest", "mcb://audit/latest"} {
		if !strings.Contains(string(resourcesResp.Result), uri) {
			t.Fatalf("resources missing %s: %s", uri, string(resourcesResp.Result))
		}
		readResp := postRPC(t, h, rpc(t, "resources/read", map[string]any{"uri": uri}))
		if readResp.Error != nil {
			t.Fatalf("read %s error = %+v", uri, readResp.Error)
		}
	}

	promptsResp := postRPC(t, h, rpc(t, "prompts/list", nil))
	if promptsResp.Error != nil {
		t.Fatalf("prompts/list error = %+v", promptsResp.Error)
	}
	if !strings.Contains(string(promptsResp.Result), "recall_context") || !strings.Contains(string(promptsResp.Result), "session_handoff") {
		t.Fatalf("prompts/list = %s", string(promptsResp.Result))
	}
	getResp := postRPC(t, h, rpc(t, "prompts/get", map[string]any{"name": "recall_context", "arguments": map[string]any{"query": "tests"}}))
	if getResp.Error != nil {
		t.Fatalf("prompts/get error = %+v", getResp.Error)
	}
	if !strings.Contains(string(getResp.Result), "tests") {
		t.Fatalf("prompt result = %s", string(getResp.Result))
	}
}
