package xdg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDir_EnvSet(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-test-data")
	got, err := Dir("XDG_DATA_HOME", ".local/share")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join("/tmp/xdg-test-data", "rabbithole")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDir_EnvUnset(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	got, err := Dir("XDG_DATA_HOME", ".local/share")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".local", "share", "rabbithole")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDir_ConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test-config")
	got, err := Dir("XDG_CONFIG_HOME", ".config")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join("/tmp/xdg-test-config", "rabbithole")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
