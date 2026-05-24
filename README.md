# Memory Claude Bank

`mcb` is a local persistent memory layer for Claude Code and OpenCode. Phase 1 provides capture, manual memory save/search, BM25 lexical recall, Docker deployment files, and basic hook/plugin examples.

## Quick Start

```bash
docker compose -f deploy/docker-compose.yml up -d --build
curl -fsS http://127.0.0.1:3411/healthz
```

The container stores SQLite data in the `mcb-data` Docker named volume at `/var/lib/mcb/memory.db`.

## Local Development

```bash
go test ./...
go test -tags sqlite_fts5 ./...
go run -tags sqlite_fts5 ./cmd/mcb serve --db /tmp/mcb.db --http 127.0.0.1:3411
```

FTS5/BM25 commands require `-tags sqlite_fts5` when running from source.

## CLI

```bash
mcb healthz
mcb version
mcb add --db /var/lib/mcb/memory.db --project /path/to/project "fact text"
mcb search --db /var/lib/mcb/memory.db --project /path/to/project "query"
mcb sessions --db /var/lib/mcb/memory.db --project /path/to/project
mcb backup --db /var/lib/mcb/memory.db --out backup.db
mcb backup --db /var/lib/mcb/memory.db --out - > backup.db
mcb doctor --db /var/lib/mcb/memory.db
```

## Claude Code Hooks

Use `deploy/claude-settings.example.json` as a merge reference for `~/.claude/settings.json`. The example sends SessionStart, PostToolUse, UserPromptSubmit, Stop, and SubagentStop payloads to `http://127.0.0.1:3411`.

If `MCB_BEARER_TOKEN` is set for the server, add `-H "Authorization: Bearer $MCB_BEARER_TOKEN"` to each curl command.

## OpenCode

Use `deploy/opencode.example.json` and `opencode/plugin/mcb.ts` as the Phase 1 plugin skeleton. The plugin posts context, tool, chat, and compact lifecycle events to mcb.

## Backup And Restore

Create a consistent SQLite backup:

```bash
docker exec mcb mcb backup --out /tmp/memory.db.backup
```

To restore offline, stop the container, replace the database file in the volume with the backup, then start the container again. Phase 1 does not provide migration rollback.

## Troubleshooting

- `curl: (7) Failed to connect`: check `docker ps`, `docker logs mcb`, and that port `127.0.0.1:3411` is published.
- `SQLITE_BUSY`: writes are serialized through one write connection and `_busy_timeout=5000`; persistent busy errors usually mean another process is holding the DB.
- `sqlite fts5 unavailable`: rebuild/run with `-tags sqlite_fts5` or use the Docker image.
- Corrupt config or permissions: run `mcb doctor --db /var/lib/mcb/memory.db`.
- Settings merge: inspect existing JSON first and merge hook arrays carefully; `jq` is useful for validation.

## Phase 1 Limits

Phase 1 does not include embeddings, MCP tools, vector search, decay, stop blocking, or compactor subagent orchestration. Compactor agent files are placeholders for Phase 4.
