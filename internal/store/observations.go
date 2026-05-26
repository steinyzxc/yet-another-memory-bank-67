package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/klauspost/compress/zstd"
)

type ObservationInput struct {
	Agent             string
	ExternalSessionID string
	CWD               string
	TS                int64
	Kind              string
	Tool              string
	PayloadJSON       []byte
	Hash              string
}

func (s *Store) InsertObservation(ctx context.Context, in ObservationInput, dedupWindowSeconds int64) (bool, error) {
	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin observation insert: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	sessionID, err := ensureSession(ctx, tx, in.Agent, in.ExternalSessionID, in.CWD, in.TS)
	if err != nil {
		return false, err
	}

	var exists int
	err = tx.QueryRowContext(ctx, `
SELECT 1
FROM observations
WHERE hash = ? AND ts > ?
LIMIT 1
`, in.Hash, in.TS-dedupWindowSeconds*1000).Scan(&exists)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("commit observation dedup: %w", err)
		}
		committed = true
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("check observation dedup: %w", err)
	}

	payload, encoding, err := encodePayload(in.PayloadJSON)
	if err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO observations (session_id, cwd, ts, kind, tool, payload, payload_len, payload_encoding, schema_version, hash)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, sessionID, in.CWD, in.TS, in.Kind, in.Tool, payload, len(in.PayloadJSON), encoding, 1, in.Hash)
	if err != nil {
		return false, fmt.Errorf("insert observation: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE sessions SET n_obs = n_obs + 1 WHERE id = ?`, sessionID)
	if err != nil {
		return false, fmt.Errorf("increment observation count: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit observation insert: %w", err)
	}
	committed = true
	return true, nil
}

func (s *Store) ObservationCount(ctx context.Context, sessionID string) (int64, error) {
	var count int64
	err := s.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM observations WHERE session_id = ?`, sessionID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count observations: %w", err)
	}
	return count, nil
}

func encodePayload(raw []byte) ([]byte, string, error) {
	if len(raw) < 512 {
		return raw, "raw", nil
	}
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, "", fmt.Errorf("create zstd encoder: %w", err)
	}
	defer enc.Close()
	return enc.EncodeAll(raw, nil), "zstd", nil
}
