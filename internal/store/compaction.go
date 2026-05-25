package store

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
)

type DecayOptions struct {
	Now           int64
	TauDays       int
	MinImportance float64
}

func (s *Store) InsertCompactionAttempt(ctx context.Context, sessionID, status, reason string, attemptedAt int64) error {
	_, err := s.writeDB.ExecContext(ctx, `
INSERT INTO compaction_attempts (session_id, status, reason, attempted_at)
VALUES (?, ?, ?, ?)
`, sessionID, status, reason, attemptedAt)
	if err != nil {
		return fmt.Errorf("insert compaction attempt: %w", err)
	}
	return nil
}

func (s *Store) ExpireCompactionAttempts(ctx context.Context, sessionID string, before int64) error {
	_, err := s.writeDB.ExecContext(ctx, `
UPDATE compaction_attempts
SET status = 'failed', reason = 'ttl expired'
WHERE session_id = ? AND status = 'requested' AND attempted_at < ?
`, sessionID, before)
	if err != nil {
		return fmt.Errorf("expire compaction attempts: %w", err)
	}
	return nil
}

func (s *Store) FreshRequestedCompactionAttempts(ctx context.Context, sessionID string, since int64) (int, error) {
	var count int
	err := s.readDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM compaction_attempts
WHERE session_id = ? AND status = 'requested' AND attempted_at >= ?
`, sessionID, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count compaction attempts: %w", err)
	}
	return count, nil
}

func (s *Store) MarkCompactionCompleted(ctx context.Context, sessionID string, completedAt int64) error {
	_, err := s.writeDB.ExecContext(ctx, `
UPDATE compaction_attempts
SET status = 'completed', completed_at = ?
WHERE id = (
    SELECT id FROM compaction_attempts
    WHERE session_id = ? AND status = 'requested'
    ORDER BY attempted_at DESC, id DESC
    LIMIT 1
)
`, completedAt, sessionID)
	if err != nil {
		return fmt.Errorf("complete compaction attempt: %w", err)
	}
	return nil
}

func (s *Store) SessionNeedsCompaction(ctx context.Context, sessionID string, minObservations int) (bool, error) {
	var nObs int
	var summary sql.NullString
	var startedAt int64
	err := s.readDB.QueryRowContext(ctx, `SELECT n_obs, summary, started_at FROM sessions WHERE id = ?`, sessionID).Scan(&nObs, &summary, &startedAt)
	if err != nil {
		return false, fmt.Errorf("load compaction session: %w", err)
	}
	if nObs < minObservations || summary.String != "" {
		return false, nil
	}
	var memoryCount int
	err = s.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM memories WHERE session_id = ? AND created_at >= ?`, sessionID, startedAt).Scan(&memoryCount)
	if err != nil {
		return false, fmt.Errorf("count compacted memories: %w", err)
	}
	return memoryCount == 0, nil
}

func (s *Store) SessionCWDs(ctx context.Context, sessionID string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.readDB.QueryContext(ctx, `
SELECT cwd
FROM observations
WHERE session_id = ?
GROUP BY cwd
ORDER BY MIN(ts), MIN(id)
LIMIT ?
`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("session cwds: %w", err)
	}
	defer rows.Close()
	var cwds []string
	for rows.Next() {
		var cwd string
		if err := rows.Scan(&cwd); err != nil {
			return nil, fmt.Errorf("scan session cwd: %w", err)
		}
		cwds = append(cwds, cwd)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session cwds: %w", err)
	}
	return cwds, nil
}

func (s *Store) SupersedeMemory(ctx context.Context, oldID, newID int64) error {
	if oldID <= 0 || newID <= 0 || oldID == newID {
		return fmt.Errorf("invalid supersession ids")
	}
	result, err := s.writeDB.ExecContext(ctx, `UPDATE memories SET superseded_by = ? WHERE id = ?`, newID, oldID)
	if err != nil {
		return fmt.Errorf("supersede memory: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("supersede rows affected: %w", err)
	}
	if changed == 0 {
		return fmt.Errorf("memory %d not found", oldID)
	}
	return nil
}

func (s *Store) DecayMemories(ctx context.Context, opts DecayOptions) (int64, error) {
	if opts.TauDays <= 0 {
		opts.TauDays = 30
	}
	if opts.MinImportance <= 0 {
		opts.MinImportance = 0.05
	}
	tauMillis := float64(opts.TauDays) * 24 * 60 * 60 * 1000
	rows, err := s.readDB.QueryContext(ctx, `SELECT id, importance, accessed_at, tier FROM memories ORDER BY id ASC`)
	if err != nil {
		return 0, fmt.Errorf("list memories for decay: %w", err)
	}
	type decayed struct {
		id         int64
		importance float64
		delete     bool
	}
	var updates []decayed
	for rows.Next() {
		var id int64
		var importance float64
		var accessedAt int64
		var tier string
		if err := rows.Scan(&id, &importance, &accessedAt, &tier); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan memory for decay: %w", err)
		}
		age := float64(opts.Now - accessedAt)
		if age < 0 {
			age = 0
		}
		updated := importance * math.Exp(-age/tauMillis)
		updates = append(updates, decayed{id: id, importance: updated, delete: updated < opts.MinImportance && strings.ToLower(tier) != "procedural"})
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close decay rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate memories for decay: %w", err)
	}
	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin decay: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var deleted int64
	for _, update := range updates {
		if update.delete {
			if s.fts5 {
				if _, err := tx.ExecContext(ctx, `DELETE FROM memories_fts WHERE rowid = ?`, update.id); err != nil {
					return 0, fmt.Errorf("delete decayed fts memory %d: %w", update.id, err)
				}
			}
			result, err := tx.ExecContext(ctx, `DELETE FROM memories WHERE id = ?`, update.id)
			if err != nil {
				return 0, fmt.Errorf("delete decayed memory %d: %w", update.id, err)
			}
			n, _ := result.RowsAffected()
			deleted += n
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE memories SET importance = ? WHERE id = ?`, update.importance, update.id); err != nil {
			return 0, fmt.Errorf("update decayed memory %d: %w", update.id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit decay: %w", err)
	}
	committed = true
	return deleted, nil
}

func (s *Store) CompactableSessions(ctx context.Context, minObservations int, since int64, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.readDB.QueryContext(ctx, `
SELECT s.id, s.agent, s.external_id, s.project, s.started_at, s.ended_at, s.summary, s.n_obs
FROM sessions s
WHERE s.n_obs >= ?
  AND s.ended_at IS NOT NULL
  AND s.ended_at >= ?
  AND (s.summary IS NULL OR s.summary = '')
  AND NOT EXISTS (SELECT 1 FROM memories m WHERE m.session_id = s.id AND m.created_at >= s.started_at)
ORDER BY s.ended_at DESC, s.id DESC
LIMIT ?
`, minObservations, since, limit)
	if err != nil {
		return nil, fmt.Errorf("compactable sessions: %w", err)
	}
	defer rows.Close()
	var sessions []Session
	for rows.Next() {
		var session Session
		var endedAt sql.NullInt64
		var summary sql.NullString
		if err := rows.Scan(&session.ID, &session.Agent, &session.ExternalID, &session.Project, &session.StartedAt, &endedAt, &summary, &session.NObs); err != nil {
			return nil, fmt.Errorf("scan compactable session: %w", err)
		}
		session.EndedAt = endedAt.Int64
		session.Summary = summary.String
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate compactable sessions: %w", err)
	}
	return sessions, nil
}
