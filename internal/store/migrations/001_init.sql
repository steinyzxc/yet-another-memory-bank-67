CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL
);

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
