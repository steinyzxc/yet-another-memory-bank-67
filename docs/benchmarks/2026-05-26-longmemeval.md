# longmemeval Benchmark Scorecard

## mcb local run

| Adapter | P@5 | R@5 | R@10 | R@20 | Hit@5 | MRR | NDCG@10 | p50 latency | p95 latency |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| mcb-bm25 | n/a | 0.890 | 0.936 | 0.974 | 0.890 | 0.787 | 0.776 | 5 ms | 6 ms |

## agentmemory BM25-only published reference

| P@5 | R@5 | R@10 | R@20 | Hit@5 | MRR | NDCG@10 | p50 latency | Notes |
|---:|---:|---:|---:|---|---:|---:|---:|---|
| n/a | 0.862 | 0.946 | 0.986 | n/a | 0.715 | 0.730 | n/a | Same-adapter published LongMemEval-S reference. BM25+Vector reference: R@5 0.952, R@10 0.986, R@20 0.994, MRR 0.882, NDCG@10 0.879. |

## By Question Type

| Type | R@5 | R@10 | R@20 | MRR | NDCG@10 |
|---|---:|---:|---:|---:|---:|
| knowledge-update | 0.974 | 0.987 | 1.000 | 0.929 | 0.916 |
| multi-session | 0.955 | 0.977 | 0.985 | 0.856 | 0.785 |
| single-session-assistant | 0.786 | 0.857 | 0.946 | 0.660 | 0.703 |
| single-session-preference | 0.467 | 0.667 | 0.833 | 0.294 | 0.371 |
| single-session-user | 0.929 | 0.971 | 0.986 | 0.817 | 0.854 |
| temporal-reasoning | 0.895 | 0.940 | 0.985 | 0.785 | 0.768 |

## Methodology

- mcb local run was executed against corpus `longmemeval-s-user-supplied` version `native`.
- Dataset `longmemeval_s_cleaned.json`: 500 answerable rows, 500 evaluated rows, 277383467 bytes, sha256 `d6f21ea9d60a0d56f34a05b609c79c88a451d2ae03597821ea3d5a9678c3a442`.
- Dataset source: https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned/resolve/main/longmemeval_s_cleaned.json.
- agentmemory published reference is not a same-run baseline.
- Methodology: fresh index per question from haystack sessions; abstention types excluded; R@K is recall_any@K.
- No answer generation, no LLM judge, no server-side LLM calls.
