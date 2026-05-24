package integrations

import (
	"encoding/json"
	"fmt"
)

func NormalizeOpenCodeTool(raw []byte) (Event, error) {
	var in struct {
		SessionID string          `json:"session_id"`
		CWD       string          `json:"cwd"`
		Tool      string          `json:"tool"`
		Input     json.RawMessage `json:"input"`
		Output    json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return Event{}, fmt.Errorf("parse opencode tool event: %w", err)
	}
	payload, err := json.Marshal(struct {
		ToolInput    json.RawMessage `json:"tool_input"`
		ToolResponse json.RawMessage `json:"tool_response"`
	}{
		ToolInput:    in.Input,
		ToolResponse: in.Output,
	})
	if err != nil {
		return Event{}, fmt.Errorf("marshal opencode payload: %w", err)
	}
	return Event{
		Agent:             "opencode",
		ExternalSessionID: in.SessionID,
		CWD:               in.CWD,
		Kind:              "tool_use",
		Tool:              in.Tool,
		PayloadJSON:       payload,
	}, nil
}
