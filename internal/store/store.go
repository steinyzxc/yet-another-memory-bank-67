package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	writeDB *sql.DB
	readDB  *sql.DB
	fts5    bool
}

func Open(ctx context.Context, path string) (*Store, error) {
	dsn := "file:" + path + "?_foreign_keys=on&_busy_timeout=5000"
	writeDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open write db: %w", err)
	}
	writeDB.SetMaxOpenConns(1)

	readDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		writeDB.Close()
		return nil, fmt.Errorf("open read db: %w", err)
	}

	s := &Store{writeDB: writeDB, readDB: readDB}
	if err := s.migrate(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	var err error
	if s.writeDB != nil {
		err = s.writeDB.Close()
	}
	if s.readDB != nil {
		if readErr := s.readDB.Close(); err == nil {
			err = readErr
		}
	}
	return err
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.readDB == nil {
		return fmt.Errorf("store is not open")
	}
	if err := s.readDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping store: %w", err)
	}
	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	if err := s.applyMigrations(ctx); err != nil {
		return err
	}
	_, err := s.writeDB.ExecContext(ctx, `
CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
	text,
	tokenize = 'unicode61'
);
`)
	if err != nil {
		if strings.Contains(err.Error(), "no such module: fts5") {
			s.fts5 = false
			return nil
		}
		return fmt.Errorf("migrate memory fts: %w", err)
	}
	s.fts5 = true
	return nil
}
