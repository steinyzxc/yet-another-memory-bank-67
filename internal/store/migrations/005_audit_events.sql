CREATE TABLE IF NOT EXISTS audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ts INTEGER NOT NULL,
    action TEXT NOT NULL,
    memory_id INTEGER,
    session_id TEXT,
    project TEXT,
    payload TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS audit_events_ts_idx ON audit_events(ts, id);
CREATE INDEX IF NOT EXISTS audit_events_memory_id_idx ON audit_events(memory_id);
