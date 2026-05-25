package admin

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/bench"
)

type benchOptions struct {
	outDir  string
	dataset string
	limit   int
}

func runBench(ctx context.Context, args []string, io IO) int {
	if len(args) == 0 {
		fmt.Fprintln(io.Stderr, "missing benchmark name")
		return 2
	}
	name := args[0]
	opts, rest, err := parseBenchOptions(args[1:])
	if err != nil {
		fmt.Fprintf(io.Stderr, "%v\n", err)
		return 2
	}
	if len(rest) > 0 {
		fmt.Fprintf(io.Stderr, "unexpected argument %q\n", rest[0])
		return 2
	}
	if opts.outDir == "" {
		opts.outDir = filepath.Join("benchmark-results", time.Now().Format("20060102-150405"))
	}
	runOpts := bench.RunOptions{OutDir: opts.outDir, Dataset: opts.dataset, Limit: opts.limit, Now: io.Now}
	var result bench.RunResult
	switch name {
	case "coding-life":
		result, err = bench.RunCodingLife(ctx, runOpts)
	case "longmemeval":
		result, err = bench.RunLongMemEval(ctx, runOpts)
	default:
		fmt.Fprintf(io.Stderr, "unsupported benchmark %q\n", name)
		return 2
	}
	if err != nil {
		fmt.Fprintf(io.Stderr, "run benchmark: %v\n", err)
		return 1
	}
	fmt.Fprintf(io.Stdout, "bench=%s adapter=%s p_at_5=%.3f r_at_5=%.3f hit_at_5=%.3f mrr=%.3f out=%s\n", result.BenchName, result.Adapter, result.Metrics.PAt5, result.Metrics.RAt5, result.Metrics.HitAt5, result.Metrics.MRR, opts.outDir)
	return 0
}

func parseBenchOptions(args []string) (benchOptions, []string, error) {
	var opts benchOptions
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--out":
			i++
			if i >= len(args) {
				return benchOptions{}, nil, errors.New("missing value for --out")
			}
			opts.outDir = args[i]
		case strings.HasPrefix(arg, "--out="):
			opts.outDir = strings.TrimPrefix(arg, "--out=")
		case arg == "--dataset":
			i++
			if i >= len(args) {
				return benchOptions{}, nil, errors.New("missing value for --dataset")
			}
			opts.dataset = args[i]
		case strings.HasPrefix(arg, "--dataset="):
			opts.dataset = strings.TrimPrefix(arg, "--dataset=")
		case arg == "--limit":
			i++
			if i >= len(args) {
				return benchOptions{}, nil, errors.New("missing value for --limit")
			}
			limit, err := strconv.Atoi(args[i])
			if err != nil || limit < 0 {
				return benchOptions{}, nil, fmt.Errorf("invalid --limit %q", args[i])
			}
			opts.limit = limit
		case strings.HasPrefix(arg, "--limit="):
			value := strings.TrimPrefix(arg, "--limit=")
			limit, err := strconv.Atoi(value)
			if err != nil || limit < 0 {
				return benchOptions{}, nil, fmt.Errorf("invalid --limit %q", value)
			}
			opts.limit = limit
		case strings.HasPrefix(arg, "--"):
			return benchOptions{}, nil, fmt.Errorf("unknown flag %q", arg)
		default:
			rest = append(rest, arg)
		}
	}
	return opts, rest, nil
}
