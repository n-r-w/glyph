// Package persistence owns the paths and owner-only directory used by Glyph state.
package persistence

import (
	"fmt"
	"os"
	"path/filepath"
)

const glyphDirectoryMode os.FileMode = 0o700

// Paths contains the persistent files rooted in one Glyph data directory.
type Paths struct {
	Directory       string
	SettingsFile    string
	CredentialsFile string
	LogsDirectory   string
	LogFile         string
}

// Initialize resolves and enforces the current user's Glyph data paths.
func Initialize() (Paths, error) {
	homeDirectory, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve user home directory: %w", err)
	}
	return initializeAt(homeDirectory)
}

// initializeAt builds Glyph data paths under the supplied home directory.
func initializeAt(homeDirectory string) (Paths, error) {
	directory := filepath.Join(filepath.Clean(homeDirectory), ".glyph")
	if err := os.MkdirAll(directory, glyphDirectoryMode); err != nil {
		return Paths{}, fmt.Errorf("create Glyph data directory: %w", err)
	}
	permissionErr := os.Chmod(directory, glyphDirectoryMode)
	if permissionErr != nil {
		return Paths{}, fmt.Errorf("enforce Glyph data directory permissions: %w", permissionErr)
	}
	info, err := os.Stat(directory)
	if err != nil {
		return Paths{}, fmt.Errorf("inspect Glyph data directory: %w", err)
	}
	if !info.IsDir() {
		return Paths{}, fmt.Errorf("glyph data path %q is not a directory", directory)
	}
	logsDirectory := filepath.Join(directory, "logs")
	return Paths{
		Directory:       directory,
		SettingsFile:    filepath.Join(directory, "settings.yaml"),
		CredentialsFile: filepath.Join(directory, "credentials.json"),
		LogsDirectory:   logsDirectory,
		LogFile:         filepath.Join(logsDirectory, "glyph.log"),
	}, nil
}
