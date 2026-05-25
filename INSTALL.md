# Install Memory Claude Bank

This guide installs `mcb` as a local Docker service for Claude Code and OpenCode.

Target setup: macOS Apple Silicon, Docker Desktop, Claude Code or OpenCode.

## Requirements

- Docker Desktop with Compose v2.
- `curl` on the host.
- Claude Code and/or OpenCode, depending on which integration you use.

No `mcb` binary needs to be installed on the host for the default Docker setup.

## Start The Service

From the repository root:

```bash
docker compose -f deploy/docker-compose.yml up -d --build
curl -fsS http://127.0.0.1:3411/healthz
curl -fsS http://127.0.0.1:3411/readyz
```

Data is stored in the Docker named volume `mcb-data` at `/var/lib/mcb/memory.db` inside the container.

## Optional: Enable Ollama Embeddings

The base deployment starts with embeddings disabled. To run Ollama as a neighboring compose container:

```bash
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.ollama.yml up -d --build
docker exec mcb-ollama ollama pull nomic-embed-text
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.ollama.yml exec mcb mcb doctor
```

In this mode `mcb` reaches Ollama at `http://ollama:11434` inside the Docker network. Ollama does not need to run on the macOS host.

After adding memories, backfill embeddings:

```bash
docker compose -f deploy/docker-compose.yml exec mcb mcb embed-missing --project /path/to/project
```

To rebuild vectors after changing the model:

```bash
docker compose -f deploy/docker-compose.yml exec mcb mcb embed-rebuild --project /path/to/project
```

## Claude Code Setup

If Claude Code is configuring itself, give it `claude/SELF_SETUP.md` and ask it to follow the file exactly.

Merge `deploy/claude-settings.example.json` into `~/.claude/settings.json`.

It configures:

- MCP server: `http://127.0.0.1:3411/mcp`
- hooks for SessionStart, PostToolUse, UserPromptSubmit, Stop, and SubagentStop

Install the compactor agent:

```bash
mkdir -p ~/.claude/agents
cp claude/agents/mcb-compactor.md ~/.claude/agents/mcb-compactor.md
```

Restart Claude Code after changing settings or agent files.

## OpenCode Setup

If OpenCode is configuring itself, give it `opencode/SELF_SETUP.md` and ask it to follow the file exactly.

Use `deploy/opencode.example.json` as a merge reference for your OpenCode config.

It configures:

- remote MCP server: `http://127.0.0.1:3411/mcp`
- plugin: `opencode/plugin/mcb.ts`

Install `opencode/agent/mcb-compactor.md` into your configured OpenCode agent directory.

Restart OpenCode after changing config, plugin, or agent files.

## Optional: Bearer Token

If you expose `mcb` beyond localhost or want local request auth, set `MCB_BEARER_TOKEN` for the container.

All non-health endpoints then require:

```text
Authorization: Bearer <token>
```

Update hook `curl` commands and MCP client headers accordingly. `/healthz` and `/readyz` stay unauthenticated.

## Basic Smoke Test

Add and search a memory:

```bash
docker compose -f deploy/docker-compose.yml exec mcb mcb add --project /tmp/mcb-smoke "mcb smoke test memory"
docker compose -f deploy/docker-compose.yml exec mcb mcb search --project /tmp/mcb-smoke "smoke test"
```

Check MCP initialize:

```bash
curl -fsS -X POST http://127.0.0.1:3411/mcp \
  -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}'
```

## Compaction

Default compaction mode is `subagent`.

When enough observations are captured, the Claude Code Stop hook returns a `decision: block` instruction to dispatch `mcb-compactor`. OpenCode uses `/integrations/opencode/compact` for the plugin-driven compactor flow.

Manual fallback:

```bash
docker compose -f deploy/docker-compose.yml exec mcb mcb compact --session claude-code:SESSION --agent claude-code
docker compose -f deploy/docker-compose.yml exec mcb mcb compact --session opencode:SESSION --agent opencode
```

To disable Stop blocking while keeping manual prompts, set:

```toml
[compaction]
mode = "manual"
```

To disable compaction requests entirely:

```toml
[compaction]
mode = "disabled"
```

## Backup

Create a backup file inside the container:

```bash
docker compose -f deploy/docker-compose.yml exec mcb mcb backup --out /tmp/memory.db.backup
```

Stream a backup to the host:

```bash
docker compose -f deploy/docker-compose.yml exec mcb mcb backup --out - > memory.db.backup
```

Restore offline: stop the container, replace the database file in the `mcb-data` volume with the backup, then start the service again.

## Upgrade

From the updated repository root:

```bash
docker compose -f deploy/docker-compose.yml up -d --build
```

With Ollama sidecar:

```bash
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.ollama.yml up -d --build
```

Migrations run automatically on startup. Create a backup before upgrading if the database contains important memories.

## Stop Or Uninstall

Stop containers but keep data:

```bash
docker compose -f deploy/docker-compose.yml down
```

With Ollama sidecar:

```bash
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.ollama.yml down
```

Remove `mcb` data permanently:

```bash
docker volume rm yet-another-memory-bank-67_mcb-data
```

Remove Ollama sidecar data permanently:

```bash
docker volume rm mcb-ollama-data
```

## Troubleshooting

- `curl: (7) Failed to connect`: check `docker compose -f deploy/docker-compose.yml ps`, `docker compose -f deploy/docker-compose.yml logs mcb`, and port `127.0.0.1:3411`.
- `sqlite fts5 unavailable`: use the Docker image or run local development commands with `-tags sqlite_fts5`.
- Ollama timeout: run `docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.ollama.yml exec mcb mcb doctor`; search falls back to BM25-only when embeddings are unavailable.
- Claude Code settings issues: merge JSON carefully; do not replace unrelated existing hooks unless intended.
- OpenCode plugin issues: confirm `MCB_URL` points to `http://127.0.0.1:3411` or leave it unset for the default.
