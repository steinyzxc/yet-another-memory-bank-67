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
	_, err := s.writeDB.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS sessions (
	id TEXT PRIMARY KEY,
	agent TEXT NOT NULL,
	external_id TEXT NOT NULL,
	project TEXT NOT NULL,
	started_at INTEGER NOT NULL,
	ended_at INTEGER,
	summary TEXT,
	n_obs INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS sessions_agent_external_id_idx ON sessions(agent, external_id);
CREATE TABLE IF NOT EXISTS observations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL REFERENCES sessions(id),
	cwd TEXT NOT NULL,
	ts INTEGER NOT NULL,
	kind TEXT NOT NULL,
	tool TEXT NOT NULL,
	payload BLOB NOT NULL,
	payload_len INTEGER NOT NULL,
	payload_encoding TEXT NOT NULL,
	schema_version INTEGER NOT NULL DEFAULT 1,
	hash TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS observations_hash_ts_idx ON observations(hash, ts);
CREATE INDEX IF NOT EXISTS observations_session_id_idx ON observations(session_id);
CREATE TABLE IF NOT EXISTS memories (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project TEXT NOT NULL,
	text TEXT NOT NULL,
	tier TEXT NOT NULL,
	source TEXT NOT NULL,
	importance REAL NOT NULL,
	session_id TEXT REFERENCES sessions(id),
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	accessed_at INTEGER NOT NULL,
	superseded_by INTEGER REFERENCES memories(id)
);
CREATE INDEX IF NOT EXISTS memories_project_created_idx ON memories(project, created_at);
CREATE INDEX IF NOT EXISTS memories_superseded_by_idx ON memories(superseded_by);
`)
	if err != nil {
		return fmt.Errorf("migrate store: %w", err)
	}
	_, err = s.writeDB.ExecContext(ctx, `
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
