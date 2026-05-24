# Complete Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the remaining Phase 1 scope from `arch.md`: production-shaped SQLite migrations, full Phase 1 CLI/admin surface, hook/context endpoints, Docker/deploy examples, and README documentation.

**Architecture:** Keep the current small Go architecture: `cmd/mcb` only parses/dispatches commands, `internal/admin` owns CLI behavior, `internal/store` owns SQLite schema and queries, `internal/server` owns HTTP routing/middleware, and `internal/integrations` owns agent payload normalization. Phase 1 remains BM25-only and does not add embeddings, MCP tools, vector search, decay, or subagent orchestration.

**Tech Stack:** Go 1.26, `net/http`, SQLite via `github.com/mattn/go-sqlite3`, FTS5 with `-tags sqlite_fts5`, TOML via `github.com/pelletier/go-toml/v2`, Docker Compose for deployment.

---

## File Structure

- Modify: `cmd/mcb/main.go` to dispatch `serve`, `migrate`, `add`, `search`, `sessions`, `backup`, `doctor`, `healthz`, and `version`.
- Modify: `internal/admin/run.go` to implement `migrate`, `sessions`, `backup`, and `doctor` next to existing `add/search` commands.
- Create: `internal/admin/backup.go` for SQLite backup implementation using the sqlite3 backup API and temp+rename semantics.
- Create: `internal/admin/doctor.go` for config, DB, permissions, and optional Ollama reachability checks.
- Modify: `internal/store/store.go` to use embedded SQL migrations instead of inline schema SQL.
- Create: `internal/store/migrations.go` to apply ordered migrations and maintain `schema_version`.
- Create: `internal/store/migrations/001_init.sql` containing sessions, observations, memories, and FTS schema.
- Modify: `internal/store/sessions.go` to add `EndSession`, `ListSessions`, and `SaveSessionSummary` helpers.
- Modify: `internal/store/observations.go` to add `ObservationCountByHash` only if needed by tests.
- Modify: `internal/server/server.go` to add middleware, bearer-token auth, user prompt capture, stop/subagent-stop no-op endpoints, and config-driven limits.
- Modify: `internal/integrations/types.go`, `claude.go`, `opencode.go` to normalize user-prompt and stop/context payloads.
- Create: `deploy/claude-settings.example.json` with curl hooks for SessionStart, PostToolUse, UserPromptSubmit, Stop, and SubagentStop.
- Create: `deploy/opencode.example.json` with plugin and remote MCP placeholders compatible with Phase 1.
- Create: `opencode/plugin/mcb.ts` for basic OpenCode capture/context HTTP calls.
- Create: `opencode/agent/mcb-compactor.md` skeleton explicitly marked Phase 4 inactive.
- Create: `claude/agents/mcb-compactor.md` skeleton explicitly marked Phase 4 inactive.
- Create: `README.md` with install, Docker, hook configuration, troubleshooting, backup/restore, and Phase 1 limitations.

## Task 1: Replace Inline Schema With Embedded Migration

**Files:**
- Create: `internal/store/migrations/001_init.sql`
- Create: `internal/store/migrations.go`
- Modify: `internal/store/store.go`
- Test: `internal/store/store_test.go`

- [ ] **Step 1: Write failing migration test**

Append this test to `internal/store/store_test.go`:

```go
func TestOpenAppliesSchemaVersionMigration(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	var version int
	err = s.readDB.QueryRowContext(ctx, `SELECT version FROM schema_version ORDER BY version DESC LIMIT 1`).Scan(&version)
	if err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != 1 {
		t.Fatalf("version = %d, want 1", version)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store -run TestOpenAppliesSchemaVersionMigration -count=1`

Expected: FAIL with `no such table: schema_version`.

- [ ] **Step 3: Create migration SQL**

Create `internal/store/migrations/001_init.sql` with the exact schema currently created in `store.go`, plus:

```sql
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL
);
```

The file must include sessions, observations, memories, indexes, and `memories_fts`.

- [ ] **Step 4: Implement migration applier**

Create `internal/store/migrations.go` with `//go:embed migrations/*.sql`, `applyMigrations(ctx)` and `applyMigration(ctx, version, sqlText)` using `BEGIN IMMEDIATE`, `schema_version`, and `INSERT INTO schema_version` after each successful migration.

- [ ] **Step 5: Replace inline migration**

Change `Store.migrate(ctx)` in `internal/store/store.go` so it only calls `s.applyMigrations(ctx)` and handles `no such module: fts5` by applying non-FTS schema plus setting `s.fts5=false`.

- [ ] **Step 6: Verify**

Run: `gofmt -w internal/store && go test ./internal/store -run TestOpenAppliesSchemaVersionMigration -count=1 && go test -tags sqlite_fts5 ./internal/store -count=1`

Expected: PASS.

- [ ] **Step 7: Commit**

Run:

```bash
git add internal/store
git commit -m "feat: add sqlite migration runner"
```

## Task 2: Complete Session Store and `mcb sessions`

**Files:**
- Modify: `internal/store/sessions.go`
- Modify: `internal/admin/run.go`
- Test: `internal/store/store_test.go`
- Test: `internal/admin/sessions_test.go`

- [ ] **Step 1: Write failing store test**

Append to `internal/store/store_test.go`:

```go
func TestListSessionsAndEndSession(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	id, err := s.EnsureSession(ctx, "claude-code", "s1", "/repo", 1000)
	if err != nil {
		t.Fatalf("ensure session: %v", err)
	}
	if err := s.EndSession(ctx, id, 2000); err != nil {
		t.Fatalf("end session: %v", err)
	}

	sessions, err := s.ListSessions(ctx, "/repo", 10)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != id || sessions[0].EndedAt != 2000 {
		t.Fatalf("sessions = %+v", sessions)
	}
}
```

- [ ] **Step 2: Verify store RED**

Run: `go test ./internal/store -run TestListSessionsAndEndSession -count=1`

Expected: build failure for missing `EndSession`, `ListSessions`, or `EndedAt`.

- [ ] **Step 3: Implement store methods**

Add `EndedAt int64` and `Summary string` to `Session`. Implement:

```go
func (s *Store) EndSession(ctx context.Context, id string, endedAt int64) error
func (s *Store) ListSessions(ctx context.Context, project string, limit int) ([]Session, error)
func (s *Store) SaveSessionSummary(ctx context.Context, id, summary string) error
```

`ListSessions` filters by project if project is non-empty, orders by `started_at DESC`, caps limit at 100, and returns empty slice on no rows.

- [ ] **Step 4: Write failing admin test**

Create `internal/admin/sessions_test.go`:

```go
package admin

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alice/mcb/internal/store"
)

func TestRunSessionsListsSessions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	_, err = s.EnsureSession(context.Background(), "claude-code", "s1", "/repo", 1000)
	if err != nil {
		t.Fatalf("ensure session: %v", err)
	}
	s.Close()

	io := IO{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getwd: func() (string, error) { return "/repo", nil }}
	code := Run(context.Background(), []string{"sessions", "--db", dbPath, "--project", "/repo"}, io)
	if code != 0 {
		t.Fatalf("sessions exit code = %d stderr=%s", code, io.Stderr)
	}
	if got := io.Stdout.(*bytes.Buffer).String(); !strings.Contains(got, "claude-code:s1") || !strings.Contains(got, "/repo") {
		t.Fatalf("sessions stdout = %q", got)
	}
}
```

- [ ] **Step 5: Verify admin RED**

Run: `go test ./internal/admin -run TestRunSessionsListsSessions -count=1`

Expected: FAIL with unsupported command `sessions`.

- [ ] **Step 6: Implement `sessions` command**

Add command dispatch in `internal/admin/run.go`, parse `--db`, `--project`, `--limit`, call `store.ListSessions`, print tab-separated `id agent project started_at ended_at n_obs`.

- [ ] **Step 7: Verify**

Run: `gofmt -w internal/store internal/admin && go test ./internal/store ./internal/admin -run 'TestListSessionsAndEndSession|TestRunSessionsListsSessions' -count=1`

Expected: PASS.

- [ ] **Step 8: Commit**

Run:

```bash
git add internal/store internal/admin
git commit -m "feat: add session admin listing"
```

## Task 3: Add Hook Endpoints for User Prompt and Stop Lifecycle

**Files:**
- Modify: `internal/integrations/types.go`
- Modify: `internal/integrations/claude.go`
- Modify: `internal/integrations/opencode.go`
- Modify: `internal/server/server.go`
- Test: `internal/integrations/integrations_test.go`
- Test: `internal/server/server_test.go`

- [ ] **Step 1: Write failing integration tests**

Add tests for `NormalizeClaudeUserPrompt`, `NormalizeOpenCodeChat`, and stop payload structs. Claude user-prompt raw JSON must be `{"session_id":"s1","cwd":"/repo","prompt":"remember this"}` and produce `Kind: "user_message"`, `Tool: ""`, payload containing `prompt`.

- [ ] **Step 2: Verify integration RED**

Run: `go test ./internal/integrations -run 'UserPrompt|OpenCodeChat' -count=1`

Expected: build failure for missing normalizers.

- [ ] **Step 3: Implement normalizers**

Add `NormalizeClaudeUserPrompt(raw []byte) (Event, error)` and `NormalizeOpenCodeChat(raw []byte) (Event, error)`. Payload JSON must be compact JSON with `prompt` or `message` only.

- [ ] **Step 4: Write failing server endpoint tests**

Add tests proving `/hooks/user-prompt`, `/integrations/opencode/chat`, `/hooks/stop`, `/hooks/subagent-stop`, and `/integrations/opencode/compact` exist. User prompt/chat endpoints must store observations; stop endpoints must call `EndSession` and return 204 or a no-op JSON response.

- [ ] **Step 5: Verify server RED**

Run: `go test ./internal/server -run 'UserPrompt|OpenCodeChat|Stop' -count=1`

Expected: FAIL with 404s.

- [ ] **Step 6: Implement endpoints**

Add routes:

```text
POST /hooks/user-prompt
POST /hooks/stop
POST /hooks/subagent-stop
POST /integrations/opencode/chat
POST /integrations/opencode/compact
```

`/hooks/stop` and `/hooks/subagent-stop` parse `session_id,cwd`, ensure/end session, and return 204. `/integrations/opencode/compact` returns `{"compact":false,"reason":"phase 1 compaction is disabled"}`.

- [ ] **Step 7: Verify**

Run: `gofmt -w internal/integrations internal/server && go test ./internal/integrations ./internal/server -count=1`

Expected: PASS.

- [ ] **Step 8: Commit**

Run:

```bash
git add internal/integrations internal/server
git commit -m "feat: add phase 1 lifecycle hooks"
```

## Task 4: Add HTTP Middleware and Bearer Token Auth

**Files:**
- Modify: `internal/server/server.go`
- Test: `internal/server/server_test.go`

- [ ] **Step 1: Write failing auth test**

Add test that constructs `server.New(s, server.Options{BearerToken:"secret"})` or equivalent and verifies `/healthz` remains public, `/hooks/post-tool` returns 401 without `Authorization: Bearer secret`, and succeeds with the header.

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/server -run TestBearerTokenProtectsNonHealthEndpoints -count=1`

Expected: build failure for missing `Options` or FAIL because endpoint is unprotected.

- [ ] **Step 3: Implement middleware**

Add `type Options struct { BearerToken string; DedupWindowSeconds int64; SessionStartTopN int }`, make `New(s)` call `NewWithOptions(s, Options{})`, and wrap mux with `recover`, auth, and request logging middleware. Auth skips only `/healthz` and `/readyz`.

- [ ] **Step 4: Verify**

Run: `gofmt -w internal/server && go test ./internal/server -run TestBearerTokenProtectsNonHealthEndpoints -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```bash
git add internal/server
git commit -m "feat: add http auth middleware"
```

## Task 5: Add `mcb backup`

**Files:**
- Create: `internal/admin/backup.go`
- Modify: `internal/admin/run.go`
- Test: `internal/admin/backup_test.go`

- [ ] **Step 1: Write failing backup tests**

Create `internal/admin/backup_test.go` with two tests: `backup --out PATH` creates a readable SQLite DB containing the inserted memory, and `backup --out -` writes non-empty bytes to stdout while stderr remains empty.

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/admin -run TestRunBackup -count=1`

Expected: FAIL with unsupported command `backup`.

- [ ] **Step 3: Implement backup**

Use sqlite3 backup API through `database/sql.Conn.Raw`. For `--out PATH`, write to a temp file in the destination directory and rename. For `--out -`, write a temp backup to `os.TempDir()`, stream the bytes to stdout, and remove the temp file.

- [ ] **Step 4: Verify**

Run: `gofmt -w internal/admin && go test ./internal/admin -run TestRunBackup -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```bash
git add internal/admin
git commit -m "feat: add sqlite backup command"
```

## Task 6: Add `mcb doctor`

**Files:**
- Create: `internal/admin/doctor.go`
- Modify: `internal/admin/run.go`
- Test: `internal/admin/doctor_test.go`

- [ ] **Step 1: Write failing doctor tests**

Create tests for corrupt config (`--config corrupt.toml` returns non-zero and mentions `config`), missing DB (`--db missing.db` returns non-zero and mentions `missing db`), and valid DB (`doctor --db existing.db` returns 0 and prints `ok`).

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/admin -run TestRunDoctor -count=1`

Expected: FAIL with unsupported command `doctor`.

- [ ] **Step 3: Implement doctor**

Parse `--config`, `--db`, and optional `--ollama-url`. Load config if provided, require DB file exists, open store read/write, call `Ping`, create/remove a temp file in DB parent to detect permissions, and print one line per check.

- [ ] **Step 4: Verify**

Run: `gofmt -w internal/admin && go test ./internal/admin -run TestRunDoctor -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```bash
git add internal/admin
git commit -m "feat: add doctor command"
```

## Task 7: Wire Config Into Server Options

**Files:**
- Modify: `cmd/mcb/main.go`
- Modify: `internal/server/server.go`
- Test: `cmd/mcb/main_serve_test.go`

- [ ] **Step 1: Write failing test**

Add test that writes a TOML config with `[capture].dedup_window_seconds = 1` and `[memory].session_start_top_n = 1`, starts `serve --config path` through the `serveHTTP` seam, inserts two memories before context call, and verifies only one memory appears.

- [ ] **Step 2: Verify RED**

Run: `go test ./cmd/mcb -run TestRunServePassesConfigToServer -count=1`

Expected: FAIL because current server ignores config values.

- [ ] **Step 3: Implement wiring**

Make `runServe` pass `server.Options{DedupWindowSeconds: cfg.Capture.DedupWindowSeconds, SessionStartTopN: cfg.Memory.SessionStartTopN, BearerToken: os.Getenv("MCB_BEARER_TOKEN")}` to `server.NewWithOptions`.

- [ ] **Step 4: Verify**

Run: `gofmt -w cmd/mcb internal/server && go test ./cmd/mcb ./internal/server -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```bash
git add cmd/mcb internal/server
git commit -m "feat: apply runtime config to server"
```

## Task 8: Add Deploy Examples, Plugin Skeletons, and README

**Files:**
- Create: `deploy/claude-settings.example.json`
- Create: `deploy/opencode.example.json`
- Create: `opencode/plugin/mcb.ts`
- Create: `opencode/agent/mcb-compactor.md`
- Create: `claude/agents/mcb-compactor.md`
- Create: `README.md`

- [ ] **Step 1: Add deploy example files**

Create Claude settings JSON using curl commands for `/hooks/session-start`, `/hooks/post-tool`, `/hooks/user-prompt`, `/hooks/stop`, `/hooks/subagent-stop`. Create OpenCode JSON pointing at `opencode/plugin/mcb.ts` and remote MCP URL placeholder for later phases.

- [ ] **Step 2: Add OpenCode plugin skeleton**

Create `opencode/plugin/mcb.ts` with exported plugin hooks that call `MCB_URL || "http://127.0.0.1:3411"` endpoints for context, tool, chat, and compact. If OpenCode hook APIs differ, keep functions small and documented so users can adapt.

- [ ] **Step 3: Add compactor skeletons**

Both agent markdown files must state Phase 1 compaction is inactive and that real durable memory extraction begins in Phase 4.

- [ ] **Step 4: Add README**

Document Docker install, local dev commands, `mcb add/search/sessions/backup/doctor`, troubleshooting for `curl: (7)`, `SQLITE_BUSY`, settings merge, and Phase 1 limitations.

- [ ] **Step 5: Verify file validity**

Run: `go test ./... && go test -tags sqlite_fts5 ./...`

Expected: PASS. If `jq` is installed, run `jq . deploy/*.json`; if not installed, skip with note.

- [ ] **Step 6: Commit**

Run:

```bash
git add README.md deploy claude opencode
git commit -m "docs: add phase 1 deployment guide"
```

## Task 9: Final Phase 1 Verification

**Files:**
- No source files expected unless verification exposes defects.

- [ ] **Step 1: Run formatting and tests**

Run:

```bash
gofmt -l cmd internal
go test ./...
go test -tags sqlite_fts5 ./...
```

Expected: `gofmt` prints nothing; both test commands pass.

- [ ] **Step 2: Run CLI smoke tests**

Run:

```bash
DB=$(mktemp -d)/memory.db
go run -tags sqlite_fts5 ./cmd/mcb migrate --db "$DB"
go run -tags sqlite_fts5 ./cmd/mcb add --db "$DB" --project /repo "phase one smoke fact"
go run -tags sqlite_fts5 ./cmd/mcb search --db "$DB" --project /repo smoke
go run -tags sqlite_fts5 ./cmd/mcb sessions --db "$DB" --project /repo
go run -tags sqlite_fts5 ./cmd/mcb backup --db "$DB" --out "$DB.backup"
go run -tags sqlite_fts5 ./cmd/mcb doctor --db "$DB"
```

Expected: each command exits 0; search prints `phase one smoke fact`; backup file exists.

- [ ] **Step 3: Run Docker verification when Docker exists**

Run:

```bash
docker compose -f deploy/docker-compose.yml config
docker compose -f deploy/docker-compose.yml up -d --build
curl -fsS http://127.0.0.1:3411/healthz
docker compose -f deploy/docker-compose.yml down
```

Expected: compose config valid, container starts, healthz returns HTTP 200. If Docker CLI is absent, record `docker: command not found` as an environment limitation.

- [ ] **Step 4: Commit verification fixes if any**

Only if verification required source changes, commit them with:

```bash
git add <changed-files>
git commit -m "fix: complete phase 1 verification"
```

## Self-Review

- Spec coverage: Tasks cover remaining Phase 1 scope from `arch.md` lines 1141-1155 and readiness checks lines 1207-1221, except Docker runtime and image size which require Docker Desktop outside this environment.
- Placeholder scan: No task uses TODO/TBD. Every task has concrete files, commands, expected failures, and expected passes.
- Type consistency: `Session.EndedAt`, `server.Options`, `store.ListSessions`, `store.EndSession`, `admin.Run`, and command names are used consistently across tasks.
