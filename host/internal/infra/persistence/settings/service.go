// Package settings loads and validates Host settings.
package settings

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// Service loads one settings file.
type Service struct {
	// path is the settings file path.
	path string
}

// New creates a settings loader for one file.
func New(path string) *Service {
	return &Service{path: path}
}

// Load parses and validates the configured settings file.
func (s *Service) Load() (Settings, error) {
	file, err := os.Open(filepath.Clean(s.path))
	if err != nil {
		return Settings{}, fmt.Errorf("open Glyph settings: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var decoded settingsFile
	if err = decoder.Decode(&decoded); err != nil {
		return Settings{}, fmt.Errorf("decode Glyph settings: %w", err)
	}
	var extra any
	trailingErr := decoder.Decode(&extra)
	if !errors.Is(trailingErr, io.EOF) {
		if trailingErr == nil {
			return Settings{}, errors.New("decode Glyph settings: multiple YAML documents are not allowed")
		}
		return Settings{}, fmt.Errorf("decode trailing Glyph settings: %w", trailingErr)
	}
	return validate(decoded)
}
