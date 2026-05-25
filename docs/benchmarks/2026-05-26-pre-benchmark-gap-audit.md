# Pre-Benchmark Gap Audit

Date: 2026-05-26

Purpose: verify that benchmark work does not hide open core gaps from earlier `mcb` implementation phases or the pragmatic agentmemory parity plan.

## Summary

No remaining blocking gap prevents benchmark implementation after this branch's preflight fixes.

## Fixed Before Benchmarks

| Area | Classification | Resolution |
|---|---|---|
| MCP `memory_save` embeddings | Blocking | `memory_save` now stores embeddings when an MCP embedder is configured. Embedding failure is best-effort and reported without failing memory creation. |
| Claude JSONL import fidelity | Blocking | Import now classifies nested Claude `message.content` `tool_use` and `tool_result` blocks, with representative tests. |
| `memory_export` NDJSON | Blocking | `memory_export` now accepts `format: "ndjson"` and returns typed NDJSON lines. |
| OpenCode compaction prompt handling | Blocking | OpenCode compaction hook now injects mcb compaction requests into compaction context when returned by the server. |
| Compactor supersession instruction | Blocking | Claude and OpenCode compactor agent docs now include `memory_supersede` and instruct its use for duplicates/conflicts. |

## Implemented And Tested

| Area | Status |
|---|---|
| Claude/OpenCode capture endpoints | Implemented and covered by Go server/integration tests. TypeScript plugin remains manually smoke-testable because Node/npm is unavailable locally. |
| BM25 lexical search | Implemented and covered by store/search tests. |
| Hybrid BM25+vector search | Implemented and covered by sqlite_fts5 tests with fake embedders and fallback coverage. |
| MCP tools/resources/prompts | Implemented and covered by MCP tests. |
| Compaction/decay core | Stop gate, attempt TTL/max attempts, manual compact prompt, summary save, supersede, decay, and eviction are covered by Go tests. Real agent dispatch remains manual smoke-testable. |
| Import/replay APIs | JSONL import idempotency and replay HTTP/MCP APIs are covered by tests. |

## Manual Or Environment-Dependent

| Area | Status |
|---|---|
| Claude Code real hook latency and subagent dispatch | Manual smoke-test; not required for retrieval benchmark correctness. |
| OpenCode plugin host behavior | Manual smoke-test; plugin source updated, but local TypeScript check is skipped because Node/npm is unavailable. |
| Docker image size and Docker Desktop p95 latency | Manual/environment-dependent; not a benchmark blocker. |
| Ollama live embedding path | Covered by mocks/fakes in tests and `doctor`; live model pull/run remains environment-dependent. |

## Deferred By Design

| Area | Reason |
|---|---|
| Native Claude marketplace plugin | Explicitly deferred in pragmatic parity plan. |
| Server-side LLM extraction/compression | Explicit non-goal: `mcb` does not call LLM providers. |
| Knowledge graph/team/actions/leases/signals/mesh/snapshots | Heavy features require separate design. |
| Web replay viewer | JSON replay API exists; UI is deferred. |
| SQLite vector extension and reranker | Optional future performance/quality work. |

## Stale Documentation To Avoid In Benchmarks

`arch.md` still contains historical roadmap snippets, including old MCP SDK wording and old schema examples. Current code is the source of truth for benchmark implementation. Public README and benchmark docs should describe current behavior rather than historical phase text.
