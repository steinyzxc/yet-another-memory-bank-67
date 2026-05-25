package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type TimelineFilter struct {
	Project   string
	SessionID string
	Limit     int
}

type TimelineItem struct {
	Type        string
	ID          int64
	Project     string
	SessionID   string
	TS          int64
	Kind        string
	Tool        string
	Text        string
	PayloadJSON []byte
}

type FileHistoryFilter struct {
	Project string
	Files   []string
	Limit   int
}

type FileHistory struct {
	Memories     []Memory
	Observations []Observation
}

type PatternFilter struct {
	Project string
	Limit   int
}

type PatternResult struct {
	TopTools map[string]int64
	TopKinds map[string]int64
	Files    map[string]int64
}

type ExportFilter struct {
	Project string
	Limit   int
}

type ExportData struct {
	Memories     []Memory
	Sessions     []Session
	Observations []Observation
}

type AuditFilter struct {
	MemoryID int64
	Limit    int
}

type AuditEvent struct {
	ID        int64
	TS        int64
	Action    string
	MemoryID  int64
	SessionID string
	Project   string
	Payload   string
}

type VerifiedMemory struct {
	Memory       Memory
	Observations []Observation
	Audit        []AuditEvent
}

func (s *Store) Timeline(ctx context.Context, filter TimelineFilter) ([]TimelineItem, error) {
	limit := normalizedStoreLimit(filter.Limit, 50, 500)
	var items []TimelineItem
	obsQuery := `SELECT id, session_id, cwd, ts, kind, tool, payload, payload_encoding, payload_len, schema_version, hash FROM observations WHERE 1=1`
	var obsArgs []any
	if filter.Project != "" {
		obsQuery += ` AND cwd = ?`
		obsArgs = append(obsArgs, filter.Project)
	}
	if filter.SessionID != "" {
		obsQuery += ` AND session_id = ?`
		obsArgs = append(obsArgs, filter.SessionID)
	}
	obsQuery += ` ORDER BY ts DESC, id DESC LIMIT ?`
	obsArgs = append(obsArgs, limit)
	observations, err := s.scanObservations(ctx, obsQuery, obsArgs...)
	if err != nil {
		return nil, err
	}
	for _, obs := range observations {
		items = append(items, TimelineItem{Type: "observation", ID: obs.ID, Project: obs.CWD, SessionID: obs.SessionID, TS: obs.TS, Kind: obs.Kind, Tool: obs.Tool, PayloadJSON: obs.PayloadJSON})
	}

	memQuery := `SELECT id, project, text, tier, source, importance, session_id, created_at, updated_at, accessed_at, superseded_by FROM memories WHERE 1=1`
	var memArgs []any
	if filter.Project != "" {
		memQuery += ` AND project = ?`
		memArgs = append(memArgs, filter.Project)
	}
	if filter.SessionID != "" {
		memQuery += ` AND session_id = ?`
		memArgs = append(memArgs, filter.SessionID)
	}
	memQuery += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	memArgs = append(memArgs, limit)
	memories, err := s.scanMemories(ctx, memQuery, memArgs...)
	if err != nil {
		return nil, err
	}
	for _, memory := range memories {
		items = append(items, TimelineItem{Type: "memory", ID: memory.ID, Project: memory.Project, SessionID: memory.SessionID, TS: memory.CreatedAt, Kind: memory.Tier, Text: memory.Text})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].TS == items[j].TS {
			return items[i].ID < items[j].ID
		}
		return items[i].TS < items[j].TS
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *Store) FileHistory(ctx context.Context, filter FileHistoryFilter) (FileHistory, error) {
	limit := normalizedStoreLimit(filter.Limit, 20, 200)
	files := normalizedFiles(filter.Files)
	if len(files) == 0 {
		return FileHistory{}, nil
	}
	memories, err := s.scanMemories(ctx, `SELECT id, project, text, tier, source, importance, session_id, created_at, updated_at, accessed_at, superseded_by FROM memories WHERE project = ? AND superseded_by IS NULL ORDER BY created_at DESC, id DESC LIMIT ?`, filter.Project, 500)
	if err != nil {
		return FileHistory{}, err
	}
	var matchedMemories []Memory
	for _, memory := range memories {
		if containsAnyFile(memory.Text, files) {
			matchedMemories = append(matchedMemories, memory)
		}
		if len(matchedMemories) >= limit {
			break
		}
	}
	observations, err := s.scanObservations(ctx, `SELECT id, session_id, cwd, ts, kind, tool, payload, payload_encoding, payload_len, schema_version, hash FROM observations WHERE cwd = ? ORDER BY ts DESC, id DESC LIMIT ?`, filter.Project, 500)
	if err != nil {
		return FileHistory{}, err
	}
	var matchedObservations []Observation
	for _, obs := range observations {
		if containsAnyFile(string(obs.PayloadJSON), files) {
			matchedObservations = append(matchedObservations, obs)
		}
		if len(matchedObservations) >= limit {
			break
		}
	}
	return FileHistory{Memories: matchedMemories, Observations: matchedObservations}, nil
}

func (s *Store) Patterns(ctx context.Context, filter PatternFilter) (PatternResult, error) {
	limit := normalizedStoreLimit(filter.Limit, 10, 100)
	result := PatternResult{TopTools: map[string]int64{}, TopKinds: map[string]int64{}, Files: map[string]int64{}}
	if err := scanCounts(ctx, s.readDB, result.TopTools, `SELECT tool, COUNT(*) FROM observations WHERE cwd = ? AND tool != '' GROUP BY tool ORDER BY COUNT(*) DESC, tool ASC LIMIT ?`, filter.Project, limit); err != nil {
		return PatternResult{}, err
	}
	if err := scanCounts(ctx, s.readDB, result.TopKinds, `SELECT kind, COUNT(*) FROM observations WHERE cwd = ? GROUP BY kind ORDER BY COUNT(*) DESC, kind ASC LIMIT ?`, filter.Project, limit); err != nil {
		return PatternResult{}, err
	}
	payloads, err := s.listProjectObservationPayloads(ctx, filter.Project, 500)
	if err != nil {
		return PatternResult{}, err
	}
	for _, payload := range payloads {
		for _, file := range filesInPayload(payload) {
			result.Files[file]++
		}
	}
	return result, nil
}

func (s *Store) Export(ctx context.Context, filter ExportFilter) (ExportData, error) {
	limit := normalizedStoreLimit(filter.Limit, 100, 1000)
	memories, err := s.scanMemories(ctx, `SELECT id, project, text, tier, source, importance, session_id, created_at, updated_at, accessed_at, superseded_by FROM memories WHERE project = ? ORDER BY created_at DESC, id DESC LIMIT ?`, filter.Project, limit)
	if err != nil {
		return ExportData{}, err
	}
	sessions, err := s.ListSessions(ctx, filter.Project, limit)
	if err != nil {
		return ExportData{}, err
	}
	observations, err := s.scanObservations(ctx, `SELECT id, session_id, cwd, ts, kind, tool, payload, payload_encoding, payload_len, schema_version, hash FROM observations WHERE cwd = ? ORDER BY ts DESC, id DESC LIMIT ?`, filter.Project, limit)
	if err != nil {
		return ExportData{}, err
	}
	return ExportData{Memories: memories, Sessions: sessions, Observations: observations}, nil
}

func (s *Store) AuditEvents(ctx context.Context, filter AuditFilter) ([]AuditEvent, error) {
	limit := normalizedStoreLimit(filter.Limit, 50, 500)
	query := `SELECT id, ts, action, memory_id, session_id, project, payload FROM audit_events WHERE 1=1`
	var args []any
	if filter.MemoryID > 0 {
		query += ` AND memory_id = ?`
		args = append(args, filter.MemoryID)
	}
	query += ` ORDER BY ts DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.readDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("audit events: %w", err)
	}
	defer rows.Close()
	var events []AuditEvent
	for rows.Next() {
		var event AuditEvent
		var memoryID sql.NullInt64
		var sessionID, project sql.NullString
		if err := rows.Scan(&event.ID, &event.TS, &event.Action, &memoryID, &sessionID, &project, &event.Payload); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		event.MemoryID = memoryID.Int64
		event.SessionID = sessionID.String
		event.Project = project.String
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit events: %w", err)
	}
	return events, nil
}

func (s *Store) VerifyMemory(ctx context.Context, id int64) (VerifiedMemory, error) {
	memory, err := s.Memory(ctx, id)
	if err != nil {
		return VerifiedMemory{}, err
	}
	var observations []Observation
	if memory.SessionID != "" {
		observations, err = s.ListSessionObservations(ctx, memory.SessionID, 100)
		if err != nil {
			return VerifiedMemory{}, err
		}
	}
	audit, err := s.AuditEvents(ctx, AuditFilter{MemoryID: id, Limit: 100})
	if err != nil {
		return VerifiedMemory{}, err
	}
	return VerifiedMemory{Memory: memory, Observations: observations, Audit: audit}, nil
}

func (s *Store) insertAuditTx(ctx context.Context, tx *sql.Tx, event AuditEvent) error {
	if event.Payload == "" {
		event.Payload = "{}"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_events (ts, action, memory_id, session_id, project, payload) VALUES (?, ?, NULLIF(?, 0), NULLIF(?, ''), NULLIF(?, ''), ?)`, event.TS, event.Action, event.MemoryID, event.SessionID, event.Project, event.Payload)
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

func (s *Store) scanMemories(ctx context.Context, query string, args ...any) ([]Memory, error) {
	rows, err := s.readDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("scan memories query: %w", err)
	}
	defer rows.Close()
	var memories []Memory
	for rows.Next() {
		var memory Memory
		var sessionID sql.NullString
		var supersededBy sql.NullInt64
		if err := rows.Scan(&memory.ID, &memory.Project, &memory.Text, &memory.Tier, &memory.Source, &memory.Importance, &sessionID, &memory.CreatedAt, &memory.UpdatedAt, &memory.AccessedAt, &supersededBy); err != nil {
			return nil, fmt.Errorf("scan memory: %w", err)
		}
		memory.SessionID = sessionID.String
		memory.SupersededBy = supersededBy.Int64
		memories = append(memories, memory)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memories: %w", err)
	}
	return memories, nil
}

func (s *Store) scanObservations(ctx context.Context, query string, args ...any) ([]Observation, error) {
	rows, err := s.readDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("scan observations query: %w", err)
	}
	defer rows.Close()
	var observations []Observation
	for rows.Next() {
		obs, err := scanObservation(rows)
		if err != nil {
			return nil, err
		}
		observations = append(observations, obs)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate observations: %w", err)
	}
	return observations, nil
}

type observationScanner interface {
	Scan(dest ...any) error
}

func scanObservation(scanner observationScanner) (Observation, error) {
	var obs Observation
	var tool sql.NullString
	var payload []byte
	if err := scanner.Scan(&obs.ID, &obs.SessionID, &obs.CWD, &obs.TS, &obs.Kind, &tool, &payload, &obs.PayloadEncoding, &obs.PayloadLen, &obs.SchemaVersion, &obs.Hash); err != nil {
		return Observation{}, fmt.Errorf("scan observation: %w", err)
	}
	obs.Tool = tool.String
	decoded, err := decodePayload(payload, obs.PayloadEncoding)
	if err != nil {
		return Observation{}, err
	}
	obs.PayloadJSON = decoded
	return obs, nil
}

func normalizedFiles(files []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, file := range files {
		file = strings.ToLower(strings.TrimSpace(file))
		if file != "" && !seen[file] {
			seen[file] = true
			out = append(out, file)
		}
	}
	return out
}

func containsAnyFile(text string, files []string) bool {
	text = strings.ToLower(text)
	for _, file := range files {
		if strings.Contains(text, file) {
			return true
		}
	}
	return false
}

func normalizedStoreLimit(value, def, max int) int {
	if value <= 0 {
		value = def
	}
	if value > max {
		value = max
	}
	return value
}

func auditPayload(values map[string]any) string {
	data, err := json.Marshal(values)
	if err != nil {
		return "{}"
	}
	return string(data)
}
