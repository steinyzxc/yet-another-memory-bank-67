package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func TestMemoryReplayReturnsOrderedRedactedEvents(t *testing.T) {
	s := openTestStore(t)
	_, _ = s.InsertObservation(context.Background(), store.ObservationInput{Agent: "claude-code", ExternalSessionID: "s1", CWD: "/repo", TS: 2000, Kind: "tool_use", Tool: "Bash", PayloadJSON: []byte(`{"token":"ghp_abcdefghijklmnopqrstuvwxyzABCDEFGHIJ"}`), Hash: "h2"}, 300)
	_, _ = s.InsertObservation(context.Background(), store.ObservationInput{Agent: "claude-code", ExternalSessionID: "s1", CWD: "/repo", TS: 1000, Kind: "assistant_message", PayloadJSON: []byte(`{"text":"done"}`), Hash: "h1"}, 300)
	h := New(s, Options{})

	result := callTool(t, h, "memory_replay", map[string]any{"session_id": "claude-code:s1", "limit": 10})
	if bytes.Contains(result, []byte("ghp_abcdefghijklmnopqrstuvwxyzABCDEFGHIJ")) {
		t.Fatalf("result leaked secret: %s", string(result))
	}
	var body struct {
		Events []struct {
			ID             string          `json:"id"`
			Timestamp      int64           `json:"timestamp"`
			Actor          string          `json:"actor"`
			Type           string          `json:"type"`
			Tool           string          `json:"tool"`
			PayloadPreview string          `json:"payload_preview"`
			PayloadDetail  json.RawMessage `json:"payload_detail"`
		} `json:"events"`
	}
	mustUnmarshal(t, result, &body)
	if len(body.Events) != 2 {
		t.Fatalf("events = %+v", body.Events)
	}
	if body.Events[0].Timestamp != 1000 || body.Events[0].Actor != "assistant" || body.Events[0].Type != "assistant_message" {
		t.Fatalf("first event = %+v", body.Events[0])
	}
	if body.Events[1].Tool != "Bash" || body.Events[1].Actor != "tool" || !bytes.Contains(body.Events[1].PayloadDetail, []byte("[REDACTED]")) || body.Events[1].PayloadPreview == "" {
		t.Fatalf("second event = %+v", body.Events[1])
	}
}
