package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type PerfSeedOptions struct {
	Project      string `json:"project"`
	RunID        string `json:"run_id"`
	Sessions     int    `json:"sessions"`
	Memories     int    `json:"memories"`
	Observations int    `json:"observations"`
	Now          int64  `json:"now"`
}

type PerfSeedResult struct {
	Project      string `json:"project"`
	RunID        string `json:"run_id"`
	Sessions     int    `json:"sessions"`
	Memories     int    `json:"memories"`
	Observations int    `json:"observations"`
	Summaries    int    `json:"summaries"`
	StartedAt    int64  `json:"started_at"`
	EndedAt      int64  `json:"ended_at"`
}

func (s *Store) SeedPerfData(ctx context.Context, opts PerfSeedOptions) (PerfSeedResult, error) {
	if opts.Project == "" {
		return PerfSeedResult{}, fmt.Errorf("project is required")
	}
	if opts.RunID == "" {
		opts.RunID = "perf"
	}
	if opts.Sessions <= 0 {
		opts.Sessions = 100
	}
	if opts.Memories < 0 || opts.Observations < 0 {
		return PerfSeedResult{}, fmt.Errorf("memories and observations must be non-negative")
	}
	if opts.Now == 0 {
		opts.Now = 1
	}
	startedAt := opts.Now - int64(opts.Sessions)*60_000
	endedAt := opts.Now

	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return PerfSeedResult{}, fmt.Errorf("begin perf seed: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	obsCounts := make([]int, opts.Sessions)
	if err := clearPerfSeedData(ctx, tx, s.fts5, opts.Project, opts.RunID); err != nil {
		return PerfSeedResult{}, err
	}
	for i := 0; i < opts.Sessions; i++ {
		externalID := PerfSessionExternalID(opts.Project, opts.RunID, i)
		sessionID := "opencode:" + externalID
		if _, err := tx.ExecContext(ctx, `
INSERT INTO sessions (id, agent, external_id, project, started_at, ended_at, summary, n_obs)
VALUES (?, 'opencode', ?, ?, ?, ?, ?, 0)
ON CONFLICT(agent, external_id) DO NOTHING
`, sessionID, externalID, opts.Project, startedAt+int64(i)*60_000, startedAt+int64(i)*60_000+30_000, perfSummary(opts.Project, opts.RunID, i)); err != nil {
			return PerfSeedResult{}, fmt.Errorf("insert perf session: %w", err)
		}
	}

	for i := 0; i < opts.Memories; i++ {
		sessionID := "opencode:" + PerfSessionExternalID(opts.Project, opts.RunID, i%opts.Sessions)
		text := perfMemoryText(opts.Project, opts.RunID, i)
		result, err := tx.ExecContext(ctx, `
INSERT INTO memories (project, text, tier, source, importance, session_id, created_at, updated_at, accessed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, opts.Project, text, perfTier(i), "perf-seed:"+opts.RunID, 0.5+float64(i%50)/100, sessionID, startedAt+int64(i), startedAt+int64(i), startedAt+int64(i))
		if err != nil {
			return PerfSeedResult{}, fmt.Errorf("insert perf memory: %w", err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			return PerfSeedResult{}, fmt.Errorf("perf memory id: %w", err)
		}
		if s.fts5 {
			if _, err := tx.ExecContext(ctx, `INSERT INTO memories_fts (rowid, text) VALUES (?, ?)`, id, text); err != nil {
				return PerfSeedResult{}, fmt.Errorf("insert perf memory fts: %w", err)
			}
		}
	}

	for i := 0; i < opts.Observations; i++ {
		sessionIdx := i % opts.Sessions
		sessionID := "opencode:" + PerfSessionExternalID(opts.Project, opts.RunID, sessionIdx)
		payload, kind, tool, err := perfObservationPayload(opts.Project, opts.RunID, i)
		if err != nil {
			return PerfSeedResult{}, err
		}
		encoded, encoding, err := encodePayload(payload)
		if err != nil {
			return PerfSeedResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO observations (session_id, cwd, ts, kind, tool, payload, payload_len, payload_encoding, schema_version, hash)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?)
`, sessionID, opts.Project, startedAt+int64(i), kind, tool, encoded, len(payload), encoding, perfHash(payload, i)); err != nil {
			return PerfSeedResult{}, fmt.Errorf("insert perf observation: %w", err)
		}
		obsCounts[sessionIdx]++
	}
	for i, count := range obsCounts {
		if _, err := tx.ExecContext(ctx, `UPDATE sessions SET n_obs = ? WHERE id = ?`, count, "opencode:"+PerfSessionExternalID(opts.Project, opts.RunID, i)); err != nil {
			return PerfSeedResult{}, fmt.Errorf("update perf session observations: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return PerfSeedResult{}, fmt.Errorf("commit perf seed: %w", err)
	}
	committed = true
	return PerfSeedResult{Project: opts.Project, RunID: opts.RunID, Sessions: opts.Sessions, Memories: opts.Memories, Observations: opts.Observations, Summaries: opts.Sessions, StartedAt: startedAt, EndedAt: endedAt}, nil
}

func clearPerfSeedData(ctx context.Context, tx *sql.Tx, fts5 bool, project, runID string) error {
	source := "perf-seed:" + runID
	if fts5 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM memories_fts WHERE rowid IN (SELECT id FROM memories WHERE project = ? AND source = ?)`, project, source); err != nil {
			return fmt.Errorf("clear perf memory fts: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memories WHERE project = ? AND source = ?`, project, source); err != nil {
		return fmt.Errorf("clear perf memories: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM observations WHERE cwd = ? AND session_id IN (SELECT id FROM sessions WHERE agent = 'opencode' AND project = ? AND external_id LIKE ?)`, project, project, perfSessionExternalIDPrefix(project, runID)+"%"); err != nil {
		return fmt.Errorf("clear perf observations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE agent = 'opencode' AND project = ? AND external_id LIKE ?`, project, perfSessionExternalIDPrefix(project, runID)+"%"); err != nil {
		return fmt.Errorf("clear perf sessions: %w", err)
	}
	return nil
}

func PerfSessionExternalID(project, runID string, i int) string {
	return fmt.Sprintf("%s%06d", perfSessionExternalIDPrefix(project, runID), i+1)
}

func perfSessionExternalIDPrefix(project, runID string) string {
	return "perf-" + perfSeedScopeKey(project, runID) + "-session-"
}

func perfSeedScopeKey(project, runID string) string {
	sum := sha256.Sum256([]byte(project + "\x00" + runID))
	return hex.EncodeToString(sum[:4])
}

func perfSummary(project, runID string, i int) string {
	return fmt.Sprintf("Perf summary %06d for %s run %s: auth uses JWT middleware, deploy uses docker compose, memory search relies on sqlite fts5 and optional ollama embeddings.", i+1, project, runID)
}

func perfTier(i int) string {
	switch i % 4 {
	case 0:
		return "semantic"
	case 1:
		return "procedural"
	case 2:
		return "episodic"
	default:
		return "working"
	}
}

func perfMemoryText(project, runID string, i int) string {
	switch i % 6 {
	case 0:
		return fmt.Sprintf("%s run %s memory %06d: JWT bearer auth is validated in src/middleware/auth_%03d.ts with jose middleware", project, runID, i, i%100)
	case 1:
		return fmt.Sprintf("%s run %s memory %06d: Docker compose starts mcb and ollama; embeddings use nomic-embed-text", project, runID, i)
	case 2:
		return fmt.Sprintf("%s run %s memory %06d: SQLite FTS5 BM25 handles local lexical memory search before vector fusion", project, runID, i)
	case 3:
		return fmt.Sprintf("%s run %s memory %06d: file README.md and internal/server/server.go are common debugging entry points", project, runID, i)
	case 4:
		return fmt.Sprintf("%s run %s memory %06d: rate limits use a token bucket per API key and return 429 on overflow", project, runID, i)
	default:
		return fmt.Sprintf("%s run %s memory %06d: prefer docker compose exec mcb for maintenance commands inside the container", project, runID, i)
	}
}

func perfObservationPayload(project, runID string, i int) ([]byte, string, string, error) {
	kinds := []string{"tool_use", "user_message", "assistant_message", "session_status", "task_completed"}
	kind := kinds[i%len(kinds)]
	tool := ""
	if kind == "tool_use" {
		tool = []string{"Read", "Grep", "Bash", "Edit"}[i%4]
	}
	payload := map[string]any{
		"run_id":  runID,
		"index":   i,
		"message": fmt.Sprintf("perf event %06d for %s with auth jwt sqlite docker compose ollama context", i, project),
		"file":    fmt.Sprintf("internal/perf/file_%03d.go", i%100),
		"token":   fmt.Sprintf("sk-test-%032d", i),
	}
	if i%10 == 0 {
		payload["duplicate_marker"] = "dedup-shape"
	}
	if i%7 == 0 {
		payload["tool_input"] = map[string]any{"file_path": fmt.Sprintf("src/module_%03d.go", i%200), "pattern": "memory search"}
		payload["tool_response"] = stringsOfSize(900 + i%200)
	}
	data, err := json.Marshal(payload)
	return data, kind, tool, err
}

func stringsOfSize(n int) string {
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = byte('a' + i%26)
	}
	return string(buf)
}

func perfHash(payload []byte, i int) string {
	sum := sha256.Sum256(append(payload, byte(i%251)))
	return hex.EncodeToString(sum[:])
}
