package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFileConfig_ValidTOML(t *testing.T) {
	dir := t.TempDir()
	toml := `
proto = "/path/to/protos"
max_messages = 2000
db = "/custom/db.sqlite"

[ui]
split_ratio = 0.7
compact_mode = true

[profiles.local]
url = "amqp://guest:guest@localhost:5672/"

[profiles.staging]
url = "amqp://user:pass@staging:5672/"
management_url = "http://staging:15672/api"
proto = "/staging/protos"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(toml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFileConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Proto != "/path/to/protos" {
		t.Errorf("Proto = %q, want %q", cfg.Proto, "/path/to/protos")
	}
	if cfg.MaxMessages != 2000 {
		t.Errorf("MaxMessages = %d, want 2000", cfg.MaxMessages)
	}
	if cfg.DBPath != "/custom/db.sqlite" {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, "/custom/db.sqlite")
	}
	if cfg.UI.SplitRatio != 0.7 {
		t.Errorf("UI.SplitRatio = %f, want 0.7", cfg.UI.SplitRatio)
	}
	if !cfg.UI.CompactMode {
		t.Error("UI.CompactMode = false, want true")
	}
	if len(cfg.Profiles) != 2 {
		t.Fatalf("len(Profiles) = %d, want 2", len(cfg.Profiles))
	}
	if cfg.Profiles["staging"].ManagementURL != "http://staging:15672/api" {
		t.Errorf("staging ManagementURL = %q", cfg.Profiles["staging"].ManagementURL)
	}
	if cfg.Profiles["staging"].Proto != "/staging/protos" {
		t.Errorf("staging Proto = %q", cfg.Profiles["staging"].Proto)
	}
}

func TestLoadFileConfig_MissingFile(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadFileConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}
	if cfg.Proto != "" {
		t.Errorf("expected empty Proto, got %q", cfg.Proto)
	}
	if len(cfg.Profiles) != 0 {
		t.Errorf("expected no profiles, got %d", len(cfg.Profiles))
	}
}

func TestLoadFileConfig_InvalidTOML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("not valid [[[ toml"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFileConfig(dir)
	if err == nil {
		t.Fatal("expected error for invalid TOML")
	}
}

func TestResolve_WithProfile(t *testing.T) {
	fc := FileConfig{
		Proto:       "/global/protos",
		MaxMessages: 500,
		UI:          UIConfig{SplitRatio: 0.6},
		Profiles: map[string]Profile{
			"staging": {
				URL:           "amqp://staging:5672/",
				ManagementURL: "http://staging:15672/api",
				Proto:         "/staging/protos",
			},
		},
	}
	cfg := fc.Resolve("staging", "/tmp/config")

	if cfg.RabbitMQURL != "amqp://staging:5672/" {
		t.Errorf("RabbitMQURL = %q", cfg.RabbitMQURL)
	}
	if cfg.ManagementURL != "http://staging:15672/api" {
		t.Errorf("ManagementURL = %q", cfg.ManagementURL)
	}
	if cfg.ProtoPath != "/staging/protos" {
		t.Errorf("ProtoPath = %q, want /staging/protos (profile override)", cfg.ProtoPath)
	}
	if cfg.MaxMessages != 500 {
		t.Errorf("MaxMessages = %d", cfg.MaxMessages)
	}
}

func TestResolve_ProfileProtoFallsBackToGlobal(t *testing.T) {
	fc := FileConfig{
		Proto: "/global/protos",
		Profiles: map[string]Profile{
			"local": {URL: "amqp://localhost:5672/"},
		},
	}
	cfg := fc.Resolve("local", "/tmp/config")

	if cfg.ProtoPath != "/global/protos" {
		t.Errorf("ProtoPath = %q, want /global/protos (global fallback)", cfg.ProtoPath)
	}
}

func TestResolve_DefaultMaxMessages(t *testing.T) {
	fc := FileConfig{}
	cfg := fc.Resolve("", "/tmp/config")

	if cfg.MaxMessages != defaultMaxMessages {
		t.Errorf("MaxMessages = %d, want %d", cfg.MaxMessages, defaultMaxMessages)
	}
}

func TestResolve_DefaultSplitRatio(t *testing.T) {
	fc := FileConfig{}
	cfg := fc.Resolve("", "/tmp/config")

	if cfg.DefaultSplitRatio != defaultSplitRatio {
		t.Errorf("DefaultSplitRatio = %f, want %f", cfg.DefaultSplitRatio, defaultSplitRatio)
	}
}

func TestResolve_EnvVarFallback(t *testing.T) {
	t.Setenv("AMQP_URL", "amqp://from-env:5672/")
	fc := FileConfig{}
	cfg := fc.Resolve("", "/tmp/config")

	if cfg.RabbitMQURL != "amqp://from-env:5672/" {
		t.Errorf("RabbitMQURL = %q, want amqp://from-env:5672/", cfg.RabbitMQURL)
	}
}

func TestResolve_EnvVarRabbitMQURL(t *testing.T) {
	t.Setenv("AMQP_URL", "")
	t.Setenv("RABBITMQ_URL", "amqp://rabbit-env:5672/")
	fc := FileConfig{}
	cfg := fc.Resolve("", "/tmp/config")

	if cfg.RabbitMQURL != "amqp://rabbit-env:5672/" {
		t.Errorf("RabbitMQURL = %q, want amqp://rabbit-env:5672/", cfg.RabbitMQURL)
	}
}

func TestSaveSplitRatio_UpdatesExistingTOML(t *testing.T) {
	dir := t.TempDir()
	initial := `
proto = "/protos"

[ui]
split_ratio = 0.5
compact_mode = true

[profiles.local]
url = "amqp://localhost:5672/"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	if err := SaveSplitRatio(dir, 0.7); err != nil {
		t.Fatalf("SaveSplitRatio failed: %v", err)
	}

	// Reload and verify
	cfg, err := LoadFileConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.SplitRatio != 0.7 {
		t.Errorf("SplitRatio = %f, want 0.7", cfg.UI.SplitRatio)
	}
	if cfg.Proto != "/protos" {
		t.Errorf("Proto = %q, should be preserved", cfg.Proto)
	}
	if !cfg.UI.CompactMode {
		t.Error("CompactMode should be preserved")
	}
	if cfg.Profiles["local"].URL != "amqp://localhost:5672/" {
		t.Error("profile should be preserved")
	}
}

func TestSaveSplitRatio_CreatesFileIfMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "dir")

	if err := SaveSplitRatio(dir, 0.65); err != nil {
		t.Fatalf("SaveSplitRatio failed: %v", err)
	}

	cfg, err := LoadFileConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.SplitRatio != 0.65 {
		t.Errorf("SplitRatio = %f, want 0.65", cfg.UI.SplitRatio)
	}
}

func TestMigratePrefs_ExistingPrefsJSON(t *testing.T) {
	dataDir := t.TempDir()
	configDir := t.TempDir()

	// Write a prefs.json file
	prefs := `{"split_ratio": 0.35}`
	if err := os.WriteFile(filepath.Join(dataDir, "prefs.json"), []byte(prefs), 0644); err != nil {
		t.Fatal(err)
	}

	if err := MigratePrefs(dataDir, configDir); err != nil {
		t.Fatalf("MigratePrefs failed: %v", err)
	}

	cfg, err := LoadFileConfig(configDir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.SplitRatio != 0.35 {
		t.Errorf("SplitRatio = %f, want 0.35", cfg.UI.SplitRatio)
	}
}

func TestMigratePrefs_NoPrefsJSON(t *testing.T) {
	dataDir := t.TempDir()
	configDir := t.TempDir()

	if err := MigratePrefs(dataDir, configDir); err != nil {
		t.Fatalf("MigratePrefs failed: %v", err)
	}

	// config.toml should not be created
	_, err := os.Stat(filepath.Join(configDir, "config.toml"))
	if !os.IsNotExist(err) {
		t.Error("config.toml should not be created when no prefs.json exists")
	}
}

func TestMigratePrefs_SkipsIfConfigExists(t *testing.T) {
	dataDir := t.TempDir()
	configDir := t.TempDir()

	// Write both files
	prefs := `{"split_ratio": 0.35}`
	if err := os.WriteFile(filepath.Join(dataDir, "prefs.json"), []byte(prefs), 0644); err != nil {
		t.Fatal(err)
	}
	existing := `[ui]
split_ratio = 0.8
`
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := MigratePrefs(dataDir, configDir); err != nil {
		t.Fatalf("MigratePrefs failed: %v", err)
	}

	// Should preserve existing config.toml
	cfg, err := LoadFileConfig(configDir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.SplitRatio != 0.8 {
		t.Errorf("SplitRatio = %f, want 0.8 (existing should be preserved)", cfg.UI.SplitRatio)
	}
}
