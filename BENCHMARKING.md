# Benchmarking mcb

This document defines how to measure whether mcb actually improves agent work.

The main risk is measuring only latency. mcb is a memory system, so the primary question is: does it retrieve and inject useful context that changes outcomes without adding unacceptable overhead?

## What To Compare

Use these configurations as baselines:

1. `no-memory`: agent without mcb integration.
2. `capture-only`: mcb hooks enabled, no manual memories, embeddings disabled.
3. `bm25`: saved memories, BM25 search only, `MCB_EMBEDDING_PROVIDER=none`.
4. `hybrid`: BM25 plus Ollama embeddings through `deploy/docker-compose.ollama.yml`.
5. `compacted`: hybrid plus session compaction summaries/facts.

For search-specific benchmarks, compare `bm25` vs `hybrid`. For agent outcome benchmarks, compare `no-memory`, `bm25`, and `compacted`.

## Metrics

### Retrieval Quality

Use judged query-memory pairs.

- `Recall@K`: whether any expected memory appears in top K.
- `MRR@K`: reciprocal rank of the first expected memory.
- `nDCG@K`: useful when several memories are relevant with graded labels.
- `diversity`: number of distinct sessions represented in top K.
- `stale-hit-rate`: percent of top K results that are superseded, obsolete, or wrong.

Recommended K values: `1`, `3`, `5`, `10`.

### Latency And Overhead

Measure p50, p90, p95, p99.

- hook capture latency: `/hooks/post-tool`, `/hooks/user-prompt`, `/integrations/opencode/tool`, `/integrations/opencode/chat`
- context inject latency: `/hooks/session-start`, `/integrations/opencode/context`
- MCP tool latency: `memory_search`, `memory_recall`, `memory_save`, `memory_session_observations`
- search internal latency: BM25 query, vector candidate load, cosine scoring, RRF, `TouchMemories`
- compaction gate latency: `/hooks/stop`, `/integrations/opencode/compact`
- embedding maintenance throughput: `mcb embed-missing`, `mcb embed-rebuild`

Hard budget from the architecture: hook capture should stay below 100 ms p95 on an empty or small DB. Search should stay human-invisible for interactive use; target p95 below 150 ms excluding Ollama cold starts.

The live HTTP performance workflow uses two commands. Seed a disposable namespace inside the container so the benchmark uses the real SQLite volume:

```bash
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.ollama.yml exec mcb \
  mcb perf-seed --db /var/lib/mcb/memory.db --project /mcb-perf/dev --run-id smoke --memories 10000 --observations 100000 --sessions 100
```

Then run the benchmark from the host against the live server:

```bash
mcb bench perf --url http://127.0.0.1:3411 --project /mcb-perf/dev --run-id smoke --out /tmp/mcb-perf --concurrency 1,10,50 --requests 1000
```

Use the same `--project` and `--run-id` for both commands. Re-running `perf-seed` with the same pair replaces only that generated seed data; other projects and run IDs are left intact. The report files are `raw.ndjson`, `summary.json`, and `scorecard.md`. Budgets are warnings unless `--fail-on-budget` is passed.

For hybrid MCP timings with Ollama, populate embeddings after seeding:

```bash
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.ollama.yml exec mcb \
  mcb embed-missing --db /var/lib/mcb/memory.db --project /mcb-perf/dev
```

### Agent Outcome Quality

Use task-level evaluations, not just search metrics.

- task success rate
- number of turns to completion
- number of times the agent asks for already-known project facts
- number of incorrect assumptions about prior decisions
- amount of repeated exploration, measured by duplicate file reads/searches
- reviewer score on final answer or code diff

For compaction, also measure:

- facts saved per session
- useful facts saved per session, judged manually
- duplicate fact rate
- unsafe fact rate: secrets, tmp paths, speculation, raw IDs
- next-session hit rate: whether compacted memories are used in future tasks

## Dataset Design

Use three dataset tiers.

### Synthetic Retrieval Dataset

Purpose: cheap regression tests for search behavior.

Create memory fixtures with:

- exact lexical matches: commands, paths, function names, errors
- paraphrases: “auth token validation” vs “JWT middleware checks bearer tokens”
- Russian morphology/paraphrases
- stale memories with `superseded_by`
- multiple memories from the same session to test diversification
- distractor projects to test project scoping

Queries should have expected memory IDs and graded relevance labels.

This dataset answers: did hybrid improve semantic recall without breaking exact lookup?

### Recorded Session Dataset

Purpose: measure realistic capture, context inject, and compaction.

Record 20-50 real or scripted sessions:

- feature implementation
- bug fix
- config change
- dependency/debugging task
- code review follow-up
- documentation update

For each session, keep:

- raw hook events
- final human summary of what should become durable memory
- follow-up queries that should retrieve those facts
- a small set of “should not save” items

This dataset answers: does compaction produce useful durable memories?

### End-To-End Agent Task Dataset

Purpose: measure whether agents perform better.

Use paired tasks:

- first task establishes project facts and decisions
- later task requires those facts but does not restate them

Examples:

- “Implement feature A using the project’s chosen auth library.” Prior session established the chosen library.
- “Fix the failing test the same way as last time.” Prior session contains the fix pattern.
- “Add another integration following the established command pattern.” Prior session captured the command pattern.

Run each task in `no-memory`, `bm25`, and `compacted` modes with identical starting repo state.

This dataset answers: does mcb reduce repeated discovery and improve task success?

## Benchmark Harness

Add a separate benchmark harness rather than overloading unit tests.

Recommended layout:

```text
bench/
├── README.md
├── datasets/
│   ├── synthetic_memories.jsonl
│   ├── synthetic_queries.jsonl
│   └── recorded_sessions/
├── cmd/mcb-bench/
│   └── main.go
└── results/
    └── .gitkeep
```

The retrieval harness should:

1. create a temp SQLite DB
2. load memories/sessions/observations
3. optionally generate or import embeddings
4. run query sets against BM25 and hybrid
5. run HTTP endpoint latency checks against a live `mcb serve`
6. write JSONL results
7. print a concise markdown summary

The live performance harness is implemented by `mcb perf-seed` and `mcb bench perf`; it does not orchestrate Docker and intentionally measures the already-running server.

Recommended output schema per query:

```json
{"mode":"hybrid","query_id":"q001","latency_ms":42,"result_ids":[12,9,4],"expected_ids":[9],"recall_at_5":true,"mrr_at_10":0.5}
```

Recommended output schema per endpoint sample:

```json
{"endpoint":"/hooks/post-tool","mode":"capture-only","n_memories":10000,"latency_ms":7,"status":204}
```

## Search Benchmarks

Run at these scales per project:

- 100 memories
- 1,000 memories
- 10,000 memories
- 50,000 memories

For each scale, measure:

- BM25-only latency
- hybrid latency with fake in-process embedder to isolate vector/RRF cost
- hybrid latency with real Ollama to measure user-facing cost
- vector candidate load latency
- cosine scoring latency
- total allocations via Go benchmarks

Important: separate real Ollama timing from vector-search timing. Ollama latency can dominate and has cold-start effects.

Useful commands once benchmarks exist:

```bash
go test -bench=. -benchmem ./internal/search ./internal/store
go test -tags sqlite_fts5 -bench=. -benchmem ./internal/search ./internal/store
```

## Capture Benchmarks

Replay hook payloads through the HTTP server.

Scenarios:

- small tool payload under 512 bytes
- large tool payload using zstd
- repeated duplicate payload inside dedup window
- concurrent hook writes, 10-100 workers
- secret-heavy payload to measure redaction overhead

Metrics:

- p95 latency
- insertion throughput
- duplicate suppression rate
- `SQLITE_BUSY` count, should be zero
- DB size growth per observation

## Context Injection Benchmarks

Measure `/hooks/session-start` and `/integrations/opencode/context`.

Vary:

- memories per project
- summaries per project
- `session_start_top_n`
- compaction hint enabled/disabled

Metrics:

- latency p95
- response bytes
- number of injected memories
- judged usefulness of injected context for next task

## Compaction Benchmarks

Compaction involves an agent, so split it into two layers.

### Gate Benchmark

Measure mcb-only decisions:

- session below `min_observations`: should return 204
- session needing compaction: should return block/prompt
- already summarized session: should return 204 or `compact=false`
- max attempts reached: should stop blocking
- TTL expired: should allow retry

Metrics:

- decision latency
- correctness of decision
- attempt state transitions

### Agent Compaction Benchmark

Run the real compactor on recorded sessions.

Metrics:

- summary quality score
- useful facts saved
- duplicate facts saved
- unsafe facts saved
- time to compact
- next-session retrieval hit rate

This requires manual or LLM-as-judge review. Keep at least a small human-reviewed gold set to calibrate automated judging.

## End-To-End Agent Evaluation

For each task, run the agent with a clean workspace and one of the memory modes.

Recommended modes:

- `no-memory`: no hooks, no MCP
- `bm25`: hooks and MCP, embeddings disabled
- `compacted`: hooks, MCP, compactor, embeddings optional

Record:

- transcript
- commands/tools used
- final diff
- test results
- reviewer score
- elapsed time
- number of repeated discoveries

Use at least 10 tasks before trusting trends. For noisy agent runs, use 3 runs per task/mode and compare medians.

## Interpreting Results

Good outcome:

- `Recall@5` improves meaningfully from BM25 to hybrid on paraphrase queries
- exact lexical queries do not regress
- context inject remains below p95 latency budget
- capture overhead stays below hook budget
- compaction facts are mostly useful and rarely unsafe
- end-to-end tasks show fewer repeated discoveries or higher success rate

Bad outcome:

- hybrid only improves synthetic queries but not end-to-end tasks
- compaction saves noisy facts that pollute future context
- p95 hook latency increases enough to be noticeable
- context injection becomes too large and distracts the agent
- stale memories appear in top results

## Minimum Viable Benchmark Suite

Start with this small suite:

1. 200-memory synthetic retrieval dataset, 50 queries, judged expected IDs.
2. Hook replay benchmark with 1,000 small payloads and 1,000 large payloads.
3. Context inject benchmark with 1,000 memories and 20 summaries.
4. Five recorded sessions for compaction quality review.
5. Five paired end-to-end tasks run in `no-memory` and `compacted` modes.

This is enough to answer whether mcb is directionally useful before investing in large-scale benchmarking.

## What Not To Benchmark First

Do not start with sqlite vector extensions, rerankers, HNSW, or web UI metrics. The current bottleneck to validate is usefulness, not vector scan speed. Add lower-level speed work only if the benchmark shows vector lookup is a real bottleneck at realistic project sizes.
