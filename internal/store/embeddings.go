package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
)

type VectorSearch struct {
	Project string
	Model   string
	Dim     int
	Limit   int
}

type VectorCandidate struct {
	Memory Memory
	Vector []float32
}

type MissingEmbeddingSearch struct {
	Project string
	Model   string
	Dim     int
	Limit   int
}

func (s *Store) SaveMemoryEmbedding(ctx context.Context, memoryID int64, model string, vec []float32) error {
	if model == "" {
		return fmt.Errorf("embedding model is empty")
	}
	if len(vec) == 0 {
		return fmt.Errorf("embedding vector is empty")
	}
	_, err := s.writeDB.ExecContext(ctx, `
INSERT INTO memory_embeddings (memory_id, model, dim, vec)
VALUES (?, ?, ?, ?)
ON CONFLICT(memory_id) DO UPDATE SET model = excluded.model, dim = excluded.dim, vec = excluded.vec
`, memoryID, model, len(vec), encodeFloat32LE(vec))
	if err != nil {
		return fmt.Errorf("save memory embedding: %w", err)
	}
	return nil
}

func (s *Store) VectorCandidates(ctx context.Context, search VectorSearch) ([]VectorCandidate, error) {
	if search.Model == "" || search.Dim <= 0 {
		return nil, nil
	}
	limit := search.Limit
	if limit <= 0 {
		limit = 10000
	}
	if limit > 100000 {
		limit = 100000
	}
	rows, err := s.readDB.QueryContext(ctx, `
SELECT m.id, m.project, m.text, m.tier, m.source, m.importance, m.session_id, m.created_at, m.updated_at, m.accessed_at, m.superseded_by, me.vec
FROM memory_embeddings me
JOIN memories m ON m.id = me.memory_id
WHERE m.project = ?
  AND m.superseded_by IS NULL
  AND me.model = ?
  AND me.dim = ?
ORDER BY m.created_at DESC, m.id DESC
LIMIT ?
`, search.Project, search.Model, search.Dim, limit)
	if err != nil {
		return nil, fmt.Errorf("vector candidates: %w", err)
	}
	defer rows.Close()

	var results []VectorCandidate
	for rows.Next() {
		candidate, err := scanVectorCandidate(rows, search.Dim)
		if err != nil {
			return nil, err
		}
		results = append(results, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vector candidates: %w", err)
	}
	return results, nil
}

func (s *Store) MissingEmbeddings(ctx context.Context, search MissingEmbeddingSearch) ([]Memory, error) {
	if search.Model == "" || search.Dim <= 0 {
		return nil, nil
	}
	limit := search.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.readDB.QueryContext(ctx, `
SELECT m.id, m.project, m.text, m.tier, m.source, m.importance, m.session_id, m.created_at, m.updated_at, m.accessed_at, m.superseded_by
FROM memories m
LEFT JOIN memory_embeddings me ON me.memory_id = m.id AND me.model = ? AND me.dim = ?
WHERE m.project = ?
  AND m.superseded_by IS NULL
  AND me.memory_id IS NULL
ORDER BY m.created_at ASC, m.id ASC
LIMIT ?
`, search.Model, search.Dim, search.Project, limit)
	if err != nil {
		return nil, fmt.Errorf("missing embeddings: %w", err)
	}
	defer rows.Close()

	var results []Memory
	for rows.Next() {
		memory, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, memory)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate missing embeddings: %w", err)
	}
	return results, nil
}

func (s *Store) DeleteEmbeddings(ctx context.Context, project, model string) error {
	_, err := s.writeDB.ExecContext(ctx, `
DELETE FROM memory_embeddings
WHERE memory_id IN (SELECT id FROM memories WHERE project = ?)
  AND (? = '' OR model = ?)
`, project, model, model)
	if err != nil {
		return fmt.Errorf("delete embeddings: %w", err)
	}
	return nil
}

func (s *Store) TouchMemories(ctx context.Context, ids []int64, accessedAt int64) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin touch memories: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE memories SET accessed_at = ?, access_cnt = access_cnt + 1 WHERE id = ?`, accessedAt, id); err != nil {
			return fmt.Errorf("touch memory %d: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit touch memories: %w", err)
	}
	committed = true
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMemory(row rowScanner) (Memory, error) {
	var memory Memory
	var sessionID sql.NullString
	var supersededBy sql.NullInt64
	if err := row.Scan(
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
		return Memory{}, fmt.Errorf("scan memory: %w", err)
	}
	memory.SessionID = sessionID.String
	memory.SupersededBy = supersededBy.Int64
	return memory, nil
}

func scanVectorCandidate(row rowScanner, dim int) (VectorCandidate, error) {
	var memory Memory
	var sessionID sql.NullString
	var supersededBy sql.NullInt64
	var blob []byte
	if err := row.Scan(
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
		&blob,
	); err != nil {
		return VectorCandidate{}, fmt.Errorf("scan vector candidate: %w", err)
	}
	vec, err := decodeFloat32LE(blob, dim)
	if err != nil {
		return VectorCandidate{}, err
	}
	memory.SessionID = sessionID.String
	memory.SupersededBy = supersededBy.Int64
	return VectorCandidate{Memory: memory, Vector: vec}, nil
}

func encodeFloat32LE(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, value := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(value))
	}
	return buf
}

func decodeFloat32LE(blob []byte, dim int) ([]float32, error) {
	if len(blob) != dim*4 {
		return nil, fmt.Errorf("embedding blob length = %d, want %d", len(blob), dim*4)
	}
	vec := make([]float32, dim)
	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4:]))
	}
	return vec, nil
}
