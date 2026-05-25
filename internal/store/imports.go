package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func (s *Store) ImportedEventExists(ctx context.Context, transcriptPath, eventID string) (bool, error) {
	var id int64
	err := s.readDB.QueryRowContext(ctx, `SELECT id FROM imported_events WHERE transcript_path = ? AND event_id = ?`, transcriptPath, eventID).Scan(&id)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, fmt.Errorf("check imported event: %w", err)
}

func (s *Store) RecordImportedEvent(ctx context.Context, transcriptPath, eventID string, importedAt int64) (bool, error) {
	result, err := s.writeDB.ExecContext(ctx, `
INSERT INTO imported_events (transcript_path, event_id, imported_at)
VALUES (?, ?, ?)
ON CONFLICT(transcript_path, event_id) DO NOTHING
`, transcriptPath, eventID, importedAt)
	if err != nil {
		return false, fmt.Errorf("record imported event: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("record imported event rows affected: %w", err)
	}
	return rows > 0, nil
}
