package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/config"
	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

type perfSeedOptions struct {
	dbPath       string
	project      string
	runID        string
	outDir       string
	sessions     int
	memories     int
	observations int
}

func runPerfSeed(ctx context.Context, args []string, io IO) int {
	opts, err := parsePerfSeedOptions(args, io)
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
	result, err := s.SeedPerfData(ctx, store.PerfSeedOptions{Project: opts.project, RunID: opts.runID, Sessions: opts.sessions, Memories: opts.memories, Observations: opts.observations, Now: io.Now()})
	if err != nil {
		fmt.Fprintf(io.Stderr, "seed perf data: %v\n", err)
		return 1
	}
	if opts.outDir != "" {
		if err := os.MkdirAll(opts.outDir, 0o755); err != nil {
			fmt.Fprintf(io.Stderr, "create output dir: %v\n", err)
			return 1
		}
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Fprintf(io.Stderr, "marshal seed metadata: %v\n", err)
			return 1
		}
		if err := os.WriteFile(filepath.Join(opts.outDir, "seed.json"), append(data, '\n'), 0o644); err != nil {
			fmt.Fprintf(io.Stderr, "write seed metadata: %v\n", err)
			return 1
		}
	}
	fmt.Fprintf(io.Stdout, "project=%s run_id=%s sessions=%d memories=%d observations=%d summaries=%d\n", result.Project, result.RunID, result.Sessions, result.Memories, result.Observations, result.Summaries)
	return 0
}

func parsePerfSeedOptions(args []string, io IO) (perfSeedOptions, error) {
	cfg, err := config.Load("")
	if err != nil {
		return perfSeedOptions{}, err
	}
	opts := perfSeedOptions{dbPath: cfg.Storage.DBPath, sessions: 100, memories: 1000, observations: 10000, runID: "perf"}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--db":
			i++
			if i >= len(args) {
				return perfSeedOptions{}, errors.New("missing value for --db")
			}
			opts.dbPath = args[i]
		case strings.HasPrefix(arg, "--db="):
			opts.dbPath = strings.TrimPrefix(arg, "--db=")
		case arg == "--project":
			i++
			if i >= len(args) {
				return perfSeedOptions{}, errors.New("missing value for --project")
			}
			opts.project = args[i]
		case strings.HasPrefix(arg, "--project="):
			opts.project = strings.TrimPrefix(arg, "--project=")
		case arg == "--run-id":
			i++
			if i >= len(args) {
				return perfSeedOptions{}, errors.New("missing value for --run-id")
			}
			opts.runID = args[i]
		case strings.HasPrefix(arg, "--run-id="):
			opts.runID = strings.TrimPrefix(arg, "--run-id=")
		case arg == "--out":
			i++
			if i >= len(args) {
				return perfSeedOptions{}, errors.New("missing value for --out")
			}
			opts.outDir = args[i]
		case strings.HasPrefix(arg, "--out="):
			opts.outDir = strings.TrimPrefix(arg, "--out=")
		case arg == "--sessions":
			i++
			if i >= len(args) {
				return perfSeedOptions{}, errors.New("missing value for --sessions")
			}
			value, err := parseNonNegativeInt("--sessions", args[i])
			if err != nil {
				return perfSeedOptions{}, err
			}
			opts.sessions = value
		case strings.HasPrefix(arg, "--sessions="):
			value, err := parseNonNegativeInt("--sessions", strings.TrimPrefix(arg, "--sessions="))
			if err != nil {
				return perfSeedOptions{}, err
			}
			opts.sessions = value
		case arg == "--memories":
			i++
			if i >= len(args) {
				return perfSeedOptions{}, errors.New("missing value for --memories")
			}
			value, err := parseNonNegativeInt("--memories", args[i])
			if err != nil {
				return perfSeedOptions{}, err
			}
			opts.memories = value
		case strings.HasPrefix(arg, "--memories="):
			value, err := parseNonNegativeInt("--memories", strings.TrimPrefix(arg, "--memories="))
			if err != nil {
				return perfSeedOptions{}, err
			}
			opts.memories = value
		case arg == "--observations":
			i++
			if i >= len(args) {
				return perfSeedOptions{}, errors.New("missing value for --observations")
			}
			value, err := parseNonNegativeInt("--observations", args[i])
			if err != nil {
				return perfSeedOptions{}, err
			}
			opts.observations = value
		case strings.HasPrefix(arg, "--observations="):
			value, err := parseNonNegativeInt("--observations", strings.TrimPrefix(arg, "--observations="))
			if err != nil {
				return perfSeedOptions{}, err
			}
			opts.observations = value
		case strings.HasPrefix(arg, "--"):
			return perfSeedOptions{}, fmt.Errorf("unknown flag %q", arg)
		default:
			return perfSeedOptions{}, fmt.Errorf("unexpected argument %q", arg)
		}
	}
	if opts.project == "" {
		return perfSeedOptions{}, errors.New("missing --project")
	}
	if opts.sessions <= 0 {
		return perfSeedOptions{}, fmt.Errorf("invalid --sessions %q", strconv.Itoa(opts.sessions))
	}
	return opts, nil
}

func parseNonNegativeInt(flag, value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("invalid %s %q", flag, value)
	}
	return parsed, nil
}
