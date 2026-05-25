package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func TestResourcesListAndReadStatusAndProjectProfile(t *testing.T) {
	s := openTestStore(t)
	_, err := s.AddMemory(context.Background(), store.MemoryInput{Project: "/repo", Text: "profile fact", CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}
	h := New(s, Options{})

	listResp := postRPC(t, h, rpc(t, "resources/list", nil))
	if listResp.Error != nil {
		t.Fatalf("resources/list error = %+v", listResp.Error)
	}
	var list struct {
		Resources []struct {
			URI  string `json:"uri"`
			Name string `json:"name"`
		} `json:"resources"`
	}
	mustUnmarshal(t, listResp.Result, &list)
	if len(list.Resources) < 2 || list.Resources[0].URI == "" {
		t.Fatalf("resources = %+v", list.Resources)
	}

	statusResp := postRPC(t, h, rpc(t, "resources/read", map[string]any{"uri": "mcb://status"}))
	if statusResp.Error != nil {
		t.Fatalf("status read error = %+v", statusResp.Error)
	}
	if !strings.Contains(string(statusResp.Result), "memory_count") {
		t.Fatalf("status result = %s", string(statusResp.Result))
	}

	profileResp := postRPC(t, h, rpc(t, "resources/read", map[string]any{"uri": "mcb://project//repo/profile"}))
	if profileResp.Error != nil {
		t.Fatalf("profile read error = %+v", profileResp.Error)
	}
	if !strings.Contains(string(profileResp.Result), "profile fact") && !strings.Contains(string(profileResp.Result), "memory_count") {
		t.Fatalf("profile result = %s", string(profileResp.Result))
	}
}

func TestMemoryUpdateToolAndAllProjectProfile(t *testing.T) {
	s := openTestStore(t)
	id, err := s.AddMemory(context.Background(), store.MemoryInput{Project: "/repo", Text: "old text", Tier: "semantic", Importance: 0.2, CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}
	_, _ = s.AddMemory(context.Background(), store.MemoryInput{Project: "/other", Text: "other text", CreatedAt: 2000})
	h := New(s, Options{Now: func() int64 { return 3000 }})

	updateResult := callTool(t, h, "memory_update", map[string]any{"id": id, "text": "new text", "tier": "procedural", "importance": 0.8})
	var updated struct {
		Updated bool `json:"updated"`
	}
	mustUnmarshal(t, updateResult, &updated)
	if !updated.Updated {
		t.Fatalf("update result = %+v", updated)
	}
	memory, err := s.Memory(context.Background(), id)
	if err != nil {
		t.Fatalf("load memory: %v", err)
	}
	if memory.Text != "new text" || memory.Tier != "procedural" || memory.Importance != 0.8 || memory.UpdatedAt != 3000 {
		t.Fatalf("memory = %+v", memory)
	}

	profileResult := callTool(t, h, "memory_profile", map[string]any{})
	var profile struct {
		Projects []struct {
			Project     string `json:"project"`
			MemoryCount int64  `json:"memory_count"`
		} `json:"projects"`
	}
	mustUnmarshal(t, profileResult, &profile)
	if len(profile.Projects) != 2 {
		encoded, _ := json.Marshal(profile)
		t.Fatalf("profile = %s", string(encoded))
	}
}
