// Package paths decides where notraker keeps its files on each OS.
package paths

import (
	"os"
	"path/filepath"
)

// Dir returns the notraker data folder, creating it if needed.
// It lands in the usual per user config spot for the OS.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "notraker")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}
