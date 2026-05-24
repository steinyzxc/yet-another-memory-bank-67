package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/alice/mcb/internal/config"
	"github.com/alice/mcb/internal/store"
)

type IO struct {
	Stdout io.Writer
	Stderr io.Writer
	Now    func() int64
	Getwd  func() (string, error)
}

type options struct {
	dbPath  string
	project string
	limit   int
}

func Run(ctx context.Context, args []string, io IO) int {
	io = fillIO(io)
	if len(args) == 0 {
		fmt.Fprintln(io.Stderr, "missing command")
		return 2
	}

	switch args[0] {
	case "add":
		return runAdd(ctx, args[1:], io)
	case "search":
		return runSearch(ctx, args[1:], io)
	default:
		fmt.Fprintf(io.Stderr, "unsupported command %q\n", args[0])
		return 2
	}
}

func runAdd(ctx context.Context, args []string, io IO) int {
	opts, textArgs, err := parseOptions(args, io)
	if err != nil {
		fmt.Fprintf(io.Stderr, "%v\n", err)
		return 2
	}
	text := strings.TrimSpace(strings.Join(textArgs, " "))
	if text == "" {
		fmt.Fprintln(io.Stderr, "missing memory text")
		return 2
	}

	s, err := store.Open(ctx, opts.dbPath)
	if err != nil {
		fmt.Fprintf(io.Stderr, "open store: %v\n", err)
		return 1
	}
	defer s.Close()

	id, err := s.AddMemory(ctx, store.MemoryInput{
		Project:   opts.project,
		Text:      text,
		Tier:      "fact",
		Source:    "manual",
		CreatedAt: io.Now(),
	})
	if err != nil {
		fmt.Fprintf(io.Stderr, "add memory: %v\n", err)
		return 1
	}
	fmt.Fprintf(io.Stdout, "memory_id=%d\n", id)
	return 0
}

func runSearch(ctx context.Context, args []string, io IO) int {
	opts, queryArgs, err := parseOptions(args, io)
	if err != nil {
		fmt.Fprintf(io.Stderr, "%v\n", err)
		return 2
	}
	query := strings.TrimSpace(strings.Join(queryArgs, " "))
	if query == "" {
		fmt.Fprintln(io.Stderr, "missing search query")
		return 2
	}

	s, err := store.Open(ctx, opts.dbPath)
	if err != nil {
		fmt.Fprintf(io.Stderr, "open store: %v\n", err)
		return 1
	}
	defer s.Close()

	results, err := s.SearchMemories(ctx, store.MemorySearch{Project: opts.project, Query: query, Limit: opts.limit})
	if err != nil {
		if errors.Is(err, store.ErrFTS5Unavailable) {
			fmt.Fprintf(io.Stderr, "%v\n", err)
			return 2
		}
		fmt.Fprintf(io.Stderr, "search memories: %v\n", err)
		return 1
	}
	for _, result := range results {
		fmt.Fprintf(io.Stdout, "%d\t%s\n", result.ID, result.Text)
	}
	return 0
}

func parseOptions(args []string, io IO) (options, []string, error) {
	cfg, err := config.Load("")
	if err != nil {
		return options{}, nil, err
	}
	opts := options{dbPath: cfg.Storage.DBPath, limit: 10}
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--db":
			i++
			if i >= len(args) {
				return options{}, nil, errors.New("missing value for --db")
			}
			opts.dbPath = args[i]
		case strings.HasPrefix(arg, "--db="):
			opts.dbPath = strings.TrimPrefix(arg, "--db=")
		case arg == "--project":
			i++
			if i >= len(args) {
				return options{}, nil, errors.New("missing value for --project")
			}
			opts.project = args[i]
		case strings.HasPrefix(arg, "--project="):
			opts.project = strings.TrimPrefix(arg, "--project=")
		case arg == "--limit":
			i++
			if i >= len(args) {
				return options{}, nil, errors.New("missing value for --limit")
			}
			limit, err := strconv.Atoi(args[i])
			if err != nil || limit <= 0 {
				return options{}, nil, fmt.Errorf("invalid --limit %q", args[i])
			}
			opts.limit = limit
		case strings.HasPrefix(arg, "--limit="):
			value := strings.TrimPrefix(arg, "--limit=")
			limit, err := strconv.Atoi(value)
			if err != nil || limit <= 0 {
				return options{}, nil, fmt.Errorf("invalid --limit %q", value)
			}
			opts.limit = limit
		case strings.HasPrefix(arg, "--"):
			return options{}, nil, fmt.Errorf("unknown flag %q", arg)
		default:
			rest = append(rest, arg)
		}
	}
	if opts.project == "" {
		cwd, err := io.Getwd()
		if err != nil {
			return options{}, nil, fmt.Errorf("get cwd: %w", err)
		}
		opts.project = cwd
	}
	return opts, rest, nil
}

func fillIO(io IO) IO {
	if io.Stdout == nil {
		io.Stdout = os.Stdout
	}
	if io.Stderr == nil {
		io.Stderr = os.Stderr
	}
	if io.Now == nil {
		io.Now = func() int64 { return time.Now().UnixMilli() }
	}
	if io.Getwd == nil {
		io.Getwd = os.Getwd
	}
	return io
}
