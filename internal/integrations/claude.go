package integrations

import (
	"encoding/json"
	"fmt"
)

func NormalizeClaudePreTool(raw []byte) (Event, error) {
	var in struct {
		SessionID string          `json:"session_id"`
		CWD       string          `json:"cwd"`
		ToolName  string          `json:"tool_name"`
		ToolInput json.RawMessage `json:"tool_input"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return Event{}, fmt.Errorf("parse claude pre tool event: %w", err)
	}
	payload, err := json.Marshal(struct {
		ToolInput json.RawMessage `json:"tool_input"`
	}{ToolInput: in.ToolInput})
	if err != nil {
		return Event{}, fmt.Errorf("marshal claude pre tool payload: %w", err)
	}
	return Event{Agent: "claude-code", ExternalSessionID: in.SessionID, CWD: in.CWD, Kind: "pre_tool_use", Tool: in.ToolName, PayloadJSON: payload}, nil
}

func NormalizeClaudePostTool(raw []byte) (Event, error) {
	var in struct {
		SessionID    string          `json:"session_id"`
		CWD          string          `json:"cwd"`
		ToolName     string          `json:"tool_name"`
		ToolInput    json.RawMessage `json:"tool_input"`
		ToolResponse json.RawMessage `json:"tool_response"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return Event{}, fmt.Errorf("parse claude tool event: %w", err)
	}
	payload, err := json.Marshal(struct {
		ToolInput    json.RawMessage `json:"tool_input"`
		ToolResponse json.RawMessage `json:"tool_response"`
	}{
		ToolInput:    in.ToolInput,
		ToolResponse: in.ToolResponse,
	})
	if err != nil {
		return Event{}, fmt.Errorf("marshal claude payload: %w", err)
	}
	return Event{
		Agent:             "claude-code",
		ExternalSessionID: in.SessionID,
		CWD:               in.CWD,
		Kind:              "tool_use",
		Tool:              in.ToolName,
		PayloadJSON:       payload,
	}, nil
}

func NormalizeClaudePostToolFailure(raw []byte) (Event, error) {
	var in struct {
		SessionID    string          `json:"session_id"`
		CWD          string          `json:"cwd"`
		ToolName     string          `json:"tool_name"`
		ToolInput    json.RawMessage `json:"tool_input"`
		ToolResponse json.RawMessage `json:"tool_response"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return Event{}, fmt.Errorf("parse claude tool failure event: %w", err)
	}
	payload, err := json.Marshal(struct {
		ToolInput    json.RawMessage `json:"tool_input"`
		ToolResponse json.RawMessage `json:"tool_response"`
	}{
		ToolInput:    in.ToolInput,
		ToolResponse: in.ToolResponse,
	})
	if err != nil {
		return Event{}, fmt.Errorf("marshal claude tool failure payload: %w", err)
	}
	return Event{Agent: "claude-code", ExternalSessionID: in.SessionID, CWD: in.CWD, Kind: "tool_error", Tool: in.ToolName, PayloadJSON: payload}, nil
}

func NormalizeClaudeUserPrompt(raw []byte) (Event, error) {
	var in struct {
		SessionID string `json:"session_id"`
		CWD       string `json:"cwd"`
		Prompt    string `json:"prompt"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return Event{}, fmt.Errorf("parse claude user prompt: %w", err)
	}
	payload, err := json.Marshal(struct {
		Prompt string `json:"prompt"`
	}{Prompt: in.Prompt})
	if err != nil {
		return Event{}, fmt.Errorf("marshal claude user prompt: %w", err)
	}
	return Event{
		Agent:             "claude-code",
		ExternalSessionID: in.SessionID,
		CWD:               in.CWD,
		Kind:              "user_message",
		PayloadJSON:       payload,
	}, nil
}

func NormalizeClaudePreCompact(raw []byte) (Event, error) {
	return normalizeClaudeLifecycle(raw, "pre_compact")
}

func NormalizeClaudeSubagentStart(raw []byte) (Event, error) {
	return normalizeClaudeLifecycle(raw, "subagent_start")
}

func NormalizeClaudeNotification(raw []byte) (Event, error) {
	return normalizeClaudeLifecycle(raw, "notification")
}

func NormalizeClaudeTaskCompleted(raw []byte) (Event, error) {
	return normalizeClaudeLifecycle(raw, "task_completed")
}

func normalizeClaudeLifecycle(raw []byte, kind string) (Event, error) {
	var base struct {
		SessionID string `json:"session_id"`
		CWD       string `json:"cwd"`
	}
	if err := json.Unmarshal(raw, &base); err != nil {
		return Event{}, fmt.Errorf("parse claude %s event: %w", kind, err)
	}
	payload, err := payloadWithoutSession(raw)
	if err != nil {
		return Event{}, fmt.Errorf("marshal claude %s payload: %w", kind, err)
	}
	return Event{Agent: "claude-code", ExternalSessionID: base.SessionID, CWD: base.CWD, Kind: kind, PayloadJSON: payload}, nil
}

func payloadWithoutSession(raw []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	delete(payload, "session_id")
	delete(payload, "cwd")
	delete(payload, "transcript_path")
	delete(payload, "hook_event_name")
	return json.Marshal(payload)
}
