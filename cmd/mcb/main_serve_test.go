package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func TestRunServeStartsHTTPWithOpenedStore(t *testing.T) {
	oldServeHTTP := serveHTTP
	defer func() { serveHTTP = oldServeHTTP }()

	var gotAddr string
	serveHTTP = func(ctx context.Context, addr string, handler http.Handler) error {
		gotAddr = addr
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("readyz status = %d body=%s", rec.Code, rec.Body.String())
		}
		return nil
	}

	dbPath := filepath.Join(t.TempDir(), "memory.db")
	code := run(context.Background(), []string{"serve", "--db", dbPath, "--http", "127.0.0.1:0"}, nil, nil)
	if code != 0 {
		t.Fatalf("serve exit code = %d", code)
	}
	if gotAddr != "127.0.0.1:0" {
		t.Fatalf("serve addr = %q", gotAddr)
	}
}

func TestRunServeUsesConfigEnvDefaults(t *testing.T) {
	oldServeHTTP := serveHTTP
	defer func() { serveHTTP = oldServeHTTP }()

	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("MCB_STORAGE_DB_PATH", dbPath)
	t.Setenv("MCB_SERVER_HTTP_BIND", "127.0.0.1:12345")

	var gotAddr string
	serveHTTP = func(ctx context.Context, addr string, handler http.Handler) error {
		gotAddr = addr
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("readyz status = %d body=%s", rec.Code, rec.Body.String())
		}
		return nil
	}

	code := run(context.Background(), []string{"serve"}, nil, nil)
	if code != 0 {
		t.Fatalf("serve exit code = %d", code)
	}
	if gotAddr != "127.0.0.1:12345" {
		t.Fatalf("serve addr = %q", gotAddr)
	}
}

func TestRunServePassesConfigToServer(t *testing.T) {
	oldServeHTTP := serveHTTP
	defer func() { serveHTTP = oldServeHTTP }()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[storage]
db_path = "`+dbPath+`"

[server]
http_bind = "127.0.0.1:0"

[memory]
session_start_top_n = 1
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open seed store: %v", err)
	}
	_, err = s.AddMemory(context.Background(), store.MemoryInput{Project: "/repo", Text: "older memory", CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add older memory: %v", err)
	}
	_, err = s.AddMemory(context.Background(), store.MemoryInput{Project: "/repo", Text: "newer memory", CreatedAt: 2000})
	if err != nil {
		t.Fatalf("add newer memory: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close seed store: %v", err)
	}

	serveHTTP = func(ctx context.Context, addr string, handler http.Handler) error {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/hooks/session-start", strings.NewReader(`{"session_id":"s1","cwd":"/repo"}`))
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("session-start status = %d body=%s", rec.Code, rec.Body.String())
		}
		var body struct {
			HookSpecificOutput struct {
				AdditionalContext string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("parse context response: %v", err)
		}
		if !strings.Contains(body.HookSpecificOutput.AdditionalContext, "newer memory") {
			t.Fatalf("missing newer memory: %q", body.HookSpecificOutput.AdditionalContext)
		}
		if strings.Contains(body.HookSpecificOutput.AdditionalContext, "older memory") {
			t.Fatalf("context was not limited to one memory: %q", body.HookSpecificOutput.AdditionalContext)
		}
		return nil
	}

	code := run(context.Background(), []string{"serve", "--config", configPath}, nil, nil)
	if code != 0 {
		t.Fatalf("serve exit code = %d", code)
	}
}
