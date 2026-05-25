package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/config"
	"github.com/steinyzxc/yet-another-memory-bank-67/internal/embed"
	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

func runEmbedMissing(ctx context.Context, args []string, io IO, rebuild bool) int {
	opts, rest, err := parseOptions(args, io)
	if err != nil {
		fmt.Fprintf(io.Stderr, "%v\n", err)
		return 2
	}
	if len(rest) > 0 {
		fmt.Fprintf(io.Stderr, "unexpected argument %q\n", rest[0])
		return 2
	}
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(io.Stderr, "load config: %v\n", err)
		return 1
	}
	client, err := newEmbeddingClient(cfg)
	if err != nil {
		fmt.Fprintf(io.Stderr, "%v\n", err)
		return 2
	}
	s, err := store.Open(ctx, opts.dbPath)
	if err != nil {
		fmt.Fprintf(io.Stderr, "open store: %v\n", err)
		return 1
	}
	defer s.Close()
	if rebuild {
		if err := s.DeleteEmbeddings(ctx, opts.project, client.ModelName()); err != nil {
			fmt.Fprintf(io.Stderr, "delete embeddings: %v\n", err)
			return 1
		}
	}
	embedded, err := embedMissing(ctx, s, client, opts.project, opts.limit)
	if err != nil {
		fmt.Fprintf(io.Stderr, "embed missing: %v\n", err)
		return 1
	}
	fmt.Fprintf(io.Stdout, "embedded=%d\n", embedded)
	return 0
}

func embedMemoryIfEnabled(ctx context.Context, s *store.Store, id int64, text string, io IO) error {
	cfg, err := config.Load("")
	if err != nil {
		return err
	}
	if cfg.Embedding.Provider != "ollama" {
		return nil
	}
	client, err := newEmbeddingClient(cfg)
	if err != nil {
		return err
	}
	vec, err := client.Embed(ctx, text)
	if err != nil {
		return err
	}
	return s.SaveMemoryEmbedding(ctx, id, client.ModelName(), vec)
}

func embedMissing(ctx context.Context, s *store.Store, client *embed.Client, project string, limit int) (int, error) {
	probe, err := client.Embed(ctx, "mcb dimension probe")
	if err != nil {
		return 0, err
	}
	dim := len(probe)
	missing, err := s.MissingEmbeddings(ctx, store.MissingEmbeddingSearch{Project: project, Model: client.ModelName(), Dim: dim, Limit: limit})
	if err != nil {
		return 0, err
	}
	embedded := 0
	for _, memory := range missing {
		vec, err := client.Embed(ctx, memory.Text)
		if err != nil {
			return embedded, err
		}
		if err := s.SaveMemoryEmbedding(ctx, memory.ID, client.ModelName(), vec); err != nil {
			return embedded, err
		}
		embedded++
	}
	return embedded, nil
}

func newEmbeddingClient(cfg config.Config) (*embed.Client, error) {
	if cfg.Embedding.Provider != "ollama" {
		return nil, fmt.Errorf("embedding provider %q is not supported for this command", cfg.Embedding.Provider)
	}
	if cfg.Embedding.OllamaURL == "" {
		return nil, fmt.Errorf("embedding ollama_url is empty")
	}
	return &embed.Client{
		URL:     cfg.Embedding.OllamaURL,
		Model:   cfg.Embedding.Model,
		Dim:     cfg.Embedding.Dimensions,
		Timeout: time.Duration(cfg.Embedding.TimeoutMS) * time.Millisecond,
	}, nil
}
