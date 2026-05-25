package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/config"
	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

type doctorOptions struct {
	configPath string
	dbPath     string
	ollamaURL  string
}

func runDoctor(ctx context.Context, args []string, io IO) int {
	opts, rest, err := parseDoctorOptions(args)
	if err != nil {
		fmt.Fprintf(io.Stderr, "%v\n", err)
		return 2
	}
	if len(rest) > 0 {
		fmt.Fprintf(io.Stderr, "unexpected argument %q\n", rest[0])
		return 2
	}
	if opts.configPath != "" {
		if _, err := config.Load(opts.configPath); err != nil {
			fmt.Fprintf(io.Stderr, "config: %v\n", err)
			return 1
		}
		fmt.Fprintln(io.Stdout, "config: ok")
	}
	if _, err := os.Stat(opts.dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(io.Stderr, "missing db: %s\n", opts.dbPath)
			return 1
		}
		fmt.Fprintf(io.Stderr, "stat db: %v\n", err)
		return 1
	}
	s, err := store.Open(ctx, opts.dbPath)
	if err != nil {
		fmt.Fprintf(io.Stderr, "open db: %v\n", err)
		return 1
	}
	defer s.Close()
	if err := s.Ping(ctx); err != nil {
		fmt.Fprintf(io.Stderr, "db ping: %v\n", err)
		return 1
	}
	if err := checkDBDirectoryWritable(opts.dbPath); err != nil {
		fmt.Fprintf(io.Stderr, "db permissions: %v\n", err)
		return 1
	}
	if opts.ollamaURL != "" {
		if err := checkOllama(ctx, opts.ollamaURL); err != nil {
			fmt.Fprintf(io.Stderr, "ollama: %v\n", err)
			return 1
		}
		fmt.Fprintln(io.Stdout, "ollama: ok")
	}
	fmt.Fprintln(io.Stdout, "ok")
	return 0
}

func parseDoctorOptions(args []string) (doctorOptions, []string, error) {
	configPath := os.Getenv("MCB_CONFIG")
	cfg, err := config.Load(configPath)
	if err != nil && configPath != "" {
		return doctorOptions{}, nil, err
	}
	if err != nil {
		cfg = config.Default()
	}
	opts := doctorOptions{configPath: configPath, dbPath: cfg.Storage.DBPath}
	if cfg.Embedding.Provider == "ollama" {
		opts.ollamaURL = cfg.Embedding.OllamaURL
	}
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--config":
			i++
			if i >= len(args) {
				return doctorOptions{}, nil, errors.New("missing value for --config")
			}
			opts.configPath = args[i]
			cfg, err := config.Load(opts.configPath)
			if err != nil {
				return doctorOptions{}, nil, fmt.Errorf("config: %w", err)
			}
			opts.dbPath = cfg.Storage.DBPath
			if cfg.Embedding.Provider == "ollama" {
				opts.ollamaURL = cfg.Embedding.OllamaURL
			}
		case strings.HasPrefix(arg, "--config="):
			opts.configPath = strings.TrimPrefix(arg, "--config=")
			cfg, err := config.Load(opts.configPath)
			if err != nil {
				return doctorOptions{}, nil, fmt.Errorf("config: %w", err)
			}
			opts.dbPath = cfg.Storage.DBPath
			if cfg.Embedding.Provider == "ollama" {
				opts.ollamaURL = cfg.Embedding.OllamaURL
			}
		case arg == "--db":
			i++
			if i >= len(args) {
				return doctorOptions{}, nil, errors.New("missing value for --db")
			}
			opts.dbPath = args[i]
		case strings.HasPrefix(arg, "--db="):
			opts.dbPath = strings.TrimPrefix(arg, "--db=")
		case arg == "--ollama-url":
			i++
			if i >= len(args) {
				return doctorOptions{}, nil, errors.New("missing value for --ollama-url")
			}
			opts.ollamaURL = args[i]
		case strings.HasPrefix(arg, "--ollama-url="):
			opts.ollamaURL = strings.TrimPrefix(arg, "--ollama-url=")
		case strings.HasPrefix(arg, "--"):
			return doctorOptions{}, nil, fmt.Errorf("unknown flag %q", arg)
		default:
			rest = append(rest, arg)
		}
	}
	return opts, rest, nil
}

func checkDBDirectoryWritable(dbPath string) error {
	dir := filepath.Dir(dbPath)
	f, err := os.CreateTemp(dir, ".mcb-doctor-*")
	if err != nil {
		return err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}

func checkOllama(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(url, "/")+"/api/tags", nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return fmt.Errorf("unexpected status %d", res.StatusCode)
	}
	return nil
}
