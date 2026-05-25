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

func TestNormalizeClaudePreTool(t *testing.T) {
	raw := []byte(`{"session_id":"s1","cwd":"/repo","tool_name":"Read","tool_input":{"file_path":"a.go"}}`)
	e, err := NormalizeClaudePreTool(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if e.Agent != "claude-code" || e.ExternalSessionID != "s1" || e.CWD != "/repo" || e.Kind != "pre_tool_use" || e.Tool != "Read" {
		t.Fatalf("bad event: %+v", e)
	}
	var payload struct {
		ToolInput map[string]string `json:"tool_input"`
	}
	if err := json.Unmarshal(e.PayloadJSON, &payload); err != nil {
		t.Fatalf("parse payload_json: %v", err)
	}
	if got := payload.ToolInput["file_path"]; got != "a.go" {
		t.Fatalf("payload_json tool_input.file_path = %q", got)
	}
}

func TestNormalizeClaudePostToolFailure(t *testing.T) {
	raw := []byte(`{"session_id":"s1","cwd":"/repo","tool_name":"Read","tool_input":{"file_path":"a.go"},"tool_response":{"ok":true}}`)
	e, err := NormalizeClaudePostToolFailure(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if e.Agent != "claude-code" || e.ExternalSessionID != "s1" || e.CWD != "/repo" || e.Kind != "tool_error" || e.Tool != "Read" {
		t.Fatalf("bad event: %+v", e)
	}
	assertPayloadJSONValue(t, e.PayloadJSON, "file_path")
}

func TestNormalizeClaudeLifecycleHooks(t *testing.T) {
	for _, tc := range []struct {
		name      string
		normalize func([]byte) (Event, error)
		body      string
		kind      string
	}{
		{name: "pre compact", normalize: NormalizeClaudePreCompact, body: `{"session_id":"s1","cwd":"/repo","trigger":"manual"}`, kind: "pre_compact"},
		{name: "subagent start", normalize: NormalizeClaudeSubagentStart, body: `{"session_id":"s1","cwd":"/repo","subagent_name":"mcb-compactor"}`, kind: "subagent_start"},
		{name: "notification", normalize: NormalizeClaudeNotification, body: `{"session_id":"s1","cwd":"/repo","message":"permission needed"}`, kind: "notification"},
		{name: "task completed", normalize: NormalizeClaudeTaskCompleted, body: `{"session_id":"s1","cwd":"/repo","task":"tests","status":"completed"}`, kind: "task_completed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, err := tc.normalize([]byte(tc.body))
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if e.Agent != "claude-code" || e.ExternalSessionID != "s1" || e.CWD != "/repo" || e.Kind != tc.kind {
				t.Fatalf("bad event: %+v", e)
			}
			if !json.Valid(e.PayloadJSON) {
				t.Fatalf("payload is not json: %s", e.PayloadJSON)
			}
		})
	}
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

func TestNormalizeOpenCodeEvent(t *testing.T) {
	raw := []byte(`{"session_id":"o1","cwd":"/repo","kind":"session_status","tool":"","payload":{"status_type":"idle"}}`)
	e, err := NormalizeOpenCodeEvent(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if e.Agent != "opencode" || e.ExternalSessionID != "o1" || e.CWD != "/repo" || e.Kind != "session_status" || e.Tool != "" {
		t.Fatalf("bad event: %+v", e)
	}
	var payload struct {
		StatusType string `json:"status_type"`
	}
	if err := json.Unmarshal(e.PayloadJSON, &payload); err != nil {
		t.Fatalf("parse payload_json: %v", err)
	}
	if payload.StatusType != "idle" {
		t.Fatalf("status_type = %q", payload.StatusType)
	}
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
