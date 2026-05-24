package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/alice/mcb/internal/config"
	_ "github.com/mattn/go-sqlite3"
)

func runBackup(ctx context.Context, args []string, io IO) int {
	opts, rest, err := parseBackupOptions(args, io)
	if err != nil {
		fmt.Fprintf(io.Stderr, "%v\n", err)
		return 2
	}
	if len(rest) > 0 {
		fmt.Fprintf(io.Stderr, "unexpected argument %q\n", rest[0])
		return 2
	}
	if opts.out == "" {
		fmt.Fprintln(io.Stderr, "missing --out")
		return 2
	}

	if opts.out == "-" {
		if err := backupToStdout(ctx, opts.dbPath, io.Stdout); err != nil {
			fmt.Fprintf(io.Stderr, "backup: %v\n", err)
			return 1
		}
		return 0
	}
	if err := backupToPath(ctx, opts.dbPath, opts.out); err != nil {
		fmt.Fprintf(io.Stderr, "backup: %v\n", err)
		return 1
	}
	return 0
}

type backupOptions struct {
	dbPath string
	out    string
}

func parseBackupOptions(args []string, io IO) (backupOptions, []string, error) {
	cfg, err := config.Load("")
	if err != nil {
		return backupOptions{}, nil, err
	}
	opts := backupOptions{dbPath: cfg.Storage.DBPath}
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--db":
			i++
			if i >= len(args) {
				return backupOptions{}, nil, errors.New("missing value for --db")
			}
			opts.dbPath = args[i]
		case strings.HasPrefix(arg, "--db="):
			opts.dbPath = strings.TrimPrefix(arg, "--db=")
		case arg == "--out":
			i++
			if i >= len(args) {
				return backupOptions{}, nil, errors.New("missing value for --out")
			}
			opts.out = args[i]
		case strings.HasPrefix(arg, "--out="):
			opts.out = strings.TrimPrefix(arg, "--out=")
		case strings.HasPrefix(arg, "--"):
			return backupOptions{}, nil, fmt.Errorf("unknown flag %q", arg)
		default:
			rest = append(rest, arg)
		}
	}
	return opts, rest, nil
}

func backupToPath(ctx context.Context, dbPath, outPath string) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(outPath), ".mcb-backup-*.db")
	if err != nil {
		return fmt.Errorf("create temp backup: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp backup: %w", err)
	}
	defer os.Remove(tmpPath)

	if err := vacuumInto(ctx, dbPath, tmpPath); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		return fmt.Errorf("rename backup: %w", err)
	}
	return nil
}

func backupToStdout(ctx context.Context, dbPath string, out io.Writer) error {
	tmp, err := os.CreateTemp("", "mcb-backup-*.db")
	if err != nil {
		return fmt.Errorf("create temp backup: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp backup: %w", err)
	}
	defer os.Remove(tmpPath)

	if err := vacuumInto(ctx, dbPath, tmpPath); err != nil {
		return err
	}
	f, err := os.Open(tmpPath)
	if err != nil {
		return fmt.Errorf("open temp backup: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(out, f); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}
	return nil
}

func vacuumInto(ctx context.Context, dbPath, outPath string) error {
	db, err := sql.Open("sqlite3", "file:"+dbPath+"?_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("open source db: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping source db: %w", err)
	}
	escaped := strings.ReplaceAll(outPath, "'", "''")
	if _, err := db.ExecContext(ctx, "VACUUM INTO '"+escaped+"'"); err != nil {
		return fmt.Errorf("vacuum into backup: %w", err)
	}
	return nil
}
