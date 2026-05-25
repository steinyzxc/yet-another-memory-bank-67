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

func NormalizeOpenCodeEvent(raw []byte) (Event, error) {
	var in struct {
		SessionID string          `json:"session_id"`
		CWD       string          `json:"cwd"`
		Kind      string          `json:"kind"`
		Tool      string          `json:"tool"`
		Payload   json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return Event{}, fmt.Errorf("parse opencode event: %w", err)
	}
	if in.Kind == "" {
		in.Kind = "opencode_event"
	}
	payload := in.Payload
	if len(payload) == 0 {
		var err error
		payload, err = payloadWithoutSession(raw)
		if err != nil {
			return Event{}, fmt.Errorf("marshal opencode event payload: %w", err)
		}
	}
	return Event{Agent: "opencode", ExternalSessionID: in.SessionID, CWD: in.CWD, Kind: in.Kind, Tool: in.Tool, PayloadJSON: payload}, nil
}

func NormalizeOpenCodeChat(raw []byte) (Event, error) {
	var in struct {
		SessionID string `json:"session_id"`
		CWD       string `json:"cwd"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return Event{}, fmt.Errorf("parse opencode chat event: %w", err)
	}
	payload, err := json.Marshal(struct {
		Message string `json:"message"`
	}{Message: in.Message})
	if err != nil {
		return Event{}, fmt.Errorf("marshal opencode chat payload: %w", err)
	}
	return Event{
		Agent:             "opencode",
		ExternalSessionID: in.SessionID,
		CWD:               in.CWD,
		Kind:              "user_message",
		PayloadJSON:       payload,
	}, nil
}
