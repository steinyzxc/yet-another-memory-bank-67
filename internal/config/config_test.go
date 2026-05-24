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
`), 0o600)
	if err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("MCB_STORAGE_DB_PATH", "/tmp/from-env.db")
	t.Setenv("MCB_MEMORY_SESSION_START_TOP_N", "6")

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
}
