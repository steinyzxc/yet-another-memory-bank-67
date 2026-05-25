package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Storage    StorageConfig    `toml:"storage"`
	Server     ServerConfig     `toml:"server"`
	Capture    CaptureConfig    `toml:"capture"`
	Memory     MemoryConfig     `toml:"memory"`
	Embedding  EmbeddingConfig  `toml:"embedding"`
	Search     SearchConfig     `toml:"search"`
	Compaction CompactionConfig `toml:"compaction"`
}

type StorageConfig struct {
	DBPath string `toml:"db_path"`
}

type ServerConfig struct {
	HTTPBind string `toml:"http_bind"`
}

type CaptureConfig struct {
	DedupWindowSeconds int64 `toml:"dedup_window_seconds"`
}

type MemoryConfig struct {
	SessionStartTopN   int     `toml:"session_start_top_n"`
	DecayTauDays       int     `toml:"decay_tau_days"`
	MinImportance      float64 `toml:"min_importance"`
	DecayIntervalHours int     `toml:"decay_interval_hours"`
}

type EmbeddingConfig struct {
	Provider                 string `toml:"provider"`
	OllamaURL                string `toml:"ollama_url"`
	Model                    string `toml:"model"`
	Dimensions               int    `toml:"dimensions"`
	TimeoutMS                int    `toml:"timeout_ms"`
	CircuitBreakerFailures   int    `toml:"circuit_breaker_failures"`
	CircuitBreakerCooldownMS int    `toml:"circuit_breaker_cooldown_ms"`
}

type SearchConfig struct {
	BM25TopK      int `toml:"bm25_top_k"`
	VectorTopK    int `toml:"vector_top_k"`
	FinalTopK     int `toml:"final_top_k"`
	RRFK          int `toml:"rrf_k"`
	MaxPerSession int `toml:"max_per_session"`
}

type CompactionConfig struct {
	Mode              string `toml:"mode"`
	MinObservations   int    `toml:"min_observations"`
	MaxBlockAttempts  int    `toml:"max_block_attempts"`
	AttemptTTLSeconds int64  `toml:"attempt_ttl_seconds"`
	SubagentName      string `toml:"subagent_name"`
}

func Default() Config {
	return Config{
		Storage: StorageConfig{DBPath: "/var/lib/mcb/memory.db"},
		Server:  ServerConfig{HTTPBind: "0.0.0.0:3411"},
		Capture: CaptureConfig{DedupWindowSeconds: 300},
		Memory:  MemoryConfig{SessionStartTopN: 8, DecayTauDays: 30, MinImportance: 0.05, DecayIntervalHours: 24},
		Embedding: EmbeddingConfig{
			Provider:                 "none",
			Model:                    "nomic-embed-text",
			TimeoutMS:                30000,
			CircuitBreakerFailures:   3,
			CircuitBreakerCooldownMS: 120000,
		},
		Search:     SearchConfig{BM25TopK: 50, VectorTopK: 50, FinalTopK: 10, RRFK: 60, MaxPerSession: 3},
		Compaction: CompactionConfig{Mode: "subagent", MinObservations: 5, MaxBlockAttempts: 2, AttemptTTLSeconds: 600, SubagentName: "mcb-compactor"},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config: %w", err)
		}
	}
	if value := os.Getenv("MCB_STORAGE_DB_PATH"); value != "" {
		cfg.Storage.DBPath = value
	}
	if value := os.Getenv("MCB_SERVER_HTTP_BIND"); value != "" {
		cfg.Server.HTTPBind = value
	}
	if value := os.Getenv("MCB_CAPTURE_DEDUP_WINDOW_SECONDS"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			return Config{}, fmt.Errorf("invalid MCB_CAPTURE_DEDUP_WINDOW_SECONDS %q", value)
		}
		cfg.Capture.DedupWindowSeconds = parsed
	}
	if value := os.Getenv("MCB_MEMORY_SESSION_START_TOP_N"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			return Config{}, fmt.Errorf("invalid MCB_MEMORY_SESSION_START_TOP_N %q", value)
		}
		cfg.Memory.SessionStartTopN = parsed
	}
	if err := applyIntEnv("MCB_MEMORY_DECAY_TAU_DAYS", &cfg.Memory.DecayTauDays, true); err != nil {
		return Config{}, err
	}
	if err := applyFloatEnv("MCB_MEMORY_MIN_IMPORTANCE", &cfg.Memory.MinImportance, false); err != nil {
		return Config{}, err
	}
	if err := applyIntEnv("MCB_MEMORY_DECAY_INTERVAL_HOURS", &cfg.Memory.DecayIntervalHours, false); err != nil {
		return Config{}, err
	}
	if value := os.Getenv("MCB_EMBEDDING_PROVIDER"); value != "" {
		cfg.Embedding.Provider = value
	}
	if value := os.Getenv("MCB_EMBEDDING_OLLAMA_URL"); value != "" {
		cfg.Embedding.OllamaURL = value
	}
	if value := os.Getenv("MCB_EMBEDDING_MODEL"); value != "" {
		cfg.Embedding.Model = value
	}
	if err := applyIntEnv("MCB_EMBEDDING_DIMENSIONS", &cfg.Embedding.Dimensions, false); err != nil {
		return Config{}, err
	}
	if err := applyIntEnv("MCB_EMBEDDING_TIMEOUT_MS", &cfg.Embedding.TimeoutMS, true); err != nil {
		return Config{}, err
	}
	if err := applyIntEnv("MCB_EMBEDDING_CIRCUIT_BREAKER_FAILURES", &cfg.Embedding.CircuitBreakerFailures, true); err != nil {
		return Config{}, err
	}
	if err := applyIntEnv("MCB_EMBEDDING_CIRCUIT_BREAKER_COOLDOWN_MS", &cfg.Embedding.CircuitBreakerCooldownMS, true); err != nil {
		return Config{}, err
	}
	if err := applyIntEnv("MCB_SEARCH_BM25_TOP_K", &cfg.Search.BM25TopK, true); err != nil {
		return Config{}, err
	}
	if err := applyIntEnv("MCB_SEARCH_VECTOR_TOP_K", &cfg.Search.VectorTopK, true); err != nil {
		return Config{}, err
	}
	if err := applyIntEnv("MCB_SEARCH_FINAL_TOP_K", &cfg.Search.FinalTopK, true); err != nil {
		return Config{}, err
	}
	if err := applyIntEnv("MCB_SEARCH_RRF_K", &cfg.Search.RRFK, true); err != nil {
		return Config{}, err
	}
	if err := applyIntEnv("MCB_SEARCH_MAX_PER_SESSION", &cfg.Search.MaxPerSession, true); err != nil {
		return Config{}, err
	}
	if value := os.Getenv("MCB_COMPACTION_MODE"); value != "" {
		cfg.Compaction.Mode = value
	}
	if value := os.Getenv("MCB_COMPACTION_SUBAGENT_NAME"); value != "" {
		cfg.Compaction.SubagentName = value
	}
	if err := applyIntEnv("MCB_COMPACTION_MIN_OBSERVATIONS", &cfg.Compaction.MinObservations, true); err != nil {
		return Config{}, err
	}
	if err := applyIntEnv("MCB_COMPACTION_MAX_BLOCK_ATTEMPTS", &cfg.Compaction.MaxBlockAttempts, true); err != nil {
		return Config{}, err
	}
	if err := applyInt64Env("MCB_COMPACTION_ATTEMPT_TTL_SECONDS", &cfg.Compaction.AttemptTTLSeconds, true); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyIntEnv(name string, dst *int, positive bool) error {
	value := os.Getenv(name)
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || (positive && parsed <= 0) || (!positive && parsed < 0) {
		return fmt.Errorf("invalid %s %q", name, value)
	}
	*dst = parsed
	return nil
}

func applyInt64Env(name string, dst *int64, positive bool) error {
	value := os.Getenv(name)
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || (positive && parsed <= 0) || (!positive && parsed < 0) {
		return fmt.Errorf("invalid %s %q", name, value)
	}
	*dst = parsed
	return nil
}

func applyFloatEnv(name string, dst *float64, positive bool) error {
	value := os.Getenv(name)
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || (positive && parsed <= 0) || (!positive && parsed < 0) {
		return fmt.Errorf("invalid %s %q", name, value)
	}
	*dst = parsed
	return nil
}
