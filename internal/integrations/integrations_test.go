package integrations

import (
	"encoding/json"
	"testing"
)

func TestNormalizeClaudePostTool(t *testing.T) {
	raw := []byte(`{"session_id":"s1","cwd":"/repo","tool_name":"Read","tool_input":{"file_path":"a.go"},"tool_response":{"ok":true}}`)
	e, err := NormalizeClaudePostTool(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if e.Agent != "claude-code" || e.ExternalSessionID != "s1" || e.CWD != "/repo" || e.Kind != "tool_use" || e.Tool != "Read" {
		t.Fatalf("bad event: %+v", e)
	}
	assertPayloadJSONValue(t, e.PayloadJSON, "file_path")
}

func TestNormalizeOpenCodeTool(t *testing.T) {
	raw := []byte(`{"session_id":"o1","cwd":"/repo","tool":"read","input":{"file":"a.go"},"output":{"ok":true}}`)
	e, err := NormalizeOpenCodeTool(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if e.Agent != "opencode" || e.ExternalSessionID != "o1" || e.CWD != "/repo" || e.Kind != "tool_use" || e.Tool != "read" {
		t.Fatalf("bad event: %+v", e)
	}
	assertPayloadJSONValue(t, e.PayloadJSON, "file")
}

func TestNormalizeClaudeUserPrompt(t *testing.T) {
	raw := []byte(`{"session_id":"s1","cwd":"/repo","prompt":"remember this"}`)
	e, err := NormalizeClaudeUserPrompt(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if e.Agent != "claude-code" || e.ExternalSessionID != "s1" || e.CWD != "/repo" || e.Kind != "user_message" || e.Tool != "" {
		t.Fatalf("bad event: %+v", e)
	}
	var payload struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(e.PayloadJSON, &payload); err != nil {
		t.Fatalf("parse payload_json: %v", err)
	}
	if payload.Prompt != "remember this" {
		t.Fatalf("prompt = %q", payload.Prompt)
	}
}

func TestNormalizeOpenCodeChat(t *testing.T) {
	raw := []byte(`{"session_id":"o1","cwd":"/repo","message":"remember this too"}`)
	e, err := NormalizeOpenCodeChat(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if e.Agent != "opencode" || e.ExternalSessionID != "o1" || e.CWD != "/repo" || e.Kind != "user_message" || e.Tool != "" {
		t.Fatalf("bad event: %+v", e)
	}
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(e.PayloadJSON, &payload); err != nil {
		t.Fatalf("parse payload_json: %v", err)
	}
	if payload.Message != "remember this too" {
		t.Fatalf("message = %q", payload.Message)
	}
}

func assertPayloadJSONValue(t *testing.T, payloadJSON json.RawMessage, fileKey string) {
	t.Helper()

	var payload struct {
		ToolInput    map[string]string `json:"tool_input"`
		ToolResponse map[string]bool   `json:"tool_response"`
	}
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Fatalf("parse payload_json: %v", err)
	}
	if got := payload.ToolInput[fileKey]; got != "a.go" {
		t.Fatalf("payload_json tool_input.%s = %q, want %q: %s", fileKey, got, "a.go", payloadJSON)
	}
	if got := payload.ToolResponse["ok"]; got != true {
		t.Fatalf("payload_json tool_response.ok = %t, want true: %s", got, payloadJSON)
	}
}
