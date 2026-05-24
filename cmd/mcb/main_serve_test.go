package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
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
