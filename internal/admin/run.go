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

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/config"
	"github.com/steinyzxc/yet-another-memory-bank-67/internal/embed"
	mcbsearch "github.com/steinyzxc/yet-another-memory-bank-67/internal/search"
	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
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
	case "migrate":
		return runMigrate(ctx, args[1:], io)
	case "add":
		return runAdd(ctx, args[1:], io)
	case "search":
		return runSearch(ctx, args[1:], io)
	case "sessions":
		return runSessions(ctx, args[1:], io)
	case "backup":
		return runBackup(ctx, args[1:], io)
	case "doctor":
		return runDoctor(ctx, args[1:], io)
	case "embed-missing":
		return runEmbedMissing(ctx, args[1:], io, false)
	case "embed-rebuild":
		return runEmbedMissing(ctx, args[1:], io, true)
	case "compact":
		return runCompact(ctx, args[1:], io)
	case "decay":
		return runDecay(ctx, args[1:], io)
	case "import-jsonl":
		return runImportJSONL(ctx, args[1:], io)
	case "bench":
		return runBench(ctx, args[1:], io)
	default:
		fmt.Fprintf(io.Stderr, "unsupported command %q\n", args[0])
		return 2
	}
}

func runCompact(ctx context.Context, args []string, io IO) int {
	opts, rest, sessionID, agent, err := parseCompactOptions(args, io)
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
	if sessionID != "" {
		session, err := s.Session(ctx, sessionID)
		if err != nil {
			fmt.Fprintf(io.Stderr, "load session: %v\n", err)
			return 1
		}
		cwds, _ := s.SessionCWDs(ctx, sessionID, 10)
		fmt.Fprintln(io.Stdout, compactInstruction(agent, session.ID, session.Project, cwds))
		return 0
	}
	if len(rest) > 0 {
		fmt.Fprintf(io.Stderr, "unexpected argument %q\n", rest[0])
		return 2
	}
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(io.Stderr, "%v\n", err)
		return 2
	}
	sessions, err := s.CompactableSessions(ctx, cfg.Compaction.MinObservations, io.Now()-7*24*60*60*1000, opts.limit)
	if err != nil {
		fmt.Fprintf(io.Stderr, "list compactable sessions: %v\n", err)
		return 1
	}
	for _, session := range sessions {
		fmt.Fprintf(io.Stdout, "%s\t%s\t%s\t%d\n", session.ID, session.Agent, session.Project, session.NObs)
	}
	return 0
}

func runDecay(ctx context.Context, args []string, io IO) int {
	opts, rest, tauDays, minImportance, err := parseDecayOptions(args, io)
	if err != nil {
		fmt.Fprintf(io.Stderr, "%v\n", err)
		return 2
	}
	if len(rest) > 0 {
		fmt.Fprintf(io.Stderr, "unexpected argument %q\n", rest[0])
		return 2
	}
	s, err := store.Open(ctx, opts.dbPath)
	if err != nil {
		fmt.Fprintf(io.Stderr, "open store: %v\n", err)
		return 1
	}
	defer s.Close()
	deleted, err := s.DecayMemories(ctx, store.DecayOptions{Now: io.Now(), TauDays: tauDays, MinImportance: minImportance})
	if err != nil {
		fmt.Fprintf(io.Stderr, "decay memories: %v\n", err)
		return 1
	}
	fmt.Fprintf(io.Stdout, "deleted=%d\n", deleted)
	return 0
}

func runMigrate(ctx context.Context, args []string, io IO) int {
	opts, rest, err := parseOptions(args, io)
	if err != nil {
		fmt.Fprintf(io.Stderr, "%v\n", err)
		return 2
	}
	if len(rest) > 0 {
		fmt.Fprintf(io.Stderr, "unexpected argument %q\n", rest[0])
		return 2
	}
	s, err := store.Open(ctx, opts.dbPath)
	if err != nil {
		fmt.Fprintf(io.Stderr, "open store: %v\n", err)
		return 1
	}
	defer s.Close()
	fmt.Fprintln(io.Stdout, "ok")
	return 0
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
	if err := embedMemoryIfEnabled(ctx, s, id, text, io); err != nil {
		fmt.Fprintf(io.Stderr, "embed memory: %v\n", err)
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

	results, err := searchMemories(ctx, s, opts, query)
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

type searchEmbedder struct{ client *embed.Client }

func (e searchEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return e.client.Embed(ctx, text)
}

func (e searchEmbedder) Model() string { return e.client.ModelName() }
func (e searchEmbedder) Dim() int      { return e.client.CurrentDim() }

func searchMemories(ctx context.Context, s *store.Store, opts options, query string) ([]store.Memory, error) {
	cfg, err := config.Load("")
	if err != nil {
		return nil, err
	}
	if cfg.Embedding.Provider != "ollama" {
		return s.SearchMemories(ctx, store.MemorySearch{Project: opts.project, Query: query, Limit: opts.limit})
	}
	client, err := newEmbeddingClient(cfg)
	if err != nil {
		return s.SearchMemories(ctx, store.MemorySearch{Project: opts.project, Query: query, Limit: opts.limit})
	}
	results, err := (mcbsearch.Searcher{
		Store:    s,
		Embedder: searchEmbedder{client: client},
		Config: mcbsearch.Config{
			BM25TopK:      cfg.Search.BM25TopK,
			VectorTopK:    cfg.Search.VectorTopK,
			FinalTopK:     cfg.Search.FinalTopK,
			RRFK:          cfg.Search.RRFK,
			MaxPerSession: cfg.Search.MaxPerSession,
		},
	}).Hybrid(ctx, mcbsearch.Query{Text: query, Project: opts.project, Limit: opts.limit})
	if err != nil {
		return s.SearchMemories(ctx, store.MemorySearch{Project: opts.project, Query: query, Limit: opts.limit})
	}
	memories := make([]store.Memory, 0, len(results))
	for _, result := range results {
		memories = append(memories, result.Memory)
	}
	return memories, nil
}

func runSessions(ctx context.Context, args []string, io IO) int {
	opts, rest, err := parseOptions(args, io)
	if err != nil {
		fmt.Fprintf(io.Stderr, "%v\n", err)
		return 2
	}
	if len(rest) > 0 {
		fmt.Fprintf(io.Stderr, "unexpected argument %q\n", rest[0])
		return 2
	}

	s, err := store.Open(ctx, opts.dbPath)
	if err != nil {
		fmt.Fprintf(io.Stderr, "open store: %v\n", err)
		return 1
	}
	defer s.Close()

	sessions, err := s.ListSessions(ctx, opts.project, opts.limit)
	if err != nil {
		fmt.Fprintf(io.Stderr, "list sessions: %v\n", err)
		return 1
	}
	for _, session := range sessions {
		fmt.Fprintf(io.Stdout, "%s\t%s\t%s\t%d\t%d\t%d\n", session.ID, session.Agent, session.Project, session.StartedAt, session.EndedAt, session.NObs)
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

func parseCompactOptions(args []string, io IO) (options, []string, string, string, error) {
	filtered := make([]string, 0, len(args))
	sessionID := ""
	agent := "claude-code"
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--session":
			i++
			if i >= len(args) {
				return options{}, nil, "", "", errors.New("missing value for --session")
			}
			sessionID = args[i]
		case strings.HasPrefix(arg, "--session="):
			sessionID = strings.TrimPrefix(arg, "--session=")
		case arg == "--agent":
			i++
			if i >= len(args) {
				return options{}, nil, "", "", errors.New("missing value for --agent")
			}
			agent = args[i]
		case strings.HasPrefix(arg, "--agent="):
			agent = strings.TrimPrefix(arg, "--agent=")
		default:
			filtered = append(filtered, arg)
		}
	}
	opts, rest, err := parseOptions(filtered, io)
	return opts, rest, sessionID, agent, err
}

func parseDecayOptions(args []string, io IO) (options, []string, int, float64, error) {
	cfg, err := config.Load("")
	if err != nil {
		return options{}, nil, 0, 0, err
	}
	filtered := make([]string, 0, len(args))
	tauDays := cfg.Memory.DecayTauDays
	minImportance := cfg.Memory.MinImportance
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--tau-days":
			i++
			if i >= len(args) {
				return options{}, nil, 0, 0, errors.New("missing value for --tau-days")
			}
			parsed, err := strconv.Atoi(args[i])
			if err != nil || parsed <= 0 {
				return options{}, nil, 0, 0, fmt.Errorf("invalid --tau-days %q", args[i])
			}
			tauDays = parsed
		case strings.HasPrefix(arg, "--tau-days="):
			value := strings.TrimPrefix(arg, "--tau-days=")
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return options{}, nil, 0, 0, fmt.Errorf("invalid --tau-days %q", value)
			}
			tauDays = parsed
		case arg == "--min-importance":
			i++
			if i >= len(args) {
				return options{}, nil, 0, 0, errors.New("missing value for --min-importance")
			}
			parsed, err := strconv.ParseFloat(args[i], 64)
			if err != nil || parsed < 0 {
				return options{}, nil, 0, 0, fmt.Errorf("invalid --min-importance %q", args[i])
			}
			minImportance = parsed
		case strings.HasPrefix(arg, "--min-importance="):
			value := strings.TrimPrefix(arg, "--min-importance=")
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || parsed < 0 {
				return options{}, nil, 0, 0, fmt.Errorf("invalid --min-importance %q", value)
			}
			minImportance = parsed
		default:
			filtered = append(filtered, arg)
		}
	}
	opts, rest, err := parseOptions(filtered, io)
	return opts, rest, tauDays, minImportance, err
}

func compactInstruction(agent, sessionID, project string, cwds []string) string {
	prompt := fmt.Sprintf("session_id=%s project=%s cwds=%v - read observations via mcp__mcb__memory_session_observations, deduplicate with mcp__mcb__memory_search, save one summary with mcp__mcb__memory_session_summary_save, then save 3-7 durable facts with mcp__mcb__memory_save. Always pass session_id.", sessionID, project, cwds)
	if agent == "opencode" {
		return fmt.Sprintf("Run the mcb-compactor subagent with this prompt:\n\n%s", prompt)
	}
	return fmt.Sprintf("To compact session %s, dispatch the mcb-compactor subagent via Task with this prompt:\n\n%s", sessionID, prompt)
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
