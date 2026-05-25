package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

var ErrFTS5Unavailable = errors.New("sqlite fts5 unavailable: rebuild with -tags sqlite_fts5")

type MemoryInput struct {
	Project    string
	Text       string
	Tier       string
	Source     string
	Importance float64
	SessionID  string
	CreatedAt  int64
}

type MemorySearch struct {
	Project string
	Query   string
	Limit   int
}

type MemoryUpdate struct {
	ID         int64
	Text       *string
	Tier       *string
	Importance *float64
	UpdatedAt  int64
}

type Memory struct {
	ID           int64
	Project      string
	Text         string
	Tier         string
	Source       string
	Importance   float64
	SessionID    string
	CreatedAt    int64
	UpdatedAt    int64
	AccessedAt   int64
	SupersededBy int64
	Score        float64
}

func (s *Store) AddMemory(ctx context.Context, in MemoryInput) (int64, error) {
	tier := in.Tier
	if tier == "" {
		tier = "fact"
	}
	source := in.Source
	if source == "" {
		source = "manual"
	}
	importance := in.Importance
	if importance == 0 {
		importance = 1.0
	}
	createdAt := in.CreatedAt
	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin memory insert: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	result, err := tx.ExecContext(ctx, `
INSERT INTO memories (project, text, tier, source, importance, session_id, created_at, updated_at, accessed_at)
VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?)
`, in.Project, in.Text, tier, source, importance, in.SessionID, createdAt, createdAt, createdAt)
	if err != nil {
		return 0, fmt.Errorf("insert memory: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("memory id: %w", err)
	}
	if s.fts5 {
		_, err = tx.ExecContext(ctx, `INSERT INTO memories_fts (rowid, text) VALUES (?, ?)`, id, in.Text)
		if err != nil {
			return 0, fmt.Errorf("insert memory fts: %w", err)
		}
	}
	if err := s.insertAuditTx(ctx, tx, AuditEvent{TS: createdAt, Action: "memory_create", MemoryID: id, SessionID: in.SessionID, Project: in.Project, Payload: auditPayload(map[string]any{"tier": tier, "source": source})}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit memory insert: %w", err)
	}
	committed = true
	return id, nil
}

func (s *Store) Memory(ctx context.Context, id int64) (Memory, error) {
	var memory Memory
	var sessionID sql.NullString
	var supersededBy sql.NullInt64
	err := s.readDB.QueryRowContext(ctx, `
SELECT id, project, text, tier, source, importance, session_id, created_at, updated_at, accessed_at, superseded_by
FROM memories
WHERE id = ?
`, id).Scan(
		&memory.ID,
		&memory.Project,
		&memory.Text,
		&memory.Tier,
		&memory.Source,
		&memory.Importance,
		&sessionID,
		&memory.CreatedAt,
		&memory.UpdatedAt,
		&memory.AccessedAt,
		&supersededBy,
	)
	if err != nil {
		return Memory{}, fmt.Errorf("load memory: %w", err)
	}
	memory.SessionID = sessionID.String
	memory.SupersededBy = supersededBy.Int64
	return memory, nil
}

func (s *Store) UpdateMemory(ctx context.Context, update MemoryUpdate) error {
	if update.ID <= 0 {
		return fmt.Errorf("memory id is required")
	}
	if update.Text == nil && update.Tier == nil && update.Importance == nil {
		return nil
	}
	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin memory update: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	sets := []string{"updated_at = ?"}
	args := []any{update.UpdatedAt}
	if update.Text != nil {
		sets = append(sets, "text = ?")
		args = append(args, *update.Text)
	}
	if update.Tier != nil {
		sets = append(sets, "tier = ?")
		args = append(args, *update.Tier)
	}
	if update.Importance != nil {
		sets = append(sets, "importance = ?")
		args = append(args, *update.Importance)
	}
	args = append(args, update.ID)
	result, err := tx.ExecContext(ctx, `UPDATE memories SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return fmt.Errorf("update memory: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update memory rows affected: %w", err)
	}
	if changed == 0 {
		return fmt.Errorf("memory %d not found", update.ID)
	}
	if update.Text != nil && s.fts5 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM memories_fts WHERE rowid = ?`, update.ID); err != nil {
			return fmt.Errorf("delete old memory fts: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO memories_fts (rowid, text) VALUES (?, ?)`, update.ID, *update.Text); err != nil {
			return fmt.Errorf("insert updated memory fts: %w", err)
		}
	}
	if err := s.insertAuditTx(ctx, tx, AuditEvent{TS: update.UpdatedAt, Action: "memory_update", MemoryID: update.ID, Payload: auditPayload(map[string]any{"text": update.Text != nil, "tier": update.Tier != nil, "importance": update.Importance != nil})}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit memory update: %w", err)
	}
	committed = true
	return nil
}

func (s *Store) SearchMemories(ctx context.Context, search MemorySearch) ([]Memory, error) {
	if !s.fts5 {
		return nil, ErrFTS5Unavailable
	}
	query := ftsQuery(search.Query)
	if query == "" {
		return nil, nil
	}
	limit := search.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	rows, err := s.readDB.QueryContext(ctx, `
SELECT m.id, m.project, m.text, m.tier, m.source, m.importance, m.session_id, m.created_at, m.updated_at, m.accessed_at, m.superseded_by, bm25(memories_fts) AS score
FROM memories_fts
JOIN memories m ON m.id = memories_fts.rowid
WHERE memories_fts MATCH ?
  AND m.project = ?
  AND m.superseded_by IS NULL
ORDER BY score, m.created_at DESC
LIMIT ?
`, query, search.Project, limit)
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}
	defer rows.Close()

	var results []Memory
	for rows.Next() {
		var memory Memory
		var sessionID sql.NullString
		var supersededBy sql.NullInt64
		if err := rows.Scan(
			&memory.ID,
			&memory.Project,
			&memory.Text,
			&memory.Tier,
			&memory.Source,
			&memory.Importance,
			&sessionID,
			&memory.CreatedAt,
			&memory.UpdatedAt,
			&memory.AccessedAt,
			&supersededBy,
			&memory.Score,
		); err != nil {
			return nil, fmt.Errorf("scan memory result: %w", err)
		}
		memory.SessionID = sessionID.String
		memory.SupersededBy = supersededBy.Int64
		results = append(results, memory)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memory results: %w", err)
	}
	return results, nil
}

func (s *Store) RecentMemories(ctx context.Context, project string, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.readDB.QueryContext(ctx, `
SELECT id, project, text, tier, source, importance, session_id, created_at, updated_at, accessed_at, superseded_by
FROM memories
WHERE project = ?
  AND superseded_by IS NULL
ORDER BY created_at DESC, id DESC
LIMIT ?
`, project, limit)
	if err != nil {
		return nil, fmt.Errorf("recent memories: %w", err)
	}
	defer rows.Close()

	var results []Memory
	for rows.Next() {
		var memory Memory
		var sessionID sql.NullString
		var supersededBy sql.NullInt64
		if err := rows.Scan(
			&memory.ID,
			&memory.Project,
			&memory.Text,
			&memory.Tier,
			&memory.Source,
			&memory.Importance,
			&sessionID,
			&memory.CreatedAt,
			&memory.UpdatedAt,
			&memory.AccessedAt,
			&supersededBy,
		); err != nil {
			return nil, fmt.Errorf("scan recent memory: %w", err)
		}
		memory.SessionID = sessionID.String
		memory.SupersededBy = supersededBy.Int64
		results = append(results, memory)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent memories: %w", err)
	}
	return results, nil
}

func ftsQuery(query string) string {
	tokens := strings.FieldsFunc(query, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
	})
	quoted := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token == "" {
			continue
		}
		quoted = append(quoted, `"`+strings.ReplaceAll(token, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " ")
}
