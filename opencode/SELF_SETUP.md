# OpenCode Self-Setup For mcb

You are OpenCode configuring this repository's Memory Claude Bank integration for the current user.

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

## 2. Locate OpenCode Config

Use the first existing config path from this list:

- project `opencode.json`
- project `opencode.jsonc`
- user `~/.config/opencode/opencode.json`
- user `~/.config/opencode/opencode.jsonc`

If none exists, create `opencode.json` in the current project unless the user asks for global setup.

Source reference: `deploy/opencode.example.json`.

## 3. Merge OpenCode Config

Required config:

```json
{
  "plugin": [
    "./opencode/plugin/mcb.ts"
  ],
  "mcp": {
    "mcb": {
      "type": "remote",
      "url": "http://127.0.0.1:3411/mcp"
    }
  }
}
```

Merge rules:

- Preserve existing plugins.
- Add `./opencode/plugin/mcb.ts` only if not already present.
- Preserve existing MCP servers.
- Add or update only `mcp.mcb`.
- Do not delete unrelated fields.

If `MCB_BEARER_TOKEN` is set for the mcb container, configure the OpenCode MCP headers if this OpenCode version supports remote MCP headers. The plugin already reads `MCB_BEARER_TOKEN` from the environment for HTTP integration calls.

## 4. Install The Compactor Agent

Install `opencode/agent/mcb-compactor.md` into the configured OpenCode agent directory.

Common choices:

- project-local agent directory if the project already uses one
- user-global `~/.config/opencode/agent/`

If no convention is present, prefer project-local setup and create:

```bash
mkdir -p .opencode/agent
cp opencode/agent/mcb-compactor.md .opencode/agent/mcb-compactor.md
```

Do not alter the agent name. It must remain `mcb-compactor` unless the mcb config `[compaction].subagent_name` is changed too.

## 5. Validate Config

For `.json` files, run:

```bash
python -m json.tool <config-path> >/dev/null
```

For `.jsonc` files, do not use `python -m json.tool` directly because comments may be valid. Use OpenCode's own config validation if available, or preserve the existing JSONC style carefully.

## 6. Validate MCP

Run:

```bash
curl -fsS -X POST http://127.0.0.1:3411/mcp \
  -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"opencode-self-setup","version":"0"}}}'
```

Expected response contains `"serverInfo":{"name":"mcb"`.

## 7. Smoke Test Memory Save

Run:

```bash
docker compose -f deploy/docker-compose.yml exec mcb mcb add --project "$PWD" "OpenCode mcb self-setup completed"
docker compose -f deploy/docker-compose.yml exec mcb mcb search --project "$PWD" "self-setup completed"
```

Expected result contains the saved memory text.

## 8. Finish

Tell the user:

- whether mcb is running
- which OpenCode config file was modified
- whether the plugin entry was added
- whether the compactor agent was installed
- whether MCP initialize succeeded
- whether the smoke test succeeded
- that OpenCode should be restarted to reload config, plugin, and agents
