# Memory Claude Bank

`mcb` is a local persistent memory layer for Claude Code and OpenCode. It provides capture, manual memory save/search, BM25 lexical recall, optional Ollama embeddings, hybrid BM25+vector search, transcript import, replay-friendly timelines, a Streamable HTTP MCP endpoint at `/mcp`, and agent-native session compaction orchestration.

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

## Ollama Embeddings

Base deployment starts with embeddings disabled. The supported embeddings deployment runs Ollama as a neighboring compose container:

```bash
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.ollama.yml up -d --build
docker exec mcb-ollama ollama pull nomic-embed-text
```

In this mode `mcb` reaches Ollama via Docker DNS at `http://ollama:11434`; Ollama does not need to run on the macOS host.

## CLI

```bash
mcb healthz
mcb version
mcb add --db /var/lib/mcb/memory.db --project /path/to/project "fact text"
mcb search --db /var/lib/mcb/memory.db --project /path/to/project "query"
mcb embed-missing --db /var/lib/mcb/memory.db --project /path/to/project
mcb embed-rebuild --db /var/lib/mcb/memory.db --project /path/to/project
mcb compact --db /var/lib/mcb/memory.db --session claude-code:SESSION --agent claude-code
mcb decay --db /var/lib/mcb/memory.db
mcb sessions --db /var/lib/mcb/memory.db --project /path/to/project
mcb import-jsonl --db /var/lib/mcb/memory.db ~/.claude/projects
mcb backup --db /var/lib/mcb/memory.db --out backup.db
mcb backup --db /var/lib/mcb/memory.db --out - > backup.db
mcb doctor --db /var/lib/mcb/memory.db
```

`import-jsonl` accepts one Claude transcript JSONL file or a directory tree. Imports are idempotent by transcript path plus event identity, preserve source timestamps when present, and redact known secret patterns before storage.

## Replay API

Replay records are available over HTTP and MCP for future viewers or agent handoffs:

```bash
curl -fsS http://127.0.0.1:3411/integrations/replay/session \
  -H 'Content-Type: application/json' \
  -d '{"session_id":"claude-code:SESSION","limit":100}'
```

The response contains ordered `events` with stable IDs, timestamps, actor/type, tool name, `payload_preview`, and redacted `payload_detail`.

## Claude Code Hooks

Use `deploy/claude-settings.example.json` as a merge reference for `~/.claude/settings.json`. The example configures remote MCP tools at `http://127.0.0.1:3411/mcp` and sends 12 hook payloads to `http://127.0.0.1:3411`: SessionStart, UserPromptSubmit, PreToolUse, PostToolUse, PostToolUseFailure, PreCompact, SubagentStart, SubagentStop, Notification, TaskCompleted, Stop, and SessionEnd.

Copy `claude/agents/mcb-compactor.md` to `~/.claude/agents/mcb-compactor.md`. When enough observations exist, the Stop hook returns a `decision: block` instruction to dispatch that subagent. The subagent reads observations through MCP, saves one session summary, and stores durable facts.

If `MCB_BEARER_TOKEN` is set for the server, add `-H "Authorization: Bearer $MCB_BEARER_TOKEN"` to each curl command.

## OpenCode

Use `deploy/opencode.example.json` and `opencode/plugin/mcb.ts`. The example configures remote MCP tools at `http://127.0.0.1:3411/mcp`; the plugin posts context, tool, chat, session lifecycle, message, part, permission, todo, command, model/config, and compact lifecycle events to mcb. It also injects memory context and per-file enrichment through OpenCode system-transform hooks when available.

Optional slash commands live in `opencode/commands/`:

- `/recall <query>` searches mcb memory.
- `/remember <text>` saves a durable memory.

Install `opencode/agent/mcb-compactor.md` in the configured OpenCode agent directory. `/integrations/opencode/compact` returns a prompt for the plugin-driven compactor flow.

## MCP Tools

The `/mcp` endpoint supports these tools:

- `memory_recall`: search memories and refresh access metadata.
- `memory_search`: search memories without refreshing access metadata.
- `memory_save`: save a durable memory fact.
- `memory_sessions`: list captured sessions.
- `memory_session_observations`: list decoded observations for a session.
- `memory_forget`: dry-run by query, or delete explicit IDs with `confirm=true`.
- `memory_profile`: return project memory/capture aggregates.
- `memory_session_summary_save`: save a compactor summary for a session.
- `memory_supersede`: mark an older memory as superseded by a newer one.
- `memory_update`: edit an existing memory's text, tier, or importance.
- `memory_timeline`: return chronological memories and observations.
- `memory_file_history`: return memories and observations related to file paths.
- `memory_patterns`: aggregate recurring tools, observation kinds, and files.
- `memory_export`: export project memories, sessions, and observations as JSON.
- `memory_audit`: list memory mutation audit events.
- `memory_verify`: trace a memory to source session observations and audit events.
- `memory_replay`: return ordered replay records for a session.

The `/mcp` endpoint also exposes resources:

- `mcb://status`: memory, session, and observation counts.
- `mcb://project/{project}/profile`: aggregate profile for one project.
- `mcb://memories/latest`: latest active memories for the default project.
- `mcb://sessions/latest`: latest sessions for the default project.
- `mcb://audit/latest`: latest mutation audit events.

MCP prompts:

- `recall_context`: generate a memory-recall prompt for the current task.
- `session_handoff`: generate a concise handoff prompt for another agent or future session.

## Compaction And Decay

Compaction is agent-native. `mcb` never calls an LLM directly and does not store provider API keys.

Relevant config defaults:

```toml
[compaction]
mode = "subagent"
min_observations = 5
max_block_attempts = 2
attempt_ttl_seconds = 600
subagent_name = "mcb-compactor"

[memory]
decay_tau_days = 30
min_importance = 0.05
decay_interval_hours = 24
```

Use `mode = "manual"` to disable Stop blocking while keeping `mcb compact` prompts available. Use `mode = "disabled"` to disable compaction requests.

## Backup And Restore

Create a consistent SQLite backup:

```bash
docker compose -f deploy/docker-compose.yml exec mcb mcb backup --out /tmp/memory.db.backup
```

To restore offline, stop the container, replace the database file in the volume with the backup, then start the container again. Phase 1 does not provide migration rollback.

## Troubleshooting

- `curl: (7) Failed to connect`: check `docker compose -f deploy/docker-compose.yml ps`, `docker compose -f deploy/docker-compose.yml logs mcb`, and that port `127.0.0.1:3411` is published.
- `SQLITE_BUSY`: writes are serialized through one write connection and `_busy_timeout=5000`; persistent busy errors usually mean another process is holding the DB.
- `sqlite fts5 unavailable`: rebuild/run with `-tags sqlite_fts5` or use the Docker image.
- Ollama timeout: with the compose overlay, run `docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.ollama.yml exec mcb mcb doctor`; search falls back to BM25-only when embeddings are unavailable.
- Corrupt config or permissions: run `mcb doctor --db /var/lib/mcb/memory.db`.
- Settings merge: inspect existing JSON first and merge hook arrays carefully; `jq` is useful for validation.

## Remaining Optional Work

Not implemented by default: native SQLite vector-search extensions, cross-encoder reranking, knowledge graph extraction, automatic LLM provider calls, and a web UI. These need separate dependency and UX decisions.
