package store

import (
	"context"
	"database/sql"
	"fmt"
)

type Session struct {
	ID         string
	Agent      string
	ExternalID string
	Project    string
	StartedAt  int64
	EndedAt    int64
	Summary    string
	NObs       int64
}

func (s *Store) EnsureSession(ctx context.Context, agent, externalID, project string, ts int64) (string, error) {
	return ensureSession(ctx, s.writeDB, agent, externalID, project, ts)
}

type sessionExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func ensureSession(ctx context.Context, execer sessionExecer, agent, externalID, project string, ts int64) (string, error) {
	id := agent + ":" + externalID
	_, err := execer.ExecContext(ctx, `
INSERT INTO sessions (id, agent, external_id, project, started_at, n_obs)
VALUES (?, ?, ?, ?, ?, 0)
ON CONFLICT(agent, external_id) DO NOTHING
`, id, agent, externalID, project, ts)
	if err != nil {
		return "", fmt.Errorf("ensure session: %w", err)
	}
	return id, nil
}

func (s *Store) Session(ctx context.Context, id string) (Session, error) {
	var session Session
	var endedAt sql.NullInt64
	var summary sql.NullString
	err := s.readDB.QueryRowContext(ctx, `
SELECT id, agent, external_id, project, started_at, ended_at, summary, n_obs
FROM sessions
WHERE id = ?
`, id).Scan(&session.ID, &session.Agent, &session.ExternalID, &session.Project, &session.StartedAt, &endedAt, &summary, &session.NObs)
	if err != nil {
		return Session{}, fmt.Errorf("load session: %w", err)
	}
	session.EndedAt = endedAt.Int64
	session.Summary = summary.String
	return session, nil
}

func (s *Store) EndSession(ctx context.Context, id string, endedAt int64) error {
	_, err := s.writeDB.ExecContext(ctx, `UPDATE sessions SET ended_at = ? WHERE id = ?`, endedAt, id)
	if err != nil {
		return fmt.Errorf("end session: %w", err)
	}
	return nil
}

func (s *Store) SaveSessionSummary(ctx context.Context, id, summary string, endedAt int64) error {
	_, err := s.writeDB.ExecContext(ctx, `UPDATE sessions SET summary = ?, ended_at = COALESCE(ended_at, ?) WHERE id = ?`, summary, endedAt, id)
	if err != nil {
		return fmt.Errorf("save session summary: %w", err)
	}
	return nil
}

func (s *Store) RecentSessionSummaries(ctx context.Context, project string, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 3
	}
	rows, err := s.readDB.QueryContext(ctx, `
SELECT id, agent, external_id, project, started_at, ended_at, summary, n_obs
FROM sessions
WHERE project = ? AND summary IS NOT NULL AND summary != ''
ORDER BY COALESCE(ended_at, started_at) DESC, id DESC
LIMIT ?
`, project, limit)
	if err != nil {
		return nil, fmt.Errorf("recent session summaries: %w", err)
	}
	defer rows.Close()
	var sessions []Session
	for rows.Next() {
		var session Session
		var endedAt sql.NullInt64
		var summary sql.NullString
		if err := rows.Scan(&session.ID, &session.Agent, &session.ExternalID, &session.Project, &session.StartedAt, &endedAt, &summary, &session.NObs); err != nil {
			return nil, fmt.Errorf("scan session summary: %w", err)
		}
		session.EndedAt = endedAt.Int64
		session.Summary = summary.String
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session summaries: %w", err)
	}
	return sessions, nil
}

func (s *Store) ListSessions(ctx context.Context, project string, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	query := `
SELECT id, agent, external_id, project, started_at, ended_at, summary, n_obs
FROM sessions
`
	var args []any
	if project != "" {
		query += `WHERE project = ?
`
		args = append(args, project)
	}
	query += `ORDER BY started_at DESC, id DESC
LIMIT ?`
	args = append(args, limit)

	rows, err := s.readDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var session Session
		var endedAt sql.NullInt64
		var summary sql.NullString
		if err := rows.Scan(&session.ID, &session.Agent, &session.ExternalID, &session.Project, &session.StartedAt, &endedAt, &summary, &session.NObs); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		session.EndedAt = endedAt.Int64
		session.Summary = summary.String
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}
	return sessions, nil
}
