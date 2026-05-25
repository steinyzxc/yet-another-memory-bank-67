# Memory Claude Bank (mcb) — Architecture

## Цель

Persistent memory layer для Claude Code и OpenCode. Деплоится одним `docker compose -f deploy/docker-compose.yml up -d --build` на хосте пользователя. Внутри контейнера — Go-демон с HTTP API (для integration hooks/plugins) и Streamable HTTP MCP transport (для tool-вызовов от агента). На хосте никаких бинарей mcb не нужно: Claude Code интегрируется через `curl` hooks, OpenCode — через plugin + MCP remote server.

Данные в SQLite, хранятся в Docker named volume. Опциональная зависимость — Ollama (на хосте или в соседнем контейнере) для эмбеддингов.

Целевая платформа фаз 1-4: **macOS Apple Silicon (arm64) + Docker Desktop + Claude Code/OpenCode**. Linux/Windows и multi-arch release — best-effort/out of scope до отдельного решения.

Функционально: запись observations из tool use и user messages, импорт Claude JSONL transcripts, replay-friendly session timelines, компрессия сессий в memories через agent-native compactor subagent (диспатчится самим Claude Code/OpenCode, не через mcb), hybrid search (BM25 + векторный) с RRF-фьюжном, инжект релевантного контекста в старт/системный контекст агента.

## Технологический стек

- **Язык**: Go 1.26
- **Деплой**: Docker Desktop на macOS arm64 (multi-stage build), Docker Compose. Один контейнер `mcb`, опционально соседний `ollama`. На хосте только `curl`.
- **БД**: SQLite через `github.com/mattn/go-sqlite3` (CGO). В контейнере CGO не проблема — build-stage компилирует, runtime-stage минимальный. Это разблокирует `sqlite-vec` extension с фазы 2 без двойной миграции драйвера.
- **BM25**: SQLite FTS5 (встроенный)
- **Vector**: фаза 2 — pure Go cosine brute force с project pre-filter (3-15ms на 5-20k per-project, см. секцию «Поиск»). Фаза 3 — `sqlite-vec` extension (SIMD-cosine в C, ×5-10 speedup, никакого Go-ассемблера писать не надо). KD-tree / ball tree / HNSW — не входят в план: при таком scale они дают экономию миллисекунд за счёт значительной complexity. KD/ball tree также вырождаются на 768-dim из-за curse of dimensionality.
- **MCP**: stdlib JSON-RPC handler over HTTP at `/mcp` (не stdio, без MCP SDK dependency).
- **Сжатие payload**: `github.com/klauspost/compress/zstd`
- **Эмбеддинги**: Ollama HTTP API. Default deploy стартует без Ollama (`provider=none`). Supported embeddings deploy использует соседний контейнер `ollama` через compose overlay и URL `http://ollama:11434`. Модель по умолчанию `nomic-embed-text`.
- **LLM-компрессия**: делегируется agent-native compactor subagent'у: Claude Code через Task tool, OpenCode через subagent + plugin/command orchestration. Mcb не вызывает LLM-провайдеров напрямую и не хранит API-ключей.
- **HTTP**: `net/http` stdlib
- **Логи**: `log/slog` stdlib, JSON в stdout (читается через `docker logs`)

Никаких ORM, web-фреймворков, DI-контейнеров. Stdlib + перечисленные библиотеки.

## Структура проекта

```
mcb/
├── cmd/
│   └── mcb/
│       └── main.go              # точка входа: serve | migrate | admin commands
├── internal/
│   ├── config/
│   │   └── config.go            # загрузка /etc/mcb/config.toml + env override
│   ├── store/
│   │   ├── store.go             # open/migrate, транзакции
│   │   ├── migrations.go        # embed SQL миграций
│   │   ├── migrations/
│   │   │   └── 001_init.sql
│   │   ├── observations.go      # CRUD observations
│   │   ├── memories.go          # CRUD memories
│   │   └── sessions.go          # CRUD sessions
│   ├── server/
│   │   ├── server.go            # http.Server, routing, middleware
│   │   ├── middleware.go        # request logging, auth, recovery
│   │   └── health.go            # /healthz, /readyz
│   ├── hooks/
│   │   ├── types.go             # normalized hook/event DTOs + Claude Code JSON
│   │   ├── handlers.go          # HTTP handlers для /hooks/* и /integrations/*
│   │   ├── session_start.go
│   │   ├── post_tool.go
│   │   ├── user_prompt.go
│   │   ├── stop.go
│   │   └── subagent_stop.go
│   ├── integrations/
│   │   ├── claude.go            # Claude Code hook payload -> normalized events
│   │   └── opencode.go          # OpenCode plugin payload -> normalized events/context
│   ├── search/
│   │   ├── bm25.go              # FTS5 запросы
│   │   ├── vector.go            # cosine similarity / sqlite-vec
│   │   ├── rrf.go               # Reciprocal Rank Fusion
│   │   └── hybrid.go            # композиция bm25 + vector + rrf
│   ├── embed/
│   │   └── ollama.go            # клиент Ollama embeddings
│   ├── secrets/
│   │   ├── patterns.go          # regex'ы секретов
│   │   └── filter.go            # redaction
│   ├── dedup/
│   │   └── hash.go              # canonical json + sha256
│   ├── mcp/
│   │   ├── server.go            # stdlib JSON-RPC MCP handler, mounted at /mcp
│   │   ├── tool_recall.go
│   │   ├── tool_save.go
│   │   ├── tool_search.go
│   │   ├── tool_sessions.go
│   │   ├── tool_session_observations.go
│   │   ├── tool_session_summary.go
│   │   ├── tool_forget.go
│   │   ├── tool_supersede.go
│   │   └── tool_profile.go
│   └── admin/
│       ├── search.go            # внутрипроцессные debug-команды через `docker compose ... exec mcb mcb ...`
│       ├── add.go
│       ├── export.go
│       └── doctor.go
├── deploy/
│   ├── Dockerfile
│   ├── docker-compose.yml
│   ├── docker-compose.ollama.yml
│   ├── config.example.toml
│   ├── claude-settings.example.json
│   └── opencode.example.json
├── test/
│   └── integration/
├── claude/
│   └── agents/
│       └── mcb-compactor.md     # subagent definition (копируется в ~/.claude/agents/)
├── opencode/
│   ├── plugin/
│   │   └── mcb.ts               # OpenCode plugin: capture/context/compaction orchestration
│   └── agent/
│       └── mcb-compactor.md     # subagent definition (копируется в .opencode/agent/ или ~/.config/opencode/agent/)
├── go.mod
├── go.sum
├── README.md
└── ARCHITECTURE.md              # этот файл
```

## Конфигурация

Внутри контейнера: `/etc/mcb/config.toml` (read-only mount или встроен в образ как default). Override через env-переменные `MCB_<SECTION>_<KEY>`.

```toml
[storage]
db_path = "/var/lib/mcb/memory.db"
# Логи идут в stdout, читаются через `docker compose -f deploy/docker-compose.yml logs mcb`

[server]
http_bind = "0.0.0.0:3411"     # внутри контейнера слушаем все интерфейсы;
                               # порт пробрасывается в 127.0.0.1:3411 на хосте
shutdown_timeout_ms = 5000

[integration]
enabled = ["claude-code", "opencode"]
default_agent = "claude-code"  # используется только когда request не передал agent

[embedding]
provider = "none"              # none | ollama; docker-compose.ollama.yml включает соседний контейнер ollama
ollama_url = ""                # если provider=ollama: http://ollama:11434
model = "nomic-embed-text"
dimensions = 0                 # 0 = auto-detect по первому embed; если >0 — валидировать len embedding
timeout_ms = 30000
circuit_breaker_failures = 3   # после N ошибок временно fallback на BM25-only
circuit_breaker_cooldown_ms = 120000

[llm]
# Mcb не вызывает LLM напрямую. Компрессия делегируется agent-native compactor subagent'у.

[compaction]
mode = "subagent"              # subagent | manual | disabled
min_observations = 5
max_observations_per_run = 100
subagent_name = "mcb-compactor"
max_block_attempts = 2
attempt_ttl_seconds = 600

[search]
bm25_top_k = 50
vector_top_k = 50
final_top_k = 10
rrf_k = 60
max_per_session = 3            # session diversification: <=N результатов из одной сессии

[capture]
dedup_window_seconds = 300
zstd_level = 3
max_observation_bytes = 65536

[memory]
session_start_top_n = 8
decay_tau_days = 30
min_importance = 0.05
decay_interval_hours = 24       # 0 = disable in-process ticker; manual `mcb decay` остаётся доступен

[security]
secret_redaction = true
bearer_token_env = "MCB_BEARER_TOKEN"  # если задан — все non-health endpoints требуют Authorization: Bearer <token>
```

Env override: `MCB_SERVER_HTTP_BIND=0.0.0.0:3411`, `MCB_EMBEDDING_PROVIDER=none`, `MCB_BEARER_TOKEN=...` и т.д.

В режиме «локально на хосте без Docker» (опциональный development mode) — путь меняется на `~/.mcb/`, bind на `127.0.0.1:3411`. Логика — в `internal/config` через флаг `--dev` или env `MCB_DEV_MODE=1`.

## Схема SQLite

Миграции через `embed` + ручной applier (не нужны библиотеки). Файлы в `internal/store/migrations/00N_*.sql`.

SQLite open policy:

- DSN включает `_foreign_keys=1` и `_busy_timeout=5000`, потому что `PRAGMA foreign_keys = ON` действует per connection.
- Store использует отдельные handles: `writeDB` с `SetMaxOpenConns(1)` для всех write transaction и `readDB` с небольшим read pool для search/admin reads. Это проще и надёжнее для SQLite WAL, чем общий пул под concurrent hooks.
- Write-path транзакции открываются как `BEGIN IMMEDIATE`, чтобы dedup check + insert были сериализованы и не ловили race между параллельными hooks.
- Admin read-only команды (`search`, `sessions`, `export`) открывают БД read-only и выставляют `PRAGMA query_only = ON`.

Migration policy: в `001_init.sql` можно использовать `NOT NULL` без `DEFAULT`, потому что БД пустая. В будущих миграциях для существующих таблиц `ADD COLUMN ... NOT NULL` только с `DEFAULT` или через copy-table migration.

Phase 3 import idempotency хранится в `imported_events`: `(transcript_path, event_id)` уникальны, `event_id` берётся из Claude transcript `uuid`/`id` или fallback `line:<n>`. Повторный `mcb import-jsonl` пропускает уже записанные события.

### 001_init.sql

```sql
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA synchronous = NORMAL;

CREATE TABLE schema_version (
    version    INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL
);

CREATE TABLE sessions (
    id          TEXT PRIMARY KEY,           -- normalized: "<agent>:<external_session_id>"
    agent       TEXT NOT NULL,              -- claude-code | opencode
    external_id TEXT NOT NULL,              -- raw session_id из агента
    project     TEXT NOT NULL,              -- CWD на момент start
    started_at  INTEGER NOT NULL,           -- unix epoch ms
    ended_at    INTEGER,
    summary     TEXT,
    n_obs       INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX idx_sessions_agent_external ON sessions(agent, external_id);
CREATE INDEX idx_sessions_project ON sessions(project);
CREATE INDEX idx_sessions_started ON sessions(started_at);

CREATE TABLE observations (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    ts          INTEGER NOT NULL,
    cwd         TEXT NOT NULL,                  -- cwd конкретного hook event; session может ходить по нескольким project
    kind        TEXT NOT NULL CHECK (kind IN ('tool_use','user_prompt','tool_error')),
    tool        TEXT,                        -- null для user_prompt
    payload     BLOB NOT NULL,               -- raw json или zstd(json), см. payload_encoding
    payload_encoding TEXT NOT NULL CHECK (payload_encoding IN ('raw','zstd')),
    payload_len INTEGER NOT NULL,            -- размер до сжатия
    schema_version INTEGER NOT NULL DEFAULT 1,
    hash        TEXT NOT NULL                -- sha256 canonical payload; не UNIQUE, дедуп window логический
);
CREATE INDEX idx_obs_session_ts ON observations(session_id, ts);
CREATE INDEX idx_obs_ts ON observations(ts); -- retention/export chronological scans
CREATE INDEX idx_obs_kind ON observations(kind);
CREATE INDEX idx_obs_hash_ts ON observations(hash, ts);

CREATE TABLE memories (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id    TEXT REFERENCES sessions(id) ON DELETE SET NULL,
    project       TEXT NOT NULL,
    tier          TEXT NOT NULL CHECK (tier IN ('working','episodic','semantic','procedural')),
    text          TEXT NOT NULL,
    created_at    INTEGER NOT NULL,
    accessed_at   INTEGER NOT NULL,
    access_cnt    INTEGER NOT NULL DEFAULT 0,
    importance    REAL NOT NULL DEFAULT 1.0,
    superseded_by INTEGER REFERENCES memories(id)
);
CREATE INDEX idx_mem_project ON memories(project);
CREATE INDEX idx_mem_tier ON memories(tier);
CREATE INDEX idx_mem_accessed ON memories(accessed_at);
CREATE INDEX idx_mem_importance ON memories(importance);

CREATE VIRTUAL TABLE memories_fts USING fts5(
    text,
    content='memories',
    content_rowid='id',
    tokenize='unicode61 remove_diacritics 2'
);

CREATE TRIGGER memories_ai AFTER INSERT ON memories BEGIN
    INSERT INTO memories_fts(rowid, text) VALUES (new.id, new.text);
END;
CREATE TRIGGER memories_ad AFTER DELETE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, text) VALUES('delete', old.id, old.text);
END;
CREATE TRIGGER memories_au AFTER UPDATE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, text) VALUES('delete', old.id, old.text);
    INSERT INTO memories_fts(rowid, text) VALUES (new.id, new.text);
END;

-- Эмбеддинги хранятся отдельной таблицей. Фаза 1 — таблица пустая.
-- Фаза 2 — pure Go вектор как BLOB. Фаза 3 — заменить на vec0 virtual table.
CREATE TABLE memory_embeddings (
    memory_id  INTEGER PRIMARY KEY REFERENCES memories(id) ON DELETE CASCADE,
    model      TEXT NOT NULL,
    dim        INTEGER NOT NULL,
    vec        BLOB NOT NULL                  -- float32 little-endian, length = dim*4
);
CREATE INDEX idx_embeddings_model_dim ON memory_embeddings(model, dim);

CREATE TABLE compaction_attempts (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id   TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    attempted_at INTEGER NOT NULL,
    status       TEXT NOT NULL CHECK (status IN ('requested','completed','skipped','failed')),
    reason       TEXT
);
CREATE INDEX idx_compact_attempts_session ON compaction_attempts(session_id, attempted_at);

INSERT INTO schema_version VALUES (1, unixepoch() * 1000);
```

## CLI

Внутри compose service `mcb` — единственный бинарь, entrypoint по умолчанию `mcb serve`. Все остальные подкоманды вызываются через `docker compose -f deploy/docker-compose.yml exec mcb mcb <cmd>`.

```
mcb serve                      # default entrypoint: HTTP + MCP сервер на :3411
mcb migrate                    # применить миграции (запускается автоматически перед serve)
mcb search <query> [--project P] [--limit N]   # debug, читает БД напрямую
mcb add <text> [--tier T] [--project P]
mcb sessions [--project P] [--limit N]
mcb compact [--session ID]     # печатает инструкцию для manual subagent dispatch
mcb decay                      # вручную запустить decay/eviction
mcb embed-missing [--project P] [--limit N]   # догнать embeddings для memories без вектора
mcb embed-rebuild [--project P] [--model M]   # пересчитать embeddings при смене model/dim
mcb backup --out -|PATH        # consistent SQLite backup; '-' stream to stdout, PATH overwrite через temp+rename
mcb export [--format json|jsonl] > /var/lib/mcb/export.jsonl
mcb doctor                     # health-check: БД, Ollama reachable, конфиг
mcb healthz                    # lightweight DB health-check для Docker HEALTHCHECK
mcb version
```

`mcb backup --out -` создаёт temp backup DB, стримит его в stdout и удаляет temp file. `mcb backup --out PATH` пишет во временный файл рядом с PATH и делает atomic rename; если PATH уже существует, он заменяется. Любая ошибка backup/copy/rename возвращает non-zero exit.

Парсинг — `os.Args` + `flag.NewFlagSet` per command. Без cobra.

Хук-эндпоинты — это HTTP, не CLI-подкоманды. Старый `mcb hook ...` интерфейс упразднён.

Примеры использования:

```bash
docker compose -f deploy/docker-compose.yml exec mcb mcb search "auth middleware" --project /home/user/proj
docker compose -f deploy/docker-compose.yml exec mcb mcb sessions --limit 20
docker compose -f deploy/docker-compose.yml exec mcb mcb doctor
docker compose -f deploy/docker-compose.yml exec mcb mcb export > backup.jsonl
```

## Интеграционная модель

Core mcb не должен знать детали конкретного агента. Все agent-specific форматы приводятся adapter'ами к общим операциям:

- `EnsureSession(agent, raw_session_id, cwd, ts)` — создаёт/обновляет session с normalized id `<agent>:<raw_session_id>`.
- `CaptureObservation(event)` — пишет `tool_use`, `tool_error`, `user_prompt`/`user_message` в `observations`.
- `BuildSessionContext(agent, session_id, cwd)` — возвращает markdown `<mcb-context>` для инжекта в агентский системный/стартовый контекст.
- `RequestCompaction(agent, session_id, cwd)` — проверяет `n_obs`, attempts, existing summary/memories и возвращает либо no-op, либо prompt для agent-native compactor subagent.

Claude Code adapter реализован через HTTP hooks, потому что Claude Code умеет вызывать shell commands на lifecycle events. OpenCode adapter реализован через plugin, потому что OpenCode даёт plugin hooks (`tool.execute.after`, `chat.message`, `experimental.chat.system.transform`, compaction hooks) и строгую config schema.

## Hook-интеграция с Claude Code

Hooks — это curl-команды, бьющие по HTTP API контейнера. Никакого Go-бинаря на хосте.

### `~/.claude/settings.json`

```json
{
  "hooks": {
    "SessionStart": [{
      "hooks": [{
        "type": "command",
        "command": "curl -fsS --max-time 5 -H 'Content-Type: application/json' --data-binary @- http://127.0.0.1:3411/hooks/session-start"
      }]
    }],
    "UserPromptSubmit": [{
      "hooks": [{
        "type": "command",
        "command": "curl -fsS --max-time 2 -H 'Content-Type: application/json' --data-binary @- http://127.0.0.1:3411/hooks/user-prompt"
      }]
    }],
    "PreToolUse": [{
      "hooks": [{
        "type": "command",
        "command": "curl -fsS --max-time 2 -H 'Content-Type: application/json' --data-binary @- http://127.0.0.1:3411/hooks/pre-tool"
      }]
    }],
    "PostToolUse": [{
      "hooks": [{
        "type": "command",
        "command": "curl -fsS --max-time 2 -H 'Content-Type: application/json' --data-binary @- http://127.0.0.1:3411/hooks/post-tool"
      }]
    }],
    "PostToolUseFailure": [{
      "hooks": [{
        "type": "command",
        "command": "curl -fsS --max-time 2 -H 'Content-Type: application/json' --data-binary @- http://127.0.0.1:3411/hooks/post-tool-failure"
      }]
    }],
    "Stop": [{
      "hooks": [{
        "type": "command",
        "command": "curl -fsS --max-time 5 -H 'Content-Type: application/json' --data-binary @- http://127.0.0.1:3411/hooks/stop"
      }]
    }],
    "PreCompact": [{
      "hooks": [{
        "type": "command",
        "command": "curl -fsS --max-time 5 -H 'Content-Type: application/json' --data-binary @- http://127.0.0.1:3411/hooks/pre-compact"
      }]
    }],
    "SubagentStart": [{
      "hooks": [{
        "type": "command",
        "command": "curl -fsS --max-time 2 -H 'Content-Type: application/json' --data-binary @- http://127.0.0.1:3411/hooks/subagent-start"
      }]
    }],
    "SubagentStop": [{
      "hooks": [{
        "type": "command",
        "command": "curl -fsS --max-time 2 -H 'Content-Type: application/json' --data-binary @- http://127.0.0.1:3411/hooks/subagent-stop"
      }]
    }],
    "Notification": [{
      "hooks": [{
        "type": "command",
        "command": "curl -fsS --max-time 2 -H 'Content-Type: application/json' --data-binary @- http://127.0.0.1:3411/hooks/notification"
      }]
    }],
    "TaskCompleted": [{
      "hooks": [{
        "type": "command",
        "command": "curl -fsS --max-time 2 -H 'Content-Type: application/json' --data-binary @- http://127.0.0.1:3411/hooks/task-completed"
      }]
    }],
    "SessionEnd": [{
      "hooks": [{
        "type": "command",
        "command": "curl -fsS --max-time 2 -H 'Content-Type: application/json' --data-binary @- http://127.0.0.1:3411/hooks/session-end"
      }]
    }]
  },
  "mcpServers": {
    "mcb": {
      "type": "http",
      "url": "http://127.0.0.1:3411/mcp"
    }
  }
}
```

Если включён bearer token — добавить `-H "Authorization: Bearer ${MCB_BEARER_TOKEN}"` в каждый curl, и в `mcpServers` блок:
```json
"mcb": {
  "type": "http",
  "url": "http://127.0.0.1:3411/mcp",
  "headers": {"Authorization": "Bearer ${MCB_BEARER_TOKEN}"}
}
```

### HTTP endpoints для hooks

| Endpoint | Метод | Body | Возврат |
|----------|-------|------|---------|
| `/hooks/session-start` | POST | hook JSON от Claude Code | `application/json` с `hookSpecificOutput.additionalContext` для инжекта контекста |
| `/hooks/user-prompt` | POST | hook JSON | `text/plain` (опционально доп. контекст) или пусто |
| `/hooks/pre-tool` | POST | hook JSON | пусто (204) |
| `/hooks/post-tool` | POST | hook JSON | пусто (204 No Content) |
| `/hooks/post-tool-failure` | POST | hook JSON | пусто (204) |
| `/hooks/pre-compact` | POST | hook JSON | `application/json` с `hookSpecificOutput.additionalContext` |
| `/hooks/subagent-start` | POST | hook JSON | пусто (204) |
| `/hooks/stop` | POST | hook JSON | либо пусто, либо `application/json` с `{"decision":"block","reason":"..."}` |
| `/hooks/subagent-stop` | POST | hook JSON | пусто (204) |
| `/hooks/notification` | POST | hook JSON | пусто (204) |
| `/hooks/task-completed` | POST | hook JSON | пусто (204) |
| `/hooks/session-end` | POST | hook JSON | пусто (204) |
| `/integrations/opencode/tool` | POST | normalized payload от OpenCode plugin `tool.execute.after` | пусто (204) |
| `/integrations/opencode/event` | POST | normalized lifecycle/message/part event от OpenCode plugin | пусто (204) |
| `/integrations/opencode/chat` | POST | normalized user/assistant message от OpenCode plugin | пусто (204) |
| `/integrations/opencode/context` | POST | `{ session_id, cwd }` от plugin system transform | `{ "additional_context": "..." }` или пустой context |
| `/integrations/opencode/enrich` | POST | `{ session_id, cwd, files }` от plugin system transform | `{ "additional_context": "...", "context": "..." }` |
| `/integrations/opencode/compact` | POST | `{ session_id, cwd, trigger }` от plugin compaction hook/command | `{ "should_compact": bool, "prompt": string }` |
| `/integrations/opencode/session-end` | POST | `{ session_id, cwd }` от plugin session delete event | пусто (204) |
| `/integrations/replay/session` | POST | `{ session_id, limit? }` | ordered `events` with stable IDs, actor/type/tool, payload preview, and redacted payload detail |
| `/healthz` | GET | — | `200 OK` если БД writable |
| `/readyz` | GET | — | `200 OK` если БД writable И (provider=none ИЛИ Ollama reachable) |
| `/mcp` | POST | MCP-протокол | Streamable HTTP MCP responses |

Curl с флагом `-fsS`:
- `-f` — fail silently на 4xx/5xx (exit non-zero), но мы хотим чтобы Claude Code воспринимал ответ нормально, поэтому если ошибка — exit non-zero, Claude Code залогирует и продолжит
- `-s` — silent (без progress)
- `-S` — но show errors при `-s`

`--max-time` важен: если контейнер упал и localhost:3411 не отвечает — connection refused моментально, но если контейнер висит — таймаут защищает Claude Code от зависания. Стопу даём 5с (валидный сценарий с подсчётом observations), остальным — 2с.

### Формат JSON от Claude Code (body POST)

Общие поля: `session_id`, `transcript_path`, `cwd`, `hook_event_name`.

- **SessionStart**: + `source` (`startup` | `resume` | `clear`)
- **UserPromptSubmit**: + `prompt` (string)
- **PostToolUse**: + `tool_name`, `tool_input` (object), `tool_response` (object)
- **Stop**: + `stop_hook_active` (bool)
- **SubagentStop**: + `stop_hook_active` (bool)

Все типы — в `internal/hooks/types.go`. Неизвестные поля игнорируются.

### Latency бюджет

`curl -> localhost:3411 -> handler -> SQLite write -> response` на macOS Docker Desktop обычно доминируется запуском нового `curl` процесса и Docker port forwarding. Целевой бюджет для PostToolUse — < 100ms p95 на тёплом контейнере. SessionStart делает SELECT с ranking — лимит < 200ms. Stop делает SELECT COUNT + UPDATE + опционально формирует JSON decision — лимит < 150ms.

### Поведение при недоступности контейнера

`curl: (7) Failed to connect to 127.0.0.1 port 3411` → exit 7 → Claude Code логирует, продолжает работу. Hook эффективно превращается в no-op. Это by design: память не критична для работы Claude Code, лучше пропустить запись чем заблокировать пользователя.

## Интеграция с OpenCode

OpenCode интегрируется через plugin, потому что MCP сам по себе даёт tools, но не даёт capture/context/compaction lifecycle. Plugin shipped в репозитории как `opencode/plugin/mcb.ts`; пользователь подключает его в project или global `opencode.json` и перезапускает OpenCode.

### `opencode.json`

`deploy/opencode.example.json`:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "plugin": ["./opencode/plugin/mcb.ts"],
  "mcp": {
    "mcb": {
      "type": "remote",
      "url": "http://127.0.0.1:3411/mcp",
      "enabled": true
    }
  }
}
```

Если включён bearer token, OpenCode MCP block получает `headers.Authorization`, а plugin читает `MCB_BEARER_TOKEN` из env и добавляет `Authorization: Bearer ...` к HTTP calls.

### Plugin responsibilities

`opencode/plugin/mcb.ts` использует documented OpenCode plugin hooks:

- `tool.execute.after`: отправляет normalized tool observation в `/integrations/opencode/tool`.
- `chat.message` или `event`: отправляет user messages в `/integrations/opencode/chat`; assistant messages сохраняются как compact metadata через `/integrations/opencode/event`, без полного generated text.
- `experimental.chat.system.transform`: запрашивает `/integrations/opencode/context` и добавляет `<mcb-context>` в system messages.
- `experimental.session.compacting` и `experimental.compaction.autocontinue`: вызывают `/integrations/opencode/compact`; если mcb возвращает prompt, plugin запускает/подталкивает `mcb-compactor` subagent flow перед обычной compaction/autocontinue.
- `command.execute.before`: опционально поддерживает manual command `/mcb-compact`, который вызывает тот же `/integrations/opencode/compact` path.

OpenCode не обязан иметь Claude-style `Stop` block decision. Для parity mcb использует ближайшие OpenCode lifecycle points: context transform для SessionStart-equivalent, tool/chat hooks для capture, compaction hooks + manual command для durable memory extraction. Если OpenCode добавит explicit session-stop event, adapter подключит его к тому же `RequestCompaction` path без изменения store/search/MCP.

### OpenCode compactor agent

`opencode/agent/mcb-compactor.md` — OpenCode agent definition с тем же prompt contract, что Claude Code compactor. Файл можно копировать в `.opencode/agent/mcb-compactor.md` проекта или в `~/.config/opencode/agent/mcb-compactor.md`:

```markdown
---
description: Extract persistent memories from the current OpenCode session observations.
mode: subagent
model: anthropic/claude-haiku-4-5
permission:
  edit: deny
  bash: deny
---

Read observations via mcp__mcb__memory_session_observations, deduplicate with mcp__mcb__memory_search, save one summary with mcp__mcb__memory_session_summary_save, then save 3-7 durable facts with mcp__mcb__memory_save. Always pass session_id to memory_save.
```

## MCP-сервер

Поднимается тем же `mcb serve` процессом. MCP смонтирован на `/mcp` как один HTTP POST endpoint со stdlib JSON-RPC routing.

### Tools

| Tool | Input | Output |
|------|-------|--------|
| `memory_recall` | `{ query: string, project?: string, limit?: int }` | список memory с text, tier, score, created_at |
| `memory_save` | `{ text: string, tier?: string, project?: string, importance?: float, session_id?: string }` | `{ id: int }` |
| `memory_search` | то же что recall, но без обновления `accessed_at` | то же |
| `memory_sessions` | `{ project?: string, limit?: int }` | список sessions с id, started_at, summary |
| `memory_session_observations` | `{ session_id: string, limit?: int }` | список observations с `cwd` и декодированным payload (`raw`/`zstd`) |
| `memory_session_summary_save` | `{ session_id: string, summary: string }` | `{ updated: bool }` |
| `memory_forget` | dry-run: `{ query: string, dry_run: true }`; delete: `{ ids: [int], confirm: true }` | dry-run возвращает кандидатов; delete возвращает `{ deleted: int }` |
| `memory_supersede` | `{ old_id: int, new_id: int }` | `{ updated: bool }` |
| `memory_profile` | `{ project: string }` | агрегаты: top концепты, files touched, частые tool names |
| `memory_update` | `{ id: int, text?: string, tier?: string, importance?: float }` | `{ updated: bool }` |
| `memory_timeline` | `{ project?: string, session_id?: string, limit?: int }` | chronological memories and observations |
| `memory_file_history` | `{ project?: string, files: string[], limit?: int }` | memories and observations related to file paths |
| `memory_patterns` | `{ project?: string, limit?: int }` | recurring tools, observation kinds, and files |
| `memory_export` | `{ project?: string, limit?: int }` | memories, sessions, and observations as JSON |
| `memory_audit` | `{ memory_id?: int, limit?: int }` | memory mutation audit events |
| `memory_verify` | `{ id: int }` | memory provenance with source observations and audit events |
| `memory_replay` | `{ session_id: string, limit?: int }` | ordered replay records with redacted payload details |

`memory_recall` обновляет `accessed_at = now()` и `access_cnt += 1` для возвращённых записей.

Tool descriptions — на английском, лаконичные, в формате ожидаемом MCP.

`memory_forget` никогда не удаляет напрямую по query. Query mode только возвращает список candidate IDs; удаление требует второго вызова с явным списком `ids` и `confirm: true`.

`memory_save` оставляет `session_id` опциональным для обычных ручных saves, но tool description должна явно говорить compactor'у: when saving facts extracted from session observations, always pass `session_id`.

Tools по фазам:

- **Фаза 3**: `memory_recall`, `memory_save`, `memory_search`, `memory_sessions`, `memory_session_observations`, `memory_forget`, `memory_profile`.
- **Фаза 4**: `memory_session_summary_save`, `memory_supersede`.
- **Parity Phase 2**: practical tools `memory_update`, `memory_timeline`, `memory_file_history`, `memory_patterns`, `memory_export`, `memory_audit`, `memory_verify`.
- **Parity Phase 3**: `mcb import-jsonl`, `/integrations/replay/session`, `memory_replay`.

### Resources

- `mcb://status` — JSON: counts, last write, Ollama reachable
- `mcb://project/{project}/profile` — то же что `memory_profile`
- `mcb://memories/latest` — latest active memories
- `mcb://sessions/latest` — latest sessions
- `mcb://audit/latest` — latest memory mutation audit events

### Prompts

- `recall_context` — prompt template for task-focused memory recall.
- `session_handoff` — prompt template for handoff summary creation.

## Pipeline: запись наблюдений

Перед любой записью observation handler обязан гарантировать существование session. Agent-specific adapters сначала нормализуют raw session id в `session_id = "<agent>:<raw_session_id>"`, чтобы Claude Code и OpenCode не могли столкнуться по ID. `SessionStart`/OpenCode session event может не прийти или упасть по timeout'у, поэтому `sessions` создаётся лениво и идемпотентно через `EnsureSession(agent, raw_session_id, cwd, ts)` внутри той же write transaction, где пишется observation.

```sql
INSERT INTO sessions (id, agent, external_id, project, started_at, n_obs)
VALUES (?, ?, ?, ?, ?, 0)
ON CONFLICT(id) DO UPDATE SET
    project = CASE
        WHEN sessions.project = '' OR sessions.project = 'unknown'
        THEN excluded.project
        ELSE sessions.project
    END,
    started_at = CASE
        WHEN excluded.started_at < sessions.started_at
        THEN excluded.started_at
        ELSE sessions.started_at
    END;
```

`project` не перетирается на каждом hook'е: session должна оставаться привязанной к исходному project. Исключение — placeholder `unknown`, если session была создана fallback-путём без валидного `cwd`. При этом конкретный `cwd` каждого hook event сохраняется в `observations.cwd`, чтобы сессия, гуляющая по нескольким директориям, не теряла per-event project context.

`PostToolUse` flow:

1. Прочитать stdin JSON, распарсить.
2. Открыть write transaction через `BEGIN IMMEDIATE`.
3. Adapter нормализует payload в общий event shape: `agent`, `raw_session_id`, normalized `session_id`, `cwd`, `kind`, `tool`, `payload`.
4. `EnsureSession(agent, raw_session_id, cwd, ts)`.
5. Канонизировать `tool_input` + `tool_response` (отсортировать ключи, нормализовать пробелы).
6. Прогнать через secret filter (см. ниже).
7. `hash := sha256(canonical_json)`.
8. Проверить в `observations` существование hash за окно `dedup_window_seconds`. Если есть — commit без insert и exit 0.
9. Если raw payload < 512 байт — сохранить как `payload_encoding='raw'`; иначе zstd-сжать payload и сохранить как `payload_encoding='zstd'`.
10. Если `len(stored_payload) > max_observation_bytes` — обрезать `tool_response` до 32KB и повторить encode.
11. `INSERT INTO observations` с `cwd`, `payload_encoding`, `schema_version=1`, `hash`.
12. Если insert реально произошёл — инкрементить `sessions.n_obs`.
13. Commit transaction. exit 0. Целевое время < 100ms p95 на macOS Docker Desktop.

`UserPromptSubmit` flow: то же что PostToolUse, но `kind = 'user_prompt'`, `tool = NULL`, в payload только `{ prompt }`.

## Pipeline: компрессия сессии (agent-native subagent dispatch)

Mcb не выполняет LLM-вызовы. Вместо этого adapter просит текущего агента запустить `mcb-compactor` subagent. Subagent ходит обратно в mcb через MCP и сохраняет summary/facts.

- **Claude Code**: Stop hook возвращает `decision: block` с инструкцией вызвать Haiku-subagent через Task tool.
- **OpenCode**: plugin вызывает `/integrations/opencode/compact` на compaction/autocontinue/manual command lifecycle и запускает или инжектит prompt для `mcb-compactor` subagent. Если в текущей версии OpenCode нет explicit session-stop event, compaction trigger считается SessionEnd-equivalent для durable memory extraction.

### Claude Code subagent definition

Файл `claude/agents/mcb-compactor.md` в репозитории mcb. Пользователь копирует в `~/.claude/agents/mcb-compactor.md`:

```markdown
---
name: mcb-compactor
description: Extract persistent memories from the current session's observations. Invoke at session end when the mcb Stop hook returns a block decision mentioning this agent.
model: claude-haiku-4-5
tools:
  - mcp__mcb__memory_session_observations
  - mcp__mcb__memory_search
  - mcp__mcb__memory_save
  - mcp__mcb__memory_session_summary_save
---

You are a memory-compression subagent for the mcb memory bank.

You receive a task description containing `session_id`, `project`, and optionally `cwds` (distinct working directories seen in observations). Your job:

1. Call `mcp__mcb__memory_session_observations` with that session_id (limit 100) to fetch the raw observations.
2. Write one concise session summary, 1-3 sentences and <= 800 characters, focused on what changed, decisions made, and unresolved follow-ups. Do not include secrets, temporary paths, raw IDs, or long command output.
3. Save that summary with `mcp__mcb__memory_session_summary_save` using the given session_id.
4. Call `mcp__mcb__memory_search` with 2-3 short queries derived from observation content, scoped to the same project, to surface existing memories. Skip facts already covered. Do not use `memory_recall`: compaction should not refresh `accessed_at` for old memories.
5. Synthesize 3-7 durable facts from what is left.
6. For each fact, call `mcp__mcb__memory_save` with:
    - `text`: a self-contained sentence with no pronouns referring to outside context.
    - `tier`: one of `semantic` (stable codebase facts, decisions), `procedural` (how-to, commands), `episodic` (what happened this session), `working` (current task state, rarely useful).
    - `project`: the relevant project path. Use observation `cwd` when the fact clearly belongs to a specific cwd; otherwise use the project path from the task description.
    - `session_id`: always pass the session_id from the task description. This is mandatory for compaction idempotency.
    - `importance`: 0.1 (trivial) to 1.0 (critical). Default 0.5.

Examples of good facts:
- "Auth middleware uses the jose library in src/middleware/auth.ts; chosen over jsonwebtoken for Edge runtime compatibility."
- "Tests for the auth module run via `pnpm test --filter=auth`."
- "User prefers tabs over spaces in this repo (verified from .editorconfig)."

Do NOT save:
- Secrets, tokens, paths under /tmp, ephemeral session IDs.
- Restatements of obvious tool output.
- Speculation not supported by observations.

After saving the summary and all facts, respond with one line: `Saved summary and N memories for session {session_id}.`
```

Tool names в `tools:` отражают то, как MCP-сервер `mcb` экспортирует свои tools (`mcp__<server_name>__<tool_name>`).

### Session summary

`sessions.summary` заполняется тем же `mcb-compactor` subagent'ом, который извлекает durable memories. Summary — это не searchable memory и не заменяет facts; это короткий human-readable recap последней сессии для следующего context inject.

Правила summary:

- 1-3 предложения, максимум 800 символов.
- Содержит только итог работы: что изменили, какие решения приняли, какие follow-up'ы остались.
- Не содержит секретов, временных путей, raw session IDs, больших stack traces или полного вывода команд.
- Перезаписывается идемпотентно для того же `session_id`: `UPDATE sessions SET summary = ?, ended_at = COALESCE(ended_at, now()) WHERE id = ?`.
- После успешной записи summary handler помечает последний свежий `compaction_attempts(status='requested')` для этого `session_id` как `completed`.

MCP tool `memory_session_summary_save` доступен только как write helper для compactor'а. Обычный recall/search его не использует; context inject читает последние summaries из `sessions` отдельно от memories.

### Claude Code Stop hook поведение

`POST /hooks/stop` handler:

1. Распарсить body, получить `session_id`, `cwd`, `stop_hook_active`.
2. `EnsureSession(session_id, cwd, now)` — Stop должен быть устойчив к пропущенному `SessionStart`.
3. Если `stop_hook_active == true` → UPDATE `ended_at`, respond `204`. Защита от петель.
4. Если `[compaction].mode == "disabled"` → UPDATE `ended_at`, respond `204`.
5. SELECT `n_obs` FROM sessions WHERE id = ?.
6. Если `n_obs < min_observations` → UPDATE `ended_at`, respond `204`.
7. Проверить «уже скомпактили»: session имеет `summary IS NOT NULL` или `EXISTS (SELECT 1 FROM memories WHERE session_id = ? AND created_at >= sessions.started_at)`. Если true → UPDATE `ended_at`, respond `204` (повторный Stop в той же сессии).
8. Lazy-cleanup старых attempts: `UPDATE compaction_attempts SET status='failed', reason='ttl expired' WHERE session_id=? AND status='requested' AND attempted_at < now - attempt_ttl_seconds`.
9. Посчитать свежие block-attempts: `SELECT COUNT(*) FROM compaction_attempts WHERE session_id = ? AND status = 'requested' AND attempted_at >= now - attempt_ttl_seconds`.
10. Если attempts >= `max_block_attempts` → INSERT `compaction_attempts(status='skipped', reason='max attempts')`, UPDATE `ended_at`, log warning, respond `204`. Это hard stop против бесконечного Stop → block → Stop loop, если агент не запустил subagent.
11. UPDATE `sessions.ended_at = now()`.
12. SELECT distinct `cwd` из `observations` для session, ordered by first seen, limit 10; включить этот список в prompt как `cwds=[...]`.
13. Если `mode == "manual"` → respond `204` (пользователь сам запустит `docker compose -f deploy/docker-compose.yml exec mcb mcb compact`).
14. Если `mode == "subagent"` → INSERT `compaction_attempts(status='requested')`, respond `200` с `Content-Type: application/json` body:

   ```json
   {
     "decision": "block",
     "reason": "Before ending, dispatch the mcb-compactor subagent via the Task tool with subagent_type=\"mcb-compactor\" and this exact prompt: \"session_id=<UUID> project=<CWD> cwds=<JSON_ARRAY_OF_DISTINCT_CWDS> — read observations via mcp__mcb__memory_session_observations, deduplicate against existing memories via mcp__mcb__memory_search, save one summary via mcp__mcb__memory_session_summary_save, then save 3-7 facts via mcp__mcb__memory_save with session_id set.\" Do not ask the user; dispatch immediately. After the subagent reports completion, stop."
   }
   ```

Curl передаст этот JSON в stdout. Claude Code прочитает `decision: block`, обработает `reason`. Main agent вызывает Task tool. Subagent работает в своём контексте, не съедая токены main session.

### SubagentStop hook

`POST /hooks/subagent-stop` handler:

- Распарсить body, залогировать в stdout (`docker compose -f deploy/docker-compose.yml logs mcb`): `{"event":"subagent_stop","session_id":"...","ts":...}`.
- respond `204`.

`SubagentStop` не используется как success marker: он срабатывает на любой subagent, не только `mcb-compactor`. Completion фиксируется write-side: `memory_session_summary_save(session_id=...)` и `memory_save(session_id=...)` помечают последний свежий `requested` attempt как `completed`.

### Надёжность

Подход полагается на то, что main agent корректно выполнит инструкцию из `reason`. Митигации:

- **Чёткий императив в reason**: «dispatch immediately», «do not ask the user», явное имя subagent_type, явные имена tools.
- **Идемпотентность**: повторный Stop с тем же session_id ничего не сделает после появления memories/summary; до этого hard limit по свежим `compaction_attempts` не даст бесконечно блокировать завершение, а TTL позволит повторить compaction позже после сбоя.
- **Context inject подсказка**: в фазе 4 добавить в `<mcb-context>` агент-специфичную строку про compactor dispatch. Для Claude Code: «If a Stop hook returns a block decision referencing mcb-compactor, dispatch it without asking confirmation.» Для OpenCode: «If mcb plugin requests mcb-compactor, run that subagent without asking confirmation.»
- **Fallback `manual`**: если в проекте Stop-блокировка нежелательна (другие hooks ломаются), переключить mode на manual и запускать `mcb compact --session <id>` руками.

### CLI fallback: `mcb compact`

`mcb compact --session <id> [--agent claude-code|opencode]` печатает в stdout готовую agent-specific инструкцию для пользователя:

```
To compact session <id>, in Claude Code run:

  /agents mcb-compactor "session_id=<id> project=<path> — read observations..."

Or invoke the Task tool with subagent_type=mcb-compactor and the prompt above.

For OpenCode, run the mcb-compactor subagent with the same prompt, or trigger the plugin manual command `/mcb-compact` if configured.
```

`mcb compact` без аргументов — список сессий за последние 7 дней, которые подлежат compact, но ещё не имеют summary/memories: `n_obs >= min_observations`, `ended_at IS NOT NULL`, `summary IS NULL`, нет `memories.session_id = sessions.id`, и свежие requested attempts за `attempt_ttl_seconds` не превышают `max_block_attempts`. Сессии с малым `n_obs` в список не попадают.

### Конфликт со встроенным `/compact` Claude Code

У Claude Code есть встроенная команда `/compact` для компрессии контекста. Это разные вещи: `/compact` ужимает текущий контекст модели, `mcb compact` извлекает persistent memories. Документировать в README, чтобы не путать.

## Pipeline: context inject

Claude Code вызывает `/hooks/session-start`; OpenCode plugin вызывает `/integrations/opencode/context` из `experimental.chat.system.transform`. Оба path используют один `BuildSessionContext`.

1. Прочитать adapter payload, получить `agent`, raw `session_id`, `cwd` (=project).
2. `EnsureSession(session_id, cwd, now)` — если observations уже создали session fallback-путём, SessionStart только уточняет metadata.
3. SELECT memories WHERE project = ? AND superseded_by IS NULL ORDER BY (importance * recency_decay) DESC LIMIT `session_start_top_n`.
   - `recency_decay = exp(-(now - accessed_at) / (tau_days * 86400000))`
4. SELECT последние 3 sessions для project с `summary IS NOT NULL` (summary заполняет `mcb-compactor` в фазе 4).
5. Сформировать markdown для `additionalContext`:

```
<mcb-context>
## Recent project memories
- [semantic] {text}
- [procedural] {text}
...

## Recent session summaries
- {date}: {summary}
- {date}: {summary}
</mcb-context>
```

6. Для Claude Code, если context непустой — вернуть `200 application/json`:

```json
{
  "hookSpecificOutput": {
    "hookEventName": "SessionStart",
    "additionalContext": "<mcb-context>...markdown...</mcb-context>"
  }
}
```

7. Для OpenCode вернуть `{ "additional_context": "..." }`; plugin добавляет текст в system messages. Если context пустой — вернуть пустой string.
8. Если context пустой для Claude Code — вернуть `204`. exit 0.

Plain text stdout для SessionStart не использовать: в актуальном Claude Code это может попасть в transcript, но не гарантирует inject в системный контекст.

## Поиск (hybrid BM25 + Vector + RRF)

Сигнатура:

```go
func (s *Searcher) Hybrid(ctx context.Context, q Query) ([]Result, error)

type Query struct {
    Text    string
    Project string  // опционально, фильтр
    Limit   int
}

type Result struct {
    Memory Memory
    Score  float64
    BM25Rank, VecRank int  // 0 если не вернул
}
```

Алгоритм:

1. Параллельно (goroutine + errgroup):
   - **BM25**: `SELECT m.* FROM memories_fts f JOIN memories m ON m.id = f.rowid WHERE memories_fts MATCH ? AND m.project = ? AND m.superseded_by IS NULL ORDER BY rank LIMIT bm25_top_k`. Query builder не пропускает raw FTS5 syntax по умолчанию: разбить пользовательский query на lexical tokens, каждый token обернуть в quotes с удвоением внутренних `"`, затем join через пробел. Это защищает от `-`, `:`, `AND`, `OR`, `NOT`, `NEAR`, `^` и прочего FTS5 syntax. Advanced raw FTS mode — только отдельным debug-флагом, не для MCP tools.
   - **Vector**: если есть provider — `embed(q.Text)`, затем `SELECT memory_id, vec FROM memory_embeddings me JOIN memories m ON m.id = me.memory_id WHERE m.project = ? AND m.superseded_by IS NULL AND me.model = ? AND me.dim = ?`. Cosine в Go (фаза 2) или MATCH через sqlite-vec (фаза 3). Top vector_top_k. Embeddings другой модели/dim игнорируются до `mcb embed-rebuild`.
2. RRF: для каждой записи `score = sum over streams of 1/(rrf_k + rank_in_stream)`. `rank_in_stream` 1-индексирован. Если запись не в стриме — её вклад 0.
3. **Session diversification**: пройти по отсортированному списку, ограничить max 3 результата на один `session_id`. Защита от доминирования одной длинной сессии в выдаче.
4. Отсортировать по score desc, обрезать до `final_top_k`.
5. UPDATE `accessed_at`, `access_cnt` для возвращённых.

Если эмбеддинг-провайдер `none` или Ollama unreachable — возвращать только BM25.

### Языковая релевантность BM25

FTS5 tokenizer `unicode61` делает Unicode tokenization и `remove_diacritics`, но не делает настоящую морфологию или stemming для русского/английского. Поэтому phase 1 BM25 нельзя считать morphological search.

Варианты решения:

- **A. Default для фазы 1: lexical BM25 only.** Оставить `unicode61`, явно тестировать точные токены, имена файлов, команды, ошибки, идентификаторы. Русский и английский поддерживаются как Unicode-текст, но без склонений/lemmatization. Самый простой и надёжный baseline.
- **B. English stemming через FTS5 porter.** Завести отдельную FTS table или tokenizer config с `porter unicode61` для английского. Улучшает `run/running`, `test/tests`, но не решает русский и может портить code identifiers. Не включать по умолчанию.
- **C. Precomputed normalized search text.** При сохранении memory писать в FTS не только original text, но и `search_text`: lower-case, aliases, лёгкий stemming/lemmatization через Go-библиотеку. Может покрыть русский, но добавляет зависимость, качество зависит от языка, и нужно тестировать false positives.
- **D. Rely on vector search с фазы 2.** Для синонимов, перефразировок и морфологии основной механизм — embeddings + RRF. BM25 остаётся точным keyword stream'ом. Это предпочтительный путь для дефолтной архитектуры.

Решение по умолчанию: фаза 1 принимает lexical BM25 без обещания морфологии; фаза 2 закрывает semantic/morphological recall через vector search. Если до фазы 2 потребуется лучшее качество на русском, выбрать вариант C как отдельную задачу.

### Скорость

Vector-поиск — линейный скан cosine с project pre-filter в SQL WHERE. Cosine считается только по embeddings одного проекта.

| n per-project | Go cosine brute force (фаза 2) | sqlite-vec SIMD (фаза 3) |
|---|---|---|
| 1k  | ~0.3ms  | ~0.05ms |
| 10k | ~3ms    | ~0.3ms  |
| 50k | ~15ms   | ~1.5ms  |
| 100k| ~30ms   | ~3ms    |

На реалистичном scale (5-20k memories per project) latency vector lookup'a — единицы ms. В общем бюджете доминируют Ollama embed call (~20-50ms) и опциональный reranker (~30-100ms). Vector search не bottleneck.

ANN-структуры (HNSW, IVF, ball/kd tree) **не входят в план**:

- **Ball/KD tree**: вырождаются до brute force на 768-dim из-за curse of dimensionality (concentration of measure, Pestov arXiv:cs/9901004). Эмпирически работают только до ~50-100 dim.
- **HNSW**: алгоритмически даёт log(n) и решает проблему scale за счёт обхода ~M×EfSearch ≈ 150 нод вместо n. Не требует SIMD (трогает мало нод). Mature Go-либа — `github.com/coder/hnsw`. Но: на нашем scale дал бы экономию единиц ms ценой graph maintenance, lazy per-project build, delete reconnection, approximate recall ~95-99% вместо 100%. Не оправдано пока per-project n < 50k.

Если когда-нибудь профайлинг покажет vector lookup как реальный bottleneck — вернуться к HNSW. До этого — мёртвый код.

Оптимизации для brute force, которые имеют смысл:
- `PRAGMA mmap_size = 268435456` — OS page cache держит embedding-таблицу в памяти.
- `float32 LE BLOB` storage, `unsafe.Slice` для zero-copy чтения в `[]float32`.
- Параллельный pass по чанкам через `errgroup` на > 20k vectors per project (горизонтальная утилизация ядер).

## Эмбеддинги через Ollama

`internal/embed/ollama.go`:

```go
type Client struct {
    URL     string
    Model   string
    Dim     int // 0 = auto-detect; >0 = validate exact len
    Timeout time.Duration
    HTTP    *http.Client
}

func (c *Client) Embed(ctx context.Context, text string) ([]float32, error)
func (c *Client) Healthy(ctx context.Context) bool
```

Запрос: `POST {url}/api/embeddings` body `{"model": c.Model, "prompt": text}`. Ответ: `{"embedding": [...]}`. Если `dimensions > 0` — проверять `len == dimensions`; если `dimensions == 0` — принять dim из первого успешного embed call и дальше валидировать в рамках процесса. В БД всегда хранить фактические `model` и `dim`.

Vector search фильтрует embeddings по текущим `model` и `dim`. Если пользователь сменил model или dim, старые vectors не участвуют в поиске, пока не выполнен `mcb embed-rebuild`. Для русскоязычных memories можно заменить default `nomic-embed-text` на multilingual model (`bge-m3`, `multilingual-e5-*`), потому что dim auto-detect и model/dim filter уже поддерживают смену модели.

Circuit breaker: после `circuit_breaker_failures` подряд ошибок/таймаутов Ollama перевести search в BM25-only на `circuit_breaker_cooldown_ms`, чтобы recall не висел на каждом запросе по 30s.

Хранение: `float32` little-endian как BLOB. `binary.Write(buf, binary.LittleEndian, vec)`.

## Фильтрация секретов

`internal/secrets/patterns.go` — список regex, применяется к payload как к строке *после* JSON-маршалинга. Замена match → `[REDACTED]`.

Минимальный набор паттернов:

```
sk-[a-zA-Z0-9]{20,}                       # OpenAI/Anthropic-style
sk-ant-[a-zA-Z0-9-]{40,}
AKIA[0-9A-Z]{16}                          # AWS access key
gh[pousr]_[A-Za-z0-9]{36,}                # GitHub tokens
xox[bp]-[0-9]+-[0-9]+-[0-9]+-[a-f0-9]+    # Slack
-----BEGIN [A-Z ]+PRIVATE KEY-----[\s\S]+?-----END [A-Z ]+PRIVATE KEY-----
eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}   # JWT
[a-zA-Z0-9+/]{40,}={0,2}                  # NB: false positives — НЕ включать в default
```

Полный список и тесты — в `internal/secrets/patterns_test.go`. Включается флагом `[security].secret_redaction`.

## Дедупликация

Канонизация для hash:

1. `json.Unmarshal` payload → `map[string]any`.
2. Рекурсивно сортировать map keys, нормализовать.
3. `json.Marshal` обратно. Для `map[string]...` Go `encoding/json` уже сортирует keys детерминированно; ручная нормализация всё ещё нужна для чисел, `map[string]any`, массивов и стабильного отброса/нормализации нерелевантных полей.
4. `sha256.Sum256(canonical)` → hex.

`hash` не UNIQUE: дедуп должен быть windowed, а не глобальный навсегда. Алгоритм выполняется внутри `BEGIN IMMEDIATE` write transaction:

```sql
SELECT 1 FROM observations
WHERE hash = ? AND ts > ?
LIMIT 1;
```

Если запись есть в окне `dedup_window_seconds` — не вставлять observation. Если совпадение старше окна — вставить новую строку, чтобы повторяющиеся события оставались видны в аудите. `BEGIN IMMEDIATE` нужен, чтобы два параллельных одинаковых hook'а не прошли check одновременно.

## Memory tiers и decay

Фаза 1: пишем всё в `semantic`, decay выключен.

Фаза 4:

- **Decay**: `mcb serve` запускает goroutine с `time.NewTicker(decay_interval_hours)` и выполняет decay внутри процесса. `decay_interval_hours = 0` отключает ticker. Отдельная CLI-команда `mcb decay` делает тот же проход вручную. Decay делает `UPDATE memories SET importance = importance * exp(-(now - accessed_at)/(tau_days*86400000))`. После — `DELETE FROM memories WHERE importance < min_importance AND tier != 'procedural'`.
- **Supersession**: при insert нового semantic-факта искать через BM25 топ-1, если cosine > 0.9 — пометить старый `superseded_by = new.id` вместо delete (для аудита).

## Зависимости

```
require (
    github.com/mattn/go-sqlite3 v1.x         // CGO; FTS5 enabled через build tag
    github.com/klauspost/compress v1.x
    github.com/pelletier/go-toml/v2 v2.x     // config parsing
)
```

Build tag для FTS5: `go build -tags "sqlite_fts5"` (mattn включает FTS5 опционально). В Dockerfile прописать.

Фаза 3+: `sqlite-vec` extension собирается отдельной build-stage из `github.com/asg017/sqlite-vec` (C-исходники), `.so` копируется в финальный образ. Mcb на старте делает `LoadExtension("sqlite_vec")`.

Транзитивные — лимит < 30 модулей. Проверка: `go mod graph | wc -l`.

## Docker deployment

### Dockerfile

`deploy/Dockerfile` — multi-stage.

```dockerfile
# ---- build stage ----
FROM golang:1.26-alpine AS build

RUN apk add --no-cache build-base git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO_ENABLED=1 для mattn/go-sqlite3; FTS5 включается build tag
RUN CGO_ENABLED=1 GOOS=linux \
    go build -tags "sqlite_fts5" -ldflags="-s -w -X main.version=$(git describe --tags --always 2>/dev/null || echo dev)" \
    -o /out/mcb ./cmd/mcb

# ---- runtime stage ----
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata sqlite-libs \
    && addgroup -S -g 10001 mcb && adduser -S -D -u 10001 -G mcb -h /var/lib/mcb mcb \
    && mkdir -p /var/lib/mcb /etc/mcb \
    && chown -R mcb:mcb /var/lib/mcb /etc/mcb

COPY --from=build /out/mcb /usr/local/bin/mcb
COPY deploy/config.example.toml /etc/mcb/config.toml

USER mcb
WORKDIR /var/lib/mcb

EXPOSE 3411

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD mcb healthz || exit 1

ENTRYPOINT ["mcb"]
CMD ["serve"]
```

Финальный образ ~25-30MB (alpine base + sqlite-libs + Go binary).

Если хочется ещё легче — `gcr.io/distroless/cc-debian12` runtime, образ ~20MB. Shell не нужен: Docker `HEALTHCHECK` вызывает сам бинарь (`mcb healthz`), а `docker compose -f deploy/docker-compose.yml exec mcb mcb search ...` работает напрямую. Дебажить distroless сложнее.

### Build target

Фазы 1-4 таргетят macOS Apple Silicon. Обычный `docker compose -f deploy/docker-compose.yml up -d --build` собирает linux/arm64 image внутри Docker Desktop. `mattn/go-sqlite3` требует CGO, но cross-compile/multi-arch release не входит в обязательный scope.

### docker-compose.yml

`deploy/docker-compose.yml`:

```yaml
services:
  mcb:
    image: mcb:latest
    container_name: mcb
    build:
      context: ..
      dockerfile: deploy/Dockerfile
    restart: unless-stopped
    ports:
      - "127.0.0.1:3411:3411"      # ТОЛЬКО loopback; не открывать наружу
    volumes:
      - mcb-data:/var/lib/mcb
      # - ./config.toml:/etc/mcb/config.toml:ro   # опционально, если файл создан пользователем
    environment:
      MCB_EMBEDDING_PROVIDER: "none"            # base compose не требует Ollama
      MCB_SERVER_HTTP_BIND: "0.0.0.0:3411"
      # MCB_BEARER_TOKEN: ${MCB_BEARER_TOKEN}  # опционально, если включаешь auth
      TZ: "Europe/Moscow"
    healthcheck:
      test: ["CMD", "mcb", "healthz"]
      interval: 30s
      timeout: 3s
      retries: 3
      start_period: 5s
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"

volumes:
  mcb-data:
    name: mcb-data
```

Базовый compose не включает embeddings. Это важно: phase 1 и машины без Ollama должны стартовать одинаково.

### docker-compose.ollama.yml

Если хочется Ollama соседним контейнером, включается overlay `deploy/docker-compose.ollama.yml`:

```yaml
services:
  mcb:
    environment:
      MCB_EMBEDDING_PROVIDER: "ollama"
      MCB_EMBEDDING_OLLAMA_URL: "http://ollama:11434"
    depends_on:
      - ollama

  ollama:
    image: ollama/ollama:latest
    container_name: mcb-ollama
    restart: unless-stopped
    volumes:
      - ollama-data:/root/.ollama
    # ports не нужны — связь через docker network
    # gpu support если есть:
    # deploy:
    #   resources:
    #     reservations:
    #       devices:
    #         - driver: nvidia
    #           count: all
    #           capabilities: [gpu]

volumes:
  ollama-data:
```

Запуск:

```bash
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.ollama.yml up -d --build
docker exec mcb-ollama ollama pull nomic-embed-text
```

В этом режиме `mcb` ходит в Ollama по Docker DNS имени `http://ollama:11434`; `host.docker.internal` и `extra_hosts` не нужны.

Ollama на macOS host не является штатным deployment path. Если это понадобится для локального эксперимента, пользователь может сделать собственный override на `host.docker.internal`, но документация и compose-файлы поддерживают sidecar-first схему.

### Установка пользователем

```bash
git clone <repo> && cd mcb
docker compose -f deploy/docker-compose.yml up -d --build

# Проверить
curl -fsS http://127.0.0.1:3411/healthz
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.ollama.yml exec mcb mcb doctor
```

### Установка Claude Code integration

```bash
mkdir -p ~/.claude/agents
cp claude/agents/mcb-compactor.md ~/.claude/agents/

# Один раз: смерджить hooks + mcpServers в ~/.claude/settings.json
# (см. deploy/claude-settings.example.json)
```

### Установка OpenCode integration

Project-local вариант:

```bash
mkdir -p .opencode/plugin .opencode/agent
cp opencode/plugin/mcb.ts .opencode/plugin/
cp opencode/agent/mcb-compactor.md .opencode/agent/

# Один раз: смерджить plugin + mcp config в ./opencode.json или .opencode/opencode.json
# (см. deploy/opencode.example.json)
```

Global вариант:

```bash
mkdir -p ~/.config/opencode/plugins ~/.config/opencode/agent
cp opencode/plugin/mcb.ts ~/.config/opencode/plugins/
cp opencode/agent/mcb-compactor.md ~/.config/opencode/agent/

# Один раз: смерджить plugin + mcp config в ~/.config/opencode/opencode.json
# После изменения opencode config/plugin/agent нужно полностью перезапустить OpenCode.
```

### Backup / restore

```bash
# Backup
docker compose -f deploy/docker-compose.yml exec mcb mcb backup --out - > ./mcb-$(date +%Y%m%d).db

# Restore
docker compose -f deploy/docker-compose.yml down
docker run --rm -v mcb-data:/data -v "$PWD":/backup alpine \
  sh -c "cp /backup/mcb-YYYYMMDD.db /data/memory.db && rm -f /data/memory.db-wal /data/memory.db-shm && chown 10001:10001 /data/memory.db"
docker compose -f deploy/docker-compose.yml up -d
```

Не делать `tar` backup живого SQLite volume: WAL/SHM могут дать inconsistent snapshot. `mcb backup` делает consistent SQLite backup через `VACUUM INTO` или backup API. При `--out -` команда пишет backup DB в stdout через временный файл и удаляет его; при `--out PATH` пишет во временный файл рядом с PATH и делает atomic rename, поэтому существующий PATH можно безопасно overwrite. Logical backup остаётся доступен через `docker compose -f deploy/docker-compose.yml exec mcb mcb export > backup.jsonl`.

### Обновление

```bash
git pull
docker compose -f deploy/docker-compose.yml up -d --build
```

Миграции БД применяются автоматически в `mcb serve` перед стартом HTTP.

### Безопасность

- Биндинг `127.0.0.1:3411` на хосте — единственный экспонированный порт. С внешней сети контейнер недоступен.
- Внутри контейнера `0.0.0.0:3411` — нужно для Docker port forwarding. Это не дыра пока `ports:` остаётся `127.0.0.1:...`.
- Bearer token (`MCB_BEARER_TOKEN`) опционален; включать если на машине несколько пользователей. Токен читается при старте контейнера, ротация требует обновить Claude settings и перезапустить `mcb`; для single-user локального setup это приемлемо.
- Контейнер запускается под non-root `mcb` UID/GID `10001:10001`. Volume владеется этим UID.
- БД лежит в named volume `mcb-data`, не в bind mount на macOS host. Не bind-mount'ить `/var/lib/mcb` в host directory: SQLite WAL поверх Docker Desktop file sharing может быть медленным и менее надёжным.
- Если Ollama запущена на macOS host, контейнер ходит только на `host.docker.internal:11434`. Если Ollama в compose overlay, связь идёт по Docker network имени `ollama`.

## План реализации по фазам

### Фаза 1 — Capture + BM25 + Docker baseline (выходные)

Цель: контейнер поднимается, Claude Code hooks и OpenCode plugin пишут observations, `mcb add` пишет memories, `mcb search` находит по BM25.

Объём:
- `cmd/mcb/main.go` — подкоманды `serve`, `migrate`, `add`, `search`, `sessions`, `backup`, `healthz`, `version`, `doctor`
- `internal/config` — TOML load + env override
- `internal/store` — open, миграция 001, CRUD observations/memories/sessions
- SQLite connection policy: `writeDB.SetMaxOpenConns(1)`, read pool отдельно, `_foreign_keys=1`, `_busy_timeout=5000`
- `internal/server` — HTTP server, middleware, routing
- `internal/hooks` + `internal/integrations` — Claude Code hook endpoints и OpenCode plugin endpoints; compaction endpoints пока no-op/update ended_at (без decision/subagent orchestration)
- `internal/secrets` — базовые паттерны + тесты
- `internal/dedup` — canonical json + sha256
- BM25 поиск через FTS5
- `/hooks/session-start` и `/integrations/opencode/context` возвращают последние N memories по recency (без decay) в agent-specific response format
- `deploy/Dockerfile` + `deploy/docker-compose.yml`
- `deploy/claude-settings.example.json` + `deploy/opencode.example.json`
- `opencode/plugin/mcb.ts` basic capture/context plugin + `opencode/agent/mcb-compactor.md` skeleton
- README: install, troubleshooting (`curl: (7)` → `docker compose ps`/`docker compose logs mcb`, `SQLITE_BUSY` → pool/busy timeout, Ollama timeout → `mcb doctor`/circuit breaker state, settings merge → `jq` recipe), backup/restore, no migration rollback guarantee

**Out of scope фазы 1**: эмбеддинги, MCP server, decision: block, decay, vector search, subagent.

### Фаза 2 — Embeddings + Hybrid search

- `internal/embed/ollama.go`
- INSERT в memory_embeddings при создании memory
- `internal/search` — vector через pure Go cosine, RRF, hybrid
- `mcb embed-missing` — догнать embeddings для memories без вектора
- `mcb embed-rebuild` — пересчитать embeddings при смене model/dim
- Vector search фильтрует `memory_embeddings` по текущим `model` и `dim`
- Ollama circuit breaker: после нескольких failures временно BM25-only
- doctor проверяет reachability Ollama
- `/readyz` начинает учитывать Ollama health (если provider=ollama)
- README: Ollama setup на macOS host и через `docker-compose.ollama.yml`, включая `ollama pull nomic-embed-text`

### Фаза 3 — MCP server

- `internal/mcp` — все tools поверх `mark3labs/mcp-go` streamable HTTP
- Mount на `/mcp` в основном HTTP server'е
- README с конфигом Claude Code `mcpServers.url` и OpenCode `mcp.<name>.type="remote"` / `url`

### Фаза 4 — Subagent compaction + decay

- `/hooks/stop` возвращает `decision: block` JSON в нужных условиях
- `/integrations/opencode/compact` возвращает prompt для OpenCode plugin-driven compactor flow
- `compaction_attempts` + `max_block_attempts` защищают от бесконечного Stop block loop
- `/hooks/subagent-stop` только observability logging; completion отмечается write-side в `memory_session_summary_save`/`memory_save(session_id=...)`
- Bundling `claude/agents/mcb-compactor.md`
- Bundling `opencode/agent/mcb-compactor.md` и plugin orchestration для OpenCode compactor
- Compactor использует `memory_search`, не `memory_recall`, чтобы не омолаживать старые memories
- MCP tool `memory_session_summary_save` и запись `sessions.summary` из compactor'а
- `mcb compact --session <id> [--agent claude-code|opencode]` CLI: print инструкции для manual dispatch
- Конфиг `[compaction]` секция
- Decay job: in-process ticker в `mcb serve` по `decay_interval_hours` + manual `mcb decay`
- Новый MCP tool `memory_supersede` для явного supersession от subagent'а
- Context inject добавляет агент-специфичную подсказку про compactor dispatch

### Фаза 5 — Optional

- **sqlite-vec extension** для SIMD-cosine. `LoadExtension("sqlite_vec")` на старте, виртуальная таблица `vec0` для embeddings. ×5-10 speedup brute force scan'a без своего SIMD-кода. Полезно если per-project n > 20k начнёт регулярно встречаться.
- **Cross-encoder reranker** на top-20 после RRF (`ms-marco-MiniLM-L-6-v2` через ONNX, локально). Включается env-флагом `MCB_RERANK_ENABLED=1`. Trade-off: +30-100ms latency, заметное улучшение точности на коротких/неоднозначных запросах. По данным agentmemory именно reranker даёт основной прирост к R@5 поверх RRF.
- Knowledge graph extraction (entity-based graph traversal как третий search stream)
- Multi-project профили
- Расширенный `memory_update` уже закрыт минимально: правка `tier`/`importance`/text без создания новой memory, с обновлением FTS.
- Web UI (read-only) на отдельном порту

## Критерии готовности по фазе

### Фаза 1

- [ ] `docker compose -f deploy/docker-compose.yml up -d --build` поднимает контейнер, `/healthz` отвечает 200
- [ ] Docker HEALTHCHECK использует `mcb healthz` и не зависит от `wget`/shell
- [ ] Миграции применяются автоматически при первом старте, БД в named volume
- [ ] Claude Code hook endpoints и OpenCode plugin endpoints отвечают < 100ms p95 на пустой БД на macOS Docker Desktop (измерено `curl -w`/plugin smoke test)
- [ ] `/hooks/post-tool` корректно дедуплицирует повторный одинаковый JSON
- [ ] SQLite write handle ограничен `SetMaxOpenConns(1)`; параллельные hook tests не ловят `SQLITE_BUSY`
- [ ] `/hooks/post-tool` редактирует JWT/AWS-key из payload (юнит-тест + integration)
- [ ] `docker compose -f deploy/docker-compose.yml exec mcb mcb add "fact" --project p1` создаёт memory, `docker compose -f deploy/docker-compose.yml exec mcb mcb search "fact" --project p1` находит
- [ ] BM25-поиск возвращает релевантные lexical результаты на русском и английском: точные слова, идентификаторы, файлы, команды, ошибки; morphology не входит в фазу 1
- [ ] `/hooks/session-start` возвращает JSON `hookSpecificOutput.additionalContext`, а `/integrations/opencode/context` возвращает `additional_context`; оба содержат корректный markdown `<mcb-context>`
- [ ] Все юнит-тесты проходят, покрытие `internal/` > 70%
- [ ] `mcb doctor` детектирует corrupt config, missing db, broken permissions, недоступный Ollama
- [ ] Образ собирается на macOS Apple Silicon через Docker Desktop, размер < 35MB
- [ ] Backup через `mcb backup` и offline restore из `.db` работают (документировано в README)
- [ ] `mcb backup --out -` пишет consistent backup в stdout без мусора в volume; `--out PATH` overwrites через temp+rename

### Фаза 2

- [ ] Ollama embed работает с `nomic-embed-text`
- [ ] Hybrid search возвращает результат при поднятой Ollama, fallback на BM25-only при выключенной
- [ ] RRF тесты на синтетических данных
- [ ] `mcb embed-missing` догоняет embeddings для memories без них
- [ ] `mcb embed-rebuild` пересчитывает vectors после смены model/dim; search игнорирует embeddings с чужим model/dim
- [ ] Circuit breaker переводит Ollama failures во временный BM25-only fallback

### Фаза 3

- [ ] `mcb serve` монтирует `/mcp`, endpoint отвечает на MCP `initialize`
- [ ] Все MCP tools соответствующей фазы работают через `mcp-cli`, Claude Code и OpenCode remote MCP config
- [ ] `memory_recall` обновляет accessed_at

### Фаза 4

- [ ] Claude Code Stop hook возвращает корректный `{"decision":"block","reason":...}` JSON в нужных условиях
- [ ] OpenCode `/integrations/opencode/compact` возвращает compactor prompt, а plugin запускает/инжектит `mcb-compactor` flow
- [ ] Compaction request идемпотентен: повторный вызов с тем же session_id не дублирует memories
- [ ] После `max_block_attempts` compaction request перестаёт блокировать/просить compactor и логирует warning
- [ ] `attempt_ttl_seconds` переводит старые requested attempts в failed, и session можно compact повторно после TTL
- [ ] `stop_hook_active=true` корректно обрабатывается (exit 0)
- [ ] Claude Code и OpenCode `mcb-compactor.md` лежат в репо, инструкции установки в README
- [ ] Manual smoke-test: в реальном Claude Code session завершить, увидеть dispatch subagent'a, получить 3-7 сохранённых memories
- [ ] Manual smoke-test: в реальном OpenCode session увидеть capture/context и compactor flow, получить 3-7 сохранённых memories
- [ ] Compactor сохраняет `sessions.summary`, и следующий Claude Code/OpenCode context inject показывает его в `Recent session summaries`
- [ ] `memory_session_summary_save`/`memory_save(session_id=...)` помечают свежий compaction attempt как completed; `SubagentStop` только логирует
- [ ] Compactor использует `memory_search`, а не `memory_recall`; старые memories не получают `accessed_at` от compaction dedup
- [ ] Compactor всегда передаёт `session_id` в `memory_save`; tool description это явно требует
- [ ] `mcb compact --session <id> --agent claude-code|opencode` печатает рабочую agent-specific инструкцию
- [ ] In-process decay ticker по `decay_interval_hours` и manual `mcb decay` понижают importance, eviction срабатывает на min_importance
- [ ] Supersession через новый MCP tool `memory_supersede` работает (subagent вызывает его явно)

## Тестирование

- `go test ./...` — все юнит-тесты
- Интеграционные тесты в `test/integration/`:
  - реальный SQLite файл во временной директории
  - моковый Ollama через `httptest.NewServer`
- Fixture-based hook tests: положить пример stdin JSON в `testdata/hooks/`, прогнать через handler, проверить state БД и stdout (для Stop hook — проверить корректность JSON decision)
- Бенчмарки в `bench/`: insert 10k observations, search 1k queries — целевая медиана < 5ms BM25, < 100ms hybrid с Ollama на macOS
- Линтинг: `go vet`, `staticcheck`, `golangci-lint run` в CI

## Out of scope

- Multi-user / team sharing — single-user only
- Linux/Windows и multi-arch release — best-effort после macOS arm64 baseline; не критерий готовности фаз 1-4
- Удалённый sync — БД локальная (named volume на хосте)
- Web UI / viewer — позже
- Encryption at rest — полагаемся на изоляцию Docker Desktop named volume и опциональный bearer token на HTTP
- Knowledge graph entity extraction — фаза 5+
- Web UI для replay сессий — фаза 5+; JSON replay API уже доступен через `/integrations/replay/session` и MCP `memory_replay`.
- Поддержка агентов кроме Claude Code и OpenCode (Cursor, Cline, прочие) — MCP-tools технически могут работать, но capture/context/compaction требуют отдельный adapter.
- Native (non-Docker) деплой — поддерживается через `--dev` флаг или env, но не первоклассный сценарий. Документация фокусируется на Docker.
- Локальная LLM-компрессия через Ollama в самом mcb — возможно вернуть как fallback `[compaction].mode = "ollama_direct"` если потребуется, но не в дефолтных фазах

## Конвенции кода

- Все ошибки оборачивать через `fmt.Errorf("operation: %w", err)`
- Никаких `panic()` вне `main` и init
- `context.Context` первым аргументом для всех функций с I/O
- Структуры с публичными полями для DTO, методы для сервисов
- Никакого global state кроме slog-logger (через `slog.SetDefault` в main)
- Имена: `MemoryStore`, не `MemoryManager`, не `MemoryService`. Глаголы — методы, не суффиксы типов.
- `cmd/mcb/main.go` содержит только parsing/dispatch подкоманд; логика admin-команд живёт в `internal/admin`.
- Тесты colocated в том же пакете для internal helpers; black-box контрактные тесты можно писать как `package foo_test`; интеграционные отдельно

## Первые шаги для имплементации

1. `go mod init github.com/<user>/mcb`
2. Создать структуру каталогов как в "Структура проекта"
3. Реализовать `internal/config` + `internal/store` + миграция 001
4. Реализовать `internal/dedup` + тесты
5. Реализовать `internal/secrets` + тесты
6. Реализовать `internal/server` — http.Server, middleware, /healthz, /readyz
7. Реализовать `internal/hooks/handlers.go` и `internal/integrations` с роутингом для Claude Code post-tool/session-start и OpenCode tool/context (минимальные)
8. Написать `cmd/mcb/main.go` — entrypoint `serve` + админ-подкоманды
9. `deploy/Dockerfile` + `deploy/docker-compose.yml`, проверить что `docker compose -f deploy/docker-compose.yml up -d --build` поднимает и `/healthz` отвечает
10. Сконфигурировать `~/.claude/settings.json` по `deploy/claude-settings.example.json`, прогнать реальную сессию Claude Code — проверить что observations пишутся
11. Сконфигурировать OpenCode по `deploy/opencode.example.json`, скопировать plugin/agent, перезапустить OpenCode — проверить что observations пишутся и context inject работает
12. Далее по чек-листу фазы 1
