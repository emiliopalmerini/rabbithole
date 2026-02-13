package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPrefs_Default(t *testing.T) {
	// Point to a nonexistent directory so no file is found
	dir := t.TempDir()
	p := LoadPrefs(filepath.Join(dir, "noexist"))
	if p.SplitRatio != 0 {
		t.Errorf("expected zero default SplitRatio, got %f", p.SplitRatio)
	}
}

func TestSaveAndLoadPrefs(t *testing.T) {
	dir := t.TempDir()
	p := Prefs{SplitRatio: 0.35}
	if err := p.Save(dir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded := LoadPrefs(dir)
	if loaded.SplitRatio != 0.35 {
		t.Errorf("expected SplitRatio 0.35, got %f", loaded.SplitRatio)
	}
}

func TestSavePrefs_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "dir")
	p := Prefs{SplitRatio: 0.6}
	if err := p.Save(dir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(filepath.Join(dir, "prefs.json")); err != nil {
		t.Fatalf("prefs.json not created: %v", err)
	}
}

func TestLoadPrefs_IgnoresCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prefs.json"), []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	p := LoadPrefs(dir)
	if p.SplitRatio != 0 {
		t.Errorf("expected zero default on corrupt file, got %f", p.SplitRatio)
	}
}
