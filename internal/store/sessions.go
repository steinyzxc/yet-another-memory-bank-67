package store

import (
	"context"
	"fmt"
)

type Session struct {
	ID         string
	Agent      string
	ExternalID string
	Project    string
	StartedAt  int64
	NObs       int64
}

func (s *Store) EnsureSession(ctx context.Context, agent, externalID, project string, ts int64) (string, error) {
	id := agent + ":" + externalID
	_, err := s.writeDB.ExecContext(ctx, `
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
	err := s.readDB.QueryRowContext(ctx, `
SELECT id, agent, external_id, project, started_at, n_obs
FROM sessions
WHERE id = ?
`, id).Scan(&session.ID, &session.Agent, &session.ExternalID, &session.Project, &session.StartedAt, &session.NObs)
	if err != nil {
		return Session{}, fmt.Errorf("load session: %w", err)
	}
	return session, nil
}
