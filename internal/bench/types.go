package bench

const BenchProject = "mcb-benchmark"

type MemoryFixture struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Tier string `json:"tier"`
}

type QueryFixture struct {
	ID      string   `json:"id"`
	Text    string   `json:"text"`
	Type    string   `json:"type"`
	GoldIDs []string `json:"gold_ids"`
}

type Corpus struct {
	Name     string          `json:"name"`
	Version  string          `json:"version"`
	Notes    string          `json:"notes"`
	Memories []MemoryFixture `json:"memories"`
	Queries  []QueryFixture  `json:"queries"`
}

type SearchResult struct {
	ID    string  `json:"id"`
	Text  string  `json:"text"`
	Score float64 `json:"score"`
}

type QueryResult struct {
	QueryID   string           `json:"query_id"`
	Query     string           `json:"query"`
	Type      string           `json:"type"`
	GoldIDs   []string         `json:"gold_ids"`
	RankedIDs []string         `json:"ranked_ids"`
	LatencyMS int64            `json:"latency_ms"`
	Metrics   AggregateMetrics `json:"metrics"`
}

type AggregateMetrics struct {
	PAt5         float64 `json:"p_at_5"`
	RAt5         float64 `json:"r_at_5"`
	RAt10        float64 `json:"r_at_10"`
	RAt20        float64 `json:"r_at_20"`
	HitAt5       float64 `json:"hit_at_5"`
	MRR          float64 `json:"mrr"`
	NDCGAt10     float64 `json:"ndcg_at_10"`
	P50LatencyMS int64   `json:"p50_latency_ms"`
	P95LatencyMS int64   `json:"p95_latency_ms"`
}

type Reference struct {
	Name       string  `json:"name"`
	PAt5       float64 `json:"p_at_5,omitempty"`
	RAt5       float64 `json:"r_at_5,omitempty"`
	RAt10      float64 `json:"r_at_10,omitempty"`
	RAt20      float64 `json:"r_at_20,omitempty"`
	HitAt5Text string  `json:"hit_at_5_text,omitempty"`
	MRR        float64 `json:"mrr,omitempty"`
	NDCGAt10   float64 `json:"ndcg_at_10,omitempty"`
	P50MS      int64   `json:"p50_ms,omitempty"`
	Notes      string  `json:"notes"`
}

type RunResult struct {
	BenchName string                      `json:"bench_name"`
	Adapter   string                      `json:"adapter"`
	Corpus    Corpus                      `json:"corpus"`
	Dataset   *DatasetMetadata            `json:"dataset,omitempty"`
	Metrics   AggregateMetrics            `json:"metrics"`
	ByType    map[string]AggregateMetrics `json:"by_type,omitempty"`
	Queries   []QueryResult               `json:"queries"`
	Reference Reference                   `json:"reference"`
}

type DatasetMetadata struct {
	Name          string `json:"name"`
	Source        string `json:"source,omitempty"`
	SHA256        string `json:"sha256"`
	Bytes         int64  `json:"bytes"`
	Rows          int    `json:"rows"`
	EvaluatedRows int    `json:"evaluated_rows"`
}

type SummaryReport struct {
	BenchName string                      `json:"bench_name"`
	Adapter   string                      `json:"adapter"`
	Corpus    string                      `json:"corpus"`
	Version   string                      `json:"version"`
	Dataset   *DatasetMetadata            `json:"dataset,omitempty"`
	Metrics   AggregateMetrics            `json:"metrics"`
	ByType    map[string]AggregateMetrics `json:"by_type,omitempty"`
	Reference Reference                   `json:"reference"`
}

type RunOptions struct {
	OutDir  string
	Dataset string
	Limit   int
	Now     func() int64
}
