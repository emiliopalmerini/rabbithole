package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const (
	configFile         = "config.toml"
	prefsFile          = "prefs.json"
	defaultMaxMessages = 1000
	defaultSplitRatio  = 0.5
)

// FileConfig is the TOML file structure.
type FileConfig struct {
	Proto       string             `toml:"proto"`
	MaxMessages int                `toml:"max_messages"`
	DBPath      string             `toml:"db"`
	UI          UIConfig           `toml:"ui"`
	Profiles    map[string]Profile `toml:"profiles"`
}

// UIConfig holds UI-related settings.
type UIConfig struct {
	SplitRatio  float64 `toml:"split_ratio"`
	CompactMode bool    `toml:"compact_mode"`
}

// Profile is a named connection profile.
type Profile struct {
	URL           string `toml:"url"`
	ManagementURL string `toml:"management_url"`
	Proto         string `toml:"proto"`
}

// Config is the resolved runtime config after profile selection.
type Config struct {
	RabbitMQURL   string
	ManagementURL string
	ProtoPath     string
	DBPath        string
	MaxMessages   int

	// UI
	DefaultSplitRatio float64
	CompactMode       bool

	// Runtime (set by browser on consume)
	Exchange   string
	RoutingKey string
	QueueName  string
	Durable    bool

	// For saving prefs back
	ConfigDir string
}

// MessageLimit returns MaxMessages, falling back to the default if unset.
func (c Config) MessageLimit() int {
	if c.MaxMessages <= 0 {
		return defaultMaxMessages
	}
	return c.MaxMessages
}

// LoadFileConfig loads config.toml from configDir.
// Returns a zero-value FileConfig (no error) if the file doesn't exist.
func LoadFileConfig(configDir string) (*FileConfig, error) {
	path := filepath.Join(configDir, configFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &FileConfig{}, nil
		}
		return nil, err
	}

	var cfg FileConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Resolve merges a profile (by name) with global config and env vars into a runtime Config.
// If profileName is empty or not found, only global/env settings are used.
func (fc FileConfig) Resolve(profileName string, configDir string) Config {
	cfg := Config{
		ProtoPath: fc.Proto,
		DBPath:    fc.DBPath,
		ConfigDir: configDir,
	}

	// Max messages
	cfg.MaxMessages = fc.MaxMessages
	if cfg.MaxMessages <= 0 {
		cfg.MaxMessages = defaultMaxMessages
	}

	// UI defaults
	cfg.DefaultSplitRatio = fc.UI.SplitRatio
	if cfg.DefaultSplitRatio == 0 {
		cfg.DefaultSplitRatio = defaultSplitRatio
	}
	cfg.CompactMode = fc.UI.CompactMode

	// Apply profile overrides
	if p, ok := fc.Profiles[profileName]; ok {
		cfg.RabbitMQURL = p.URL
		cfg.ManagementURL = p.ManagementURL
		if p.Proto != "" {
			cfg.ProtoPath = p.Proto
		}
	}

	// Fall back to env vars for URL if not set by profile
	if cfg.RabbitMQURL == "" {
		if u := os.Getenv("AMQP_URL"); u != "" {
			cfg.RabbitMQURL = u
		} else if u := os.Getenv("RABBITMQ_URL"); u != "" {
			cfg.RabbitMQURL = u
		}
	}

	return cfg
}

// SaveSplitRatio reads the existing TOML (if any), updates split_ratio, and writes back.
func SaveSplitRatio(configDir string, ratio float64) error {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	path := filepath.Join(configDir, configFile)

	// Load existing config to preserve other fields
	cfg, err := LoadFileConfig(configDir)
	if err != nil {
		cfg = &FileConfig{}
	}
	cfg.UI.SplitRatio = ratio

	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(cfg); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0644)
}

// MigratePrefs migrates prefs.json from dataDir into config.toml in configDir.
// It's a no-op if prefs.json doesn't exist or config.toml already exists.
func MigratePrefs(dataDir, configDir string) error {
	// Don't overwrite existing config
	configPath := filepath.Join(configDir, configFile)
	if _, err := os.Stat(configPath); err == nil {
		return nil
	}

	// Read prefs.json
	prefsPath := filepath.Join(dataDir, prefsFile)
	data, err := os.ReadFile(prefsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	var prefs struct {
		SplitRatio float64 `json:"split_ratio"`
	}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return nil // ignore corrupt prefs
	}

	if prefs.SplitRatio > 0 {
		return SaveSplitRatio(configDir, prefs.SplitRatio)
	}
	return nil
}

// ProfileNames returns a sorted list of profile names.
func (fc FileConfig) ProfileNames() []string {
	names := make([]string, 0, len(fc.Profiles))
	for name := range fc.Profiles {
		names = append(names, name)
	}
	// Sort for deterministic ordering
	sortStrings(names)
	return names
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
