package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func TestReplaySessionEndpointReturnsOrderedRedactedEvents(t *testing.T) {
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	_, _ = s.InsertObservation(t.Context(), store.ObservationInput{Agent: "claude-code", ExternalSessionID: "s1", CWD: "/repo", TS: 2000, Kind: "tool_use", Tool: "Bash", PayloadJSON: []byte(`{"token":"ghp_abcdefghijklmnopqrstuvwxyzABCDEFGHIJ"}`), Hash: "h2"}, 300)
	_, _ = s.InsertObservation(t.Context(), store.ObservationInput{Agent: "claude-code", ExternalSessionID: "s1", CWD: "/repo", TS: 1000, Kind: "user_message", PayloadJSON: []byte(`{"prompt":"hello"}`), Hash: "h1"}, 300)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/integrations/replay/session", bytes.NewBufferString(`{"session_id":"claude-code:s1","limit":10}`))
	New(s).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("ghp_abcdefghijklmnopqrstuvwxyzABCDEFGHIJ")) {
		t.Fatalf("response leaked secret: %s", rec.Body.String())
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
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if len(body.Events) != 2 {
		t.Fatalf("events = %+v", body.Events)
	}
	if body.Events[0].Timestamp != 1000 || body.Events[0].Actor != "user" || body.Events[0].Type != "user_message" {
		t.Fatalf("first event = %+v", body.Events[0])
	}
	if body.Events[1].Tool != "Bash" || body.Events[1].Actor != "tool" || !bytes.Contains(body.Events[1].PayloadDetail, []byte("[REDACTED]")) || body.Events[1].PayloadPreview == "" {
		t.Fatalf("second event = %+v", body.Events[1])
	}
	if body.Events[0].ID == "" || body.Events[0].ID == body.Events[1].ID {
		t.Fatalf("event ids = %q %q", body.Events[0].ID, body.Events[1].ID)
	}
}
