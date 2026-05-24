package integrations

import (
	"encoding/json"
	"fmt"
)

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
