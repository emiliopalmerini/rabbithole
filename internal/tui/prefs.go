package tui

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/epalmerini/rabbithole/internal/db"
)

const prefsFile = "prefs.json"

// Prefs holds user preferences that persist across sessions.
type Prefs struct {
	SplitRatio float64 `json:"split_ratio,omitempty"`
}

// LoadPrefs reads preferences from dataDir/prefs.json.
// Returns zero-value Prefs on any error (missing file, bad JSON, etc.).
func LoadPrefs(dataDir string) Prefs {
	data, err := os.ReadFile(filepath.Join(dataDir, prefsFile))
	if err != nil {
		return Prefs{}
	}
	var p Prefs
	if err := json.Unmarshal(data, &p); err != nil {
		return Prefs{}
	}
	return p
}

// Save writes preferences to dataDir/prefs.json, creating the directory if needed.
func (p Prefs) Save(dataDir string) error {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dataDir, prefsFile), data, 0644)
}

// loadSplitRatio returns the persisted split ratio, falling back to cfgDefault (or 0.5).
func loadSplitRatio(cfgDefault float64) float64 {
	if dir, err := db.DefaultDataDir(); err == nil {
		if p := LoadPrefs(dir); p.SplitRatio > 0 {
			return p.SplitRatio
		}
	}
	if cfgDefault != 0 {
		return cfgDefault
	}
	return 0.5
}

// saveSplitRatio persists the split ratio to the prefs file (best-effort).
func saveSplitRatio(ratio float64) {
	dir, err := db.DefaultDataDir()
	if err != nil {
		return
	}
	p := LoadPrefs(dir)
	p.SplitRatio = ratio
	_ = p.Save(dir)
}
