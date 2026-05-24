package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Storage StorageConfig `toml:"storage"`
	Server  ServerConfig  `toml:"server"`
	Capture CaptureConfig `toml:"capture"`
	Memory  MemoryConfig  `toml:"memory"`
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
	SessionStartTopN int `toml:"session_start_top_n"`
}

func Default() Config {
	return Config{
		Storage: StorageConfig{DBPath: "/var/lib/mcb/memory.db"},
		Server:  ServerConfig{HTTPBind: "0.0.0.0:3411"},
		Capture: CaptureConfig{DedupWindowSeconds: 300},
		Memory:  MemoryConfig{SessionStartTopN: 8},
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
	return cfg, nil
}
