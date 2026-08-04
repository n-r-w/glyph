// Package logging configures application logging sinks.
package logging

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

const (
	logDirectoryMode = 0o700
	logFileMode      = 0o600
)

// OpenUI creates an append-only structured UI logger and its owned file.
func OpenUI(directory, path string) (*slog.Logger, *os.File, error) {
	if err := os.MkdirAll(filepath.Clean(directory), logDirectoryMode); err != nil {
		return nil, nil, fmt.Errorf("create Glyph log directory: %w", err)
	}
	if err := os.Chmod(filepath.Clean(directory), logDirectoryMode); err != nil {
		return nil, nil, fmt.Errorf("enforce Glyph log directory permissions: %w", err)
	}
	info, err := os.Stat(filepath.Clean(directory))
	if err != nil {
		return nil, nil, fmt.Errorf("inspect Glyph log directory: %w", err)
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("glyph log path %q is not a directory", directory)
	}
	file, err := os.OpenFile(filepath.Clean(path), os.O_APPEND|os.O_CREATE|os.O_WRONLY, logFileMode)
	if err != nil {
		return nil, nil, fmt.Errorf("open Glyph log file: %w", err)
	}
	if chmodErr := os.Chmod(filepath.Clean(path), logFileMode); chmodErr != nil {
		return nil, nil, fmt.Errorf("enforce Glyph log file permissions: %w", errors.Join(chmodErr, file.Close()))
	}
	return slog.New(slog.NewJSONHandler(file, nil)), file, nil
}
