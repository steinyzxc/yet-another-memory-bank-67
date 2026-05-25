package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	URL     string
	Model   string
	Dim     int
	Timeout time.Duration
	HTTP    *http.Client

	detectedDim int
}

func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	if c.URL == "" {
		return nil, fmt.Errorf("ollama url is empty")
	}
	model := c.Model
	if model == "" {
		model = "nomic-embed-text"
	}
	body, err := json.Marshal(struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}{Model: model, Prompt: text})
	if err != nil {
		return nil, fmt.Errorf("encode embedding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.URL, "/")+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: c.timeout()}
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call ollama embeddings: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("ollama embeddings status %d", res.StatusCode)
	}
	var out struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(out.Embedding) == 0 {
		return nil, fmt.Errorf("ollama returned empty embedding")
	}
	if err := c.validateDim(len(out.Embedding)); err != nil {
		return nil, err
	}
	return out.Embedding, nil
}

func (c *Client) Healthy(ctx context.Context) bool {
	if c.URL == "" {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.URL, "/")+"/api/tags", nil)
	if err != nil {
		return false
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: c.timeout()}
	}
	res, err := client.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	return res.StatusCode < 400
}

func (c *Client) ModelName() string {
	if c.Model == "" {
		return "nomic-embed-text"
	}
	return c.Model
}

func (c *Client) CurrentDim() int {
	if c.Dim > 0 {
		return c.Dim
	}
	return c.detectedDim
}

func (c *Client) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return 30 * time.Second
}

func (c *Client) validateDim(dim int) error {
	if c.Dim > 0 {
		if dim != c.Dim {
			return fmt.Errorf("embedding dimension = %d, want %d", dim, c.Dim)
		}
		return nil
	}
	if c.detectedDim == 0 {
		c.detectedDim = dim
		return nil
	}
	if dim != c.detectedDim {
		return fmt.Errorf("embedding dimension = %d, want detected %d", dim, c.detectedDim)
	}
	return nil
}

type CircuitBreaker struct {
	failures  int
	threshold int
	cooldown  time.Duration
	openedAt  time.Time
	now       func() time.Time
}

func NewCircuitBreaker(threshold int, cooldown time.Duration, now func() time.Time) *CircuitBreaker {
	if threshold <= 0 {
		threshold = 3
	}
	if cooldown <= 0 {
		cooldown = 2 * time.Minute
	}
	if now == nil {
		now = time.Now
	}
	return &CircuitBreaker{threshold: threshold, cooldown: cooldown, now: now}
}

func (b *CircuitBreaker) Open() bool {
	if b == nil || b.openedAt.IsZero() {
		return false
	}
	if b.now().Sub(b.openedAt) >= b.cooldown {
		b.failures = 0
		b.openedAt = time.Time{}
		return false
	}
	return true
}

func (b *CircuitBreaker) RecordFailure() {
	if b == nil {
		return
	}
	b.failures++
	if b.failures >= b.threshold {
		b.openedAt = b.now()
	}
}

func (b *CircuitBreaker) RecordSuccess() {
	if b == nil {
		return
	}
	b.failures = 0
	b.openedAt = time.Time{}
}
