# coding-life Benchmark Scorecard

## mcb local run

| Adapter | P@5 | R@5 | R@10 | R@20 | Hit@5 | MRR | NDCG@10 | p50 latency | p95 latency |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| mcb-bm25 | 0.833 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0 ms | 0 ms |

## agentmemory published reference

| P@5 | R@5 | R@10 | R@20 | Hit@5 | MRR | NDCG@10 | p50 latency | Notes |
|---:|---:|---:|---:|---|---:|---:|---:|---|
| 0.578 | 0.967 | n/a | n/a | 15/15 | n/a | n/a | 14 ms | Published upstream coding-agent-life-v1 result. This mcb run uses a clean-room corpus, not the same corpus. |

## By Question Type

| Type | R@5 | R@10 | R@20 | MRR | NDCG@10 |
|---|---:|---:|---:|---:|---:|
| architecture | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 |
| bug | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 |
| decision | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 |
| deployment | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 |
| file_fact | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 |
| workflow | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 |

## Methodology

- mcb local run was executed against corpus `coding-life-cleanroom` version `v1`.
- agentmemory published reference is not a same-run baseline.
- not same corpus: this scorecard uses a clean-room corpus, not upstream coding-agent-life-v1.
- No answer generation, no LLM judge, no server-side LLM calls.
