# Claude Code Self-Setup For mcb

You are Claude Code configuring this repository's Memory Claude Bank integration for the current user.

Follow these steps exactly. Preserve existing user configuration. Do not overwrite unrelated settings.

## 1. Verify mcb Is Running

From the repository root, run:

```bash
curl -fsS http://127.0.0.1:3411/healthz
curl -fsS http://127.0.0.1:3411/readyz
```

If either command fails, start mcb:

```bash
docker compose -f deploy/docker-compose.yml up -d --build
```

If the user wants embeddings, use the Ollama sidecar instead:

```bash
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.ollama.yml up -d --build
docker exec mcb-ollama ollama pull nomic-embed-text
```

## 2. Merge Claude Settings

Target file: `~/.claude/settings.json`.

Source file: `deploy/claude-settings.example.json`.

Required config:

```json
{
  "mcpServers": {
    "mcb": {
      "type": "http",
      "url": "http://127.0.0.1:3411/mcp"
    }
  }
}
```

Required hook groups from the source file:

- `SessionStart`
- `PostToolUse`
- `UserPromptSubmit`
- `Stop`
- `SubagentStop`

Merge rules:

- If `~/.claude/settings.json` does not exist, create it from `deploy/claude-settings.example.json`.
- If it exists, parse JSON and merge only the `mcpServers.mcb` object and missing mcb hook entries.
- Do not delete existing `mcpServers` entries.
- Do not delete existing hooks.
- Avoid duplicating the same mcb curl command if it is already present.

If `MCB_BEARER_TOKEN` is set for the mcb container, update each mcb curl command to include:

```bash
-H "Authorization: Bearer $MCB_BEARER_TOKEN"
```

Also configure the MCP client headers if this Claude Code version supports MCP HTTP headers. If it does not, tell the user bearer-token MCP auth must be configured manually for their Claude Code version.

## 3. Install The Compactor Agent

Copy:

```bash
mkdir -p ~/.claude/agents
cp claude/agents/mcb-compactor.md ~/.claude/agents/mcb-compactor.md
```

Do not alter the agent name. It must remain `mcb-compactor` unless the mcb config `[compaction].subagent_name` is changed too.

## 4. Validate JSON

Run:

```bash
python -m json.tool ~/.claude/settings.json >/dev/null
```

If validation fails, fix the JSON before continuing.

## 5. Validate MCP

Run:

```bash
curl -fsS -X POST http://127.0.0.1:3411/mcp \
  -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"claude-self-setup","version":"0"}}}'
```

Expected response contains `"serverInfo":{"name":"mcb"`.

## 6. Smoke Test Memory Save

Run:

```bash
docker compose -f deploy/docker-compose.yml exec mcb mcb add --project "$PWD" "Claude Code mcb self-setup completed"
docker compose -f deploy/docker-compose.yml exec mcb mcb search --project "$PWD" "self-setup completed"
```

Expected result contains the saved memory text.

## 7. Finish

Tell the user:

- whether mcb is running
- which settings file was modified
- whether the compactor agent was installed
- whether MCP initialize succeeded
- whether the smoke test succeeded
- that Claude Code should be restarted to reload settings and agents
