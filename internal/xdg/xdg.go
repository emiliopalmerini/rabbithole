package xdg

import (
	"os"
	"path/filepath"
)

const appName = "rabbithole"

// Dir returns the XDG directory for the application.
// It checks envVar first (e.g. XDG_DATA_HOME), falling back to ~/fallbackDot
// (e.g. .local/share). The result always has "/rabbithole" appended.
func Dir(envVar, fallbackDot string) (string, error) {
	if dir := os.Getenv(envVar); dir != "" {
		return filepath.Join(dir, appName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, fallbackDot, appName), nil
}
