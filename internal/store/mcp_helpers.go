package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/klauspost/compress/zstd"
)

type Observation struct {
	ID              int64
	SessionID       string
	CWD             string
	TS              int64
	Kind            string
	Tool            string
	PayloadJSON     []byte
	PayloadEncoding string
	PayloadLen      int
	SchemaVersion   int
	Hash            string
}

type ProjectProfile struct {
	Project          string
	MemoryCount      int64
	SessionCount     int64
	ObservationCount int64
	TopTiers         map[string]int64
	TopTools         map[string]int64
	FilesTouched     map[string]int64
}

func (s *Store) ListSessionObservations(ctx context.Context, sessionID string, limit int) ([]Observation, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.readDB.QueryContext(ctx, `
SELECT id, session_id, cwd, ts, kind, tool, payload, payload_encoding, payload_len, schema_version, hash
FROM observations
WHERE session_id = ?
ORDER BY ts ASC, id ASC
LIMIT ?
`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("list session observations: %w", err)
	}
	defer rows.Close()

	var observations []Observation
	for rows.Next() {
		var obs Observation
		var tool sql.NullString
		var payload []byte
		if err := rows.Scan(&obs.ID, &obs.SessionID, &obs.CWD, &obs.TS, &obs.Kind, &tool, &payload, &obs.PayloadEncoding, &obs.PayloadLen, &obs.SchemaVersion, &obs.Hash); err != nil {
			return nil, fmt.Errorf("scan observation: %w", err)
		}
		obs.Tool = tool.String
		decoded, err := decodePayload(payload, obs.PayloadEncoding)
		if err != nil {
			return nil, err
		}
		obs.PayloadJSON = decoded
		observations = append(observations, obs)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate observations: %w", err)
	}
	return observations, nil
}

func (s *Store) DeleteMemories(ctx context.Context, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin delete memories: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var deleted int64
	for _, id := range ids {
		if s.fts5 {
			if _, err := tx.ExecContext(ctx, `DELETE FROM memories_fts WHERE rowid = ?`, id); err != nil {
				return 0, fmt.Errorf("delete memory fts %d: %w", id, err)
			}
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM memories WHERE id = ?`, id)
		if err != nil {
			return 0, fmt.Errorf("delete memory %d: %w", id, err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("delete memory %d rows affected: %w", id, err)
		}
		deleted += n
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit delete memories: %w", err)
	}
	committed = true
	return deleted, nil
}

func (s *Store) ProjectProfile(ctx context.Context, project string) (ProjectProfile, error) {
	profile := ProjectProfile{Project: project, TopTiers: map[string]int64{}, TopTools: map[string]int64{}, FilesTouched: map[string]int64{}}
	if err := s.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM memories WHERE project = ? AND superseded_by IS NULL`, project).Scan(&profile.MemoryCount); err != nil {
		return ProjectProfile{}, fmt.Errorf("count profile memories: %w", err)
	}
	if err := s.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE project = ?`, project).Scan(&profile.SessionCount); err != nil {
		return ProjectProfile{}, fmt.Errorf("count profile sessions: %w", err)
	}
	if err := s.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM observations WHERE cwd = ?`, project).Scan(&profile.ObservationCount); err != nil {
		return ProjectProfile{}, fmt.Errorf("count profile observations: %w", err)
	}
	if err := scanCounts(ctx, s.readDB, profile.TopTiers, `
SELECT tier, COUNT(*)
FROM memories
WHERE project = ? AND superseded_by IS NULL
GROUP BY tier
ORDER BY COUNT(*) DESC, tier ASC
LIMIT 10
`, project); err != nil {
		return ProjectProfile{}, err
	}
	if err := scanCounts(ctx, s.readDB, profile.TopTools, `
SELECT tool, COUNT(*)
FROM observations
WHERE cwd = ? AND tool != ''
GROUP BY tool
ORDER BY COUNT(*) DESC, tool ASC
LIMIT 10
`, project); err != nil {
		return ProjectProfile{}, err
	}
	observations, err := s.listProjectObservationPayloads(ctx, project, 200)
	if err != nil {
		return ProjectProfile{}, err
	}
	for _, payload := range observations {
		for _, file := range filesInPayload(payload) {
			profile.FilesTouched[file]++
		}
	}
	return profile, nil
}

func (s *Store) ProjectProfiles(ctx context.Context) ([]ProjectProfile, error) {
	rows, err := s.readDB.QueryContext(ctx, `
SELECT project
FROM memories
WHERE superseded_by IS NULL
GROUP BY project
ORDER BY project ASC
`)
	if err != nil {
		return nil, fmt.Errorf("list profile projects: %w", err)
	}
	defer rows.Close()
	var projects []string
	for rows.Next() {
		var project string
		if err := rows.Scan(&project); err != nil {
			return nil, fmt.Errorf("scan profile project: %w", err)
		}
		projects = append(projects, project)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate profile projects: %w", err)
	}
	profiles := make([]ProjectProfile, 0, len(projects))
	for _, project := range projects {
		profile, err := s.ProjectProfile(ctx, project)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

func (s *Store) Status(ctx context.Context) (map[string]any, error) {
	status := map[string]any{}
	for key, query := range map[string]string{
		"memory_count":      `SELECT COUNT(*) FROM memories WHERE superseded_by IS NULL`,
		"session_count":     `SELECT COUNT(*) FROM sessions`,
		"observation_count": `SELECT COUNT(*) FROM observations`,
	} {
		var count int64
		if err := s.readDB.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return nil, fmt.Errorf("status %s: %w", key, err)
		}
		status[key] = count
	}
	return status, nil
}

func scanCounts(ctx context.Context, db *sql.DB, dst map[string]int64, query string, args ...any) error {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("profile counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var count int64
		if err := rows.Scan(&key, &count); err != nil {
			return fmt.Errorf("scan profile count: %w", err)
		}
		dst[key] = count
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate profile counts: %w", err)
	}
	return nil
}

func (s *Store) listProjectObservationPayloads(ctx context.Context, project string, limit int) ([][]byte, error) {
	rows, err := s.readDB.QueryContext(ctx, `
SELECT payload, payload_encoding
FROM observations
WHERE cwd = ?
ORDER BY ts DESC, id DESC
LIMIT ?
`, project, limit)
	if err != nil {
		return nil, fmt.Errorf("list profile payloads: %w", err)
	}
	defer rows.Close()
	var payloads [][]byte
	for rows.Next() {
		var payload []byte
		var encoding string
		if err := rows.Scan(&payload, &encoding); err != nil {
			return nil, fmt.Errorf("scan profile payload: %w", err)
		}
		decoded, err := decodePayload(payload, encoding)
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, decoded)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate profile payloads: %w", err)
	}
	return payloads, nil
}

func decodePayload(payload []byte, encoding string) ([]byte, error) {
	switch encoding {
	case "raw":
		return payload, nil
	case "zstd":
		dec, err := zstd.NewReader(nil)
		if err != nil {
			return nil, fmt.Errorf("create zstd decoder: %w", err)
		}
		defer dec.Close()
		decoded, err := dec.DecodeAll(payload, nil)
		if err != nil {
			return nil, fmt.Errorf("decode observation payload: %w", err)
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("unknown payload encoding %q", encoding)
	}
}

func filesInPayload(payload []byte) []string {
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var files []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for key, value := range x {
				if isFileKey(key) {
					if s, ok := value.(string); ok && s != "" && !seen[s] {
						seen[s] = true
						files = append(files, s)
					}
				}
				walk(value)
			}
		case []any:
			for _, item := range x {
				walk(item)
			}
		}
	}
	walk(value)
	return files
}

func isFileKey(key string) bool {
	key = strings.ToLower(key)
	return key == "file" || key == "filepath" || key == "path" || strings.HasSuffix(key, "_file") || strings.HasSuffix(key, "_path")
}
