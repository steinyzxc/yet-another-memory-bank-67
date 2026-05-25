package embed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOllamaClientEmbedsAndValidatesDimension(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":[0.25,0.5,0.75]}`))
	}))
	defer server.Close()

	client := &Client{URL: server.URL, Model: "nomic-embed-text", Dim: 3, Timeout: time.Second}
	vec, err := client.Embed(context.Background(), "search text")
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if gotPath != "/api/embeddings" {
		t.Fatalf("path = %q", gotPath)
	}
	if len(vec) != 3 || vec[0] != 0.25 || vec[2] != 0.75 {
		t.Fatalf("vec = %+v", vec)
	}

	client.Dim = 2
	if _, err := client.Embed(context.Background(), "search text"); err == nil {
		t.Fatal("expected dimension validation error")
	}
}

func TestCircuitBreakerSkipsAfterFailuresAndRecovers(t *testing.T) {
	breaker := NewCircuitBreaker(2, time.Minute, func() time.Time { return time.UnixMilli(1000) })
	if breaker.Open() {
		t.Fatal("new breaker should be closed")
	}
	breaker.RecordFailure()
	if breaker.Open() {
		t.Fatal("breaker opened too early")
	}
	breaker.RecordFailure()
	if !breaker.Open() {
		t.Fatal("breaker should open after threshold failures")
	}
	breaker.RecordSuccess()
	if breaker.Open() {
		t.Fatal("success should close breaker")
	}
}
