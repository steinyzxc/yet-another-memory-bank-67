CREATE TABLE IF NOT EXISTS imported_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    transcript_path TEXT NOT NULL,
    event_id TEXT NOT NULL,
    imported_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS imported_events_path_event_idx ON imported_events(transcript_path, event_id);
