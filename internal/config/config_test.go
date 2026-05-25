package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesDefaultsTOMLAndEnvOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(path, []byte(`
[storage]
db_path = "/tmp/from-file.db"

[server]
http_bind = "127.0.0.1:9999"

[capture]
dedup_window_seconds = 123

[memory]
session_start_top_n = 4
decay_tau_days = 15
min_importance = 0.2
decay_interval_hours = 12

[embedding]
provider = "ollama"
ollama_url = "http://ollama:11434"
model = "nomic-embed-text"
dimensions = 768
timeout_ms = 5000
circuit_breaker_failures = 2
circuit_breaker_cooldown_ms = 1000

[search]
bm25_top_k = 20
vector_top_k = 30
final_top_k = 7
rrf_k = 55
max_per_session = 2

[compaction]
mode = "manual"
min_observations = 7
max_block_attempts = 3
attempt_ttl_seconds = 900
subagent_name = "custom-compactor"
`), 0o600)
	if err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("MCB_STORAGE_DB_PATH", "/tmp/from-env.db")
	t.Setenv("MCB_MEMORY_SESSION_START_TOP_N", "6")
	t.Setenv("MCB_EMBEDDING_MODEL", "bge-m3")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Storage.DBPath != "/tmp/from-env.db" {
		t.Fatalf("db path = %q", cfg.Storage.DBPath)
	}
	if cfg.Server.HTTPBind != "127.0.0.1:9999" {
		t.Fatalf("http bind = %q", cfg.Server.HTTPBind)
	}
	if cfg.Capture.DedupWindowSeconds != 123 {
		t.Fatalf("dedup window = %d", cfg.Capture.DedupWindowSeconds)
	}
	if cfg.Memory.SessionStartTopN != 6 {
		t.Fatalf("session start top n = %d", cfg.Memory.SessionStartTopN)
	}
	if cfg.Memory.DecayTauDays != 15 || cfg.Memory.MinImportance != 0.2 || cfg.Memory.DecayIntervalHours != 12 {
		t.Fatalf("memory decay config = %+v", cfg.Memory)
	}
	if cfg.Embedding.Provider != "ollama" || cfg.Embedding.OllamaURL != "http://ollama:11434" || cfg.Embedding.Model != "bge-m3" || cfg.Embedding.Dimensions != 768 {
		t.Fatalf("embedding config = %+v", cfg.Embedding)
	}
	if cfg.Embedding.TimeoutMS != 5000 || cfg.Embedding.CircuitBreakerFailures != 2 || cfg.Embedding.CircuitBreakerCooldownMS != 1000 {
		t.Fatalf("embedding timing config = %+v", cfg.Embedding)
	}
	if cfg.Search.BM25TopK != 20 || cfg.Search.VectorTopK != 30 || cfg.Search.FinalTopK != 7 || cfg.Search.RRFK != 55 || cfg.Search.MaxPerSession != 2 {
		t.Fatalf("search config = %+v", cfg.Search)
	}
	if cfg.Compaction.Mode != "manual" || cfg.Compaction.MinObservations != 7 || cfg.Compaction.MaxBlockAttempts != 3 || cfg.Compaction.AttemptTTLSeconds != 900 || cfg.Compaction.SubagentName != "custom-compactor" {
		t.Fatalf("compaction config = %+v", cfg.Compaction)
	}
}

func TestDefaultConfigMatchesPhaseOneRuntime(t *testing.T) {
	cfg := Default()
	if cfg.Storage.DBPath != "/var/lib/mcb/memory.db" {
		t.Fatalf("default db path = %q", cfg.Storage.DBPath)
	}
	if cfg.Server.HTTPBind != "0.0.0.0:3411" {
		t.Fatalf("default http bind = %q", cfg.Server.HTTPBind)
	}
	if cfg.Capture.DedupWindowSeconds != 300 {
		t.Fatalf("default dedup window = %d", cfg.Capture.DedupWindowSeconds)
	}
	if cfg.Memory.SessionStartTopN != 8 {
		t.Fatalf("default session top n = %d", cfg.Memory.SessionStartTopN)
	}
	if cfg.Memory.DecayTauDays != 30 || cfg.Memory.MinImportance != 0.05 || cfg.Memory.DecayIntervalHours != 24 {
		t.Fatalf("default memory decay = %+v", cfg.Memory)
	}
	if cfg.Embedding.Provider != "none" || cfg.Embedding.Model != "nomic-embed-text" || cfg.Embedding.TimeoutMS != 30000 {
		t.Fatalf("default embedding = %+v", cfg.Embedding)
	}
	if cfg.Search.BM25TopK != 50 || cfg.Search.VectorTopK != 50 || cfg.Search.FinalTopK != 10 || cfg.Search.RRFK != 60 || cfg.Search.MaxPerSession != 3 {
		t.Fatalf("default search = %+v", cfg.Search)
	}
	if cfg.Compaction.Mode != "subagent" || cfg.Compaction.MinObservations != 5 || cfg.Compaction.MaxBlockAttempts != 2 || cfg.Compaction.AttemptTTLSeconds != 600 || cfg.Compaction.SubagentName != "mcb-compactor" {
		t.Fatalf("default compaction = %+v", cfg.Compaction)
	}
}

func TestLoadCompactionEnvOverrides(t *testing.T) {
	t.Setenv("MCB_COMPACTION_MODE", "disabled")
	t.Setenv("MCB_COMPACTION_MIN_OBSERVATIONS", "9")
	t.Setenv("MCB_COMPACTION_MAX_BLOCK_ATTEMPTS", "4")
	t.Setenv("MCB_COMPACTION_ATTEMPT_TTL_SECONDS", "1200")
	t.Setenv("MCB_COMPACTION_SUBAGENT_NAME", "other-compactor")
	t.Setenv("MCB_MEMORY_DECAY_TAU_DAYS", "20")
	t.Setenv("MCB_MEMORY_MIN_IMPORTANCE", "0.15")
	t.Setenv("MCB_MEMORY_DECAY_INTERVAL_HOURS", "0")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Compaction.Mode != "disabled" || cfg.Compaction.MinObservations != 9 || cfg.Compaction.MaxBlockAttempts != 4 || cfg.Compaction.AttemptTTLSeconds != 1200 || cfg.Compaction.SubagentName != "other-compactor" {
		t.Fatalf("compaction config = %+v", cfg.Compaction)
	}
	if cfg.Memory.DecayTauDays != 20 || cfg.Memory.MinImportance != 0.15 || cfg.Memory.DecayIntervalHours != 0 {
		t.Fatalf("memory config = %+v", cfg.Memory)
	}
}
