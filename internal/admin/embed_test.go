package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func TestRunEmbedMissingStoresVectors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":[1,0]}`))
	}))
	defer server.Close()

	dbPath := filepath.Join(t.TempDir(), "memory.db")
	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	id, err := s.AddMemory(context.Background(), store.MemoryInput{Project: "/repo", Text: "embed me", CreatedAt: 1000})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close seed store: %v", err)
	}

	t.Setenv("MCB_EMBEDDING_PROVIDER", "ollama")
	t.Setenv("MCB_EMBEDDING_OLLAMA_URL", server.URL)
	t.Setenv("MCB_EMBEDDING_MODEL", "test-model")
	t.Setenv("MCB_EMBEDDING_DIMENSIONS", "2")
	io := IO{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getwd: func() (string, error) { return "/repo", nil }}
	code := Run(context.Background(), []string{"embed-missing", "--db", dbPath, "--project", "/repo"}, io)
	if code != 0 {
		t.Fatalf("embed-missing exit code = %d stderr=%s", code, io.Stderr)
	}
	if got := io.Stdout.(*bytes.Buffer).String(); !strings.Contains(got, "embedded=1") {
		t.Fatalf("stdout = %q", got)
	}

	s, err = store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer s.Close()
	candidates, err := s.VectorCandidates(context.Background(), store.VectorSearch{Project: "/repo", Model: "test-model", Dim: 2})
	if err != nil {
		t.Fatalf("vector candidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Memory.ID != id {
		t.Fatalf("candidates = %+v", candidates)
	}
}
