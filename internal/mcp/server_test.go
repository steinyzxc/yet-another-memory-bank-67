package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func TestInitializeAndToolsList(t *testing.T) {
	s := openTestStore(t)
	h := New(s, Options{})

	initBody := rpc(t, "initialize", map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "0"}})
	initResp := postRPC(t, h, initBody)
	if initResp.Error != nil {
		t.Fatalf("initialize error = %+v", initResp.Error)
	}
	var initResult struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	mustUnmarshal(t, initResp.Result, &initResult)
	if initResult.ProtocolVersion == "" || initResult.ServerInfo.Name != "mcb" {
		t.Fatalf("initialize result = %+v", initResult)
	}

	listResp := postRPC(t, h, rpc(t, "tools/list", nil))
	if listResp.Error != nil {
		t.Fatalf("tools/list error = %+v", listResp.Error)
	}
	var listResult struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	mustUnmarshal(t, listResp.Result, &listResult)
	want := map[string]bool{
		"memory_recall": true, "memory_save": true, "memory_search": true, "memory_sessions": true,
		"memory_session_observations": true, "memory_forget": true, "memory_profile": true,
	}
	for _, tool := range listResult.Tools {
		delete(want, tool.Name)
		if tool.Description == "" || tool.InputSchema["type"] != "object" {
			t.Fatalf("bad tool metadata: %+v", tool)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing tools: %+v", want)
	}
}

func TestMemorySaveSessionsObservationsProfileAndForgetDelete(t *testing.T) {
	s := openTestStore(t)
	h := New(s, Options{DefaultProject: "/repo", Now: func() int64 { return 5000 }})

	saveResult := callTool(t, h, "memory_save", map[string]any{
		"text":       "MCP saves durable project facts",
		"tier":       "semantic",
		"importance": 0.7,
		"session_id": "claude-code:s1",
	})
	var saved struct {
		ID int64 `json:"id"`
	}
	mustUnmarshal(t, saveResult, &saved)
	if saved.ID == 0 {
		t.Fatalf("saved id = 0")
	}
	memory, err := s.Memory(context.Background(), saved.ID)
	if err != nil {
		t.Fatalf("load memory: %v", err)
	}
	if memory.Project != "/repo" || memory.Tier != "semantic" || memory.Importance != 0.7 || memory.SessionID != "claude-code:s1" {
		t.Fatalf("memory = %+v", memory)
	}

	_, err = s.EnsureSession(context.Background(), "claude-code", "s1", "/repo", 1000)
	if err != nil {
		t.Fatalf("ensure session: %v", err)
	}
	_, err = s.InsertObservation(context.Background(), store.ObservationInput{Agent: "claude-code", ExternalSessionID: "s1", CWD: "/repo", TS: 2000, Kind: "tool_use", Tool: "Read", PayloadJSON: []byte(`{"file":"a.go"}`), Hash: "h1"}, 300)
	if err != nil {
		t.Fatalf("insert observation: %v", err)
	}

	sessionsResult := callTool(t, h, "memory_sessions", map[string]any{"project": "/repo", "limit": 5})
	var sessions struct {
		Sessions []struct {
			ID      string `json:"id"`
			Project string `json:"project"`
		} `json:"sessions"`
	}
	mustUnmarshal(t, sessionsResult, &sessions)
	if len(sessions.Sessions) != 1 || sessions.Sessions[0].ID != "claude-code:s1" {
		t.Fatalf("sessions = %+v", sessions)
	}

	observationsResult := callTool(t, h, "memory_session_observations", map[string]any{"session_id": "claude-code:s1"})
	var observations struct {
		Observations []struct {
			CWD     string          `json:"cwd"`
			Payload json.RawMessage `json:"payload"`
		} `json:"observations"`
	}
	mustUnmarshal(t, observationsResult, &observations)
	if len(observations.Observations) != 1 || observations.Observations[0].CWD != "/repo" || !bytes.Contains(observations.Observations[0].Payload, []byte("a.go")) {
		t.Fatalf("observations = %+v", observations)
	}

	profileResult := callTool(t, h, "memory_profile", map[string]any{"project": "/repo"})
	var profile struct {
		Project          string         `json:"project"`
		MemoryCount      int64          `json:"memory_count"`
		SessionCount     int64          `json:"session_count"`
		ObservationCount int64          `json:"observation_count"`
		TopTools         map[string]int `json:"top_tools"`
	}
	mustUnmarshal(t, profileResult, &profile)
	if profile.Project != "/repo" || profile.MemoryCount != 1 || profile.SessionCount != 1 || profile.ObservationCount != 1 || profile.TopTools["Read"] != 1 {
		t.Fatalf("profile = %+v", profile)
	}

	forgetResult := callTool(t, h, "memory_forget", map[string]any{"ids": []int64{saved.ID}, "confirm": true})
	var forgotten struct {
		Deleted int64 `json:"deleted"`
	}
	mustUnmarshal(t, forgetResult, &forgotten)
	if forgotten.Deleted != 1 {
		t.Fatalf("forgotten = %+v", forgotten)
	}
	if _, err := s.Memory(context.Background(), saved.ID); err == nil {
		t.Fatalf("memory %d still exists", saved.ID)
	}
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func rpc(t *testing.T, method string, params any) []byte {
	t.Helper()
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		body["params"] = params
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal rpc: %v", err)
	}
	return data
}

func postRPC(t *testing.T, h http.Handler, body []byte) rpcResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(body)))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp rpcResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v body=%s", err, rec.Body.String())
	}
	return resp
}

func callTool(t *testing.T, h http.Handler, name string, args map[string]any) json.RawMessage {
	t.Helper()
	resp := postRPC(t, h, rpc(t, "tools/call", map[string]any{"name": name, "arguments": args}))
	if resp.Error != nil {
		t.Fatalf("%s error = %+v", name, resp.Error)
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	mustUnmarshal(t, resp.Result, &result)
	if len(result.Content) != 1 || result.Content[0].Type != "text" {
		t.Fatalf("tool content = %+v", result.Content)
	}
	return json.RawMessage(result.Content[0].Text)
}

func mustUnmarshal(t *testing.T, data []byte, dst any) {
	t.Helper()
	if err := json.Unmarshal(data, dst); err != nil {
		t.Fatalf("unmarshal %s: %v", string(data), err)
	}
}
