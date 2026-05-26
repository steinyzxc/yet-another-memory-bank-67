package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/admin"
	"github.com/steinyzxc/yet-another-memory-bank-67/internal/config"
	"github.com/steinyzxc/yet-another-memory-bank-67/internal/embed"
	internalmcp "github.com/steinyzxc/yet-another-memory-bank-67/internal/mcp"
	mcbsearch "github.com/steinyzxc/yet-another-memory-bank-67/internal/search"
	mcbserver "github.com/steinyzxc/yet-another-memory-bank-67/internal/server"
	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

var version = "dev"

var serveHTTP = func(ctx context.Context, addr string, handler http.Handler) error {
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdownCtx)
		case <-done:
		}
	}()
	err := srv.ListenAndServe()
	close(done)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "healthz":
		fmt.Fprintln(stdout, "ok")
		return 0
	case "serve":
		return runServe(ctx, args[1:], stderr)
	case "migrate", "add", "search", "sessions", "backup", "doctor", "embed-missing", "embed-rebuild", "compact", "decay", "import-jsonl", "bench", "perf-seed":
		return admin.Run(ctx, args, admin.IO{Stdout: stdout, Stderr: stderr})
	default:
		fmt.Fprintf(stderr, "unsupported command %q\n", cmd)
		return 2
	}
}

type serveOptions struct {
	dbPath             string
	http               string
	dedupWindowSeconds int64
	sessionStartTopN   int
	bearerToken        string
	embedding          config.EmbeddingConfig
	search             config.SearchConfig
	memory             config.MemoryConfig
	compaction         config.CompactionConfig
}

func runServe(ctx context.Context, args []string, stderr io.Writer) int {
	opts, err := parseServeOptions(args)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 2
	}
	s, err := store.Open(ctx, opts.dbPath)
	if err != nil {
		fmt.Fprintf(stderr, "open store: %v\n", err)
		return 1
	}
	defer s.Close()
	serverOpts := mcbserver.Options{
		BearerToken:        opts.bearerToken,
		DedupWindowSeconds: opts.dedupWindowSeconds,
		SessionStartTopN:   opts.sessionStartTopN,
		Compaction: mcbserver.CompactionOptions{
			Mode:              opts.compaction.Mode,
			MinObservations:   opts.compaction.MinObservations,
			MaxBlockAttempts:  opts.compaction.MaxBlockAttempts,
			AttemptTTLSeconds: opts.compaction.AttemptTTLSeconds,
			SubagentName:      opts.compaction.SubagentName,
		},
		MCPOptions: internalmcp.Options{
			SearchConfig: mcbsearch.Config{BM25TopK: opts.search.BM25TopK, VectorTopK: opts.search.VectorTopK, FinalTopK: opts.search.FinalTopK, RRFK: opts.search.RRFK, MaxPerSession: opts.search.MaxPerSession, Model: opts.embedding.Model, Dim: opts.embedding.Dimensions},
		},
	}
	if opts.memory.DecayIntervalHours > 0 {
		go runDecayTicker(ctx, s, opts.memory)
	}
	if opts.embedding.Provider == "ollama" {
		client := &embed.Client{URL: opts.embedding.OllamaURL, Model: opts.embedding.Model, Dim: opts.embedding.Dimensions, Timeout: time.Duration(opts.embedding.TimeoutMS) * time.Millisecond}
		serverOpts.MCPOptions.Embedder = client
		serverOpts.MCPOptions.CircuitBreaker = embed.NewCircuitBreaker(opts.embedding.CircuitBreakerFailures, time.Duration(opts.embedding.CircuitBreakerCooldownMS)*time.Millisecond, nil)
		serverOpts.ReadinessProbe = func(r *http.Request) error {
			if !client.Healthy(r.Context()) {
				return fmt.Errorf("ollama is not ready")
			}
			return nil
		}
	}
	slog.Info("mcb server starting", "http", opts.http, "db", opts.dbPath, "embedding_provider", opts.embedding.Provider, "compaction_mode", opts.compaction.Mode)
	handler := mcbserver.NewWithOptions(s, serverOpts)
	if err := serveHTTP(ctx, opts.http, handler); err != nil {
		fmt.Fprintf(stderr, "serve: %v\n", err)
		return 1
	}
	return 0
}

func parseServeOptions(args []string) (serveOptions, error) {
	configPath := os.Getenv("MCB_CONFIG")
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--config":
			i++
			if i >= len(args) {
				return serveOptions{}, errors.New("missing value for --config")
			}
			configPath = args[i]
		case strings.HasPrefix(arg, "--config="):
			configPath = strings.TrimPrefix(arg, "--config=")
		default:
			filtered = append(filtered, arg)
		}
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return serveOptions{}, err
	}
	opts := serveOptions{
		dbPath:             cfg.Storage.DBPath,
		http:               cfg.Server.HTTPBind,
		dedupWindowSeconds: cfg.Capture.DedupWindowSeconds,
		sessionStartTopN:   cfg.Memory.SessionStartTopN,
		bearerToken:        os.Getenv("MCB_BEARER_TOKEN"),
		embedding:          cfg.Embedding,
		search:             cfg.Search,
		memory:             cfg.Memory,
		compaction:         cfg.Compaction,
	}
	for i := 0; i < len(filtered); i++ {
		arg := filtered[i]
		switch {
		case arg == "--db":
			i++
			if i >= len(filtered) {
				return serveOptions{}, errors.New("missing value for --db")
			}
			opts.dbPath = filtered[i]
		case strings.HasPrefix(arg, "--db="):
			opts.dbPath = strings.TrimPrefix(arg, "--db=")
		case arg == "--http" || arg == "--http-bind":
			i++
			if i >= len(filtered) {
				return serveOptions{}, fmt.Errorf("missing value for %s", arg)
			}
			opts.http = filtered[i]
		case strings.HasPrefix(arg, "--http="):
			opts.http = strings.TrimPrefix(arg, "--http=")
		case strings.HasPrefix(arg, "--http-bind="):
			opts.http = strings.TrimPrefix(arg, "--http-bind=")
		case strings.HasPrefix(arg, "--"):
			return serveOptions{}, fmt.Errorf("unknown flag %q", arg)
		default:
			return serveOptions{}, fmt.Errorf("unexpected argument %q", arg)
		}
	}
	return opts, nil
}

func runDecayTicker(ctx context.Context, s *store.Store, cfg config.MemoryConfig) {
	ticker := time.NewTicker(time.Duration(cfg.DecayIntervalHours) * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = s.DecayMemories(ctx, store.DecayOptions{Now: time.Now().UnixMilli(), TauDays: cfg.DecayTauDays, MinImportance: cfg.MinImportance})
		}
	}
}
