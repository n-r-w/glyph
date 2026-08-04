// Package settings loads and validates prototype Host settings.
package settings

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/n-r-w/glyph/host/internal/domain/pluginid"
)

// ThinkingLevel is one OpenAI SDK reasoning-effort enum value.
type ThinkingLevel string

const (
	// ThinkingLevelNone disables model reasoning when the selected model supports it.
	ThinkingLevelNone ThinkingLevel = "none"
	// ThinkingLevelMinimal requests minimal model reasoning.
	ThinkingLevelMinimal ThinkingLevel = "minimal"
	// ThinkingLevelLow requests low model reasoning.
	ThinkingLevelLow ThinkingLevel = "low"
	// ThinkingLevelMedium requests medium model reasoning.
	ThinkingLevelMedium ThinkingLevel = "medium"
	// ThinkingLevelHigh requests high model reasoning.
	ThinkingLevelHigh ThinkingLevel = "high"
	// ThinkingLevelXHigh requests extra-high model reasoning.
	ThinkingLevelXHigh ThinkingLevel = "xhigh"
	// ThinkingLevelMax requests maximum model reasoning.
	ThinkingLevelMax ThinkingLevel = "max"
)

// Settings contains the validated startup model and UI selection.
type Settings struct {
	DefaultProvider      string
	DefaultModel         string
	DefaultThinkingLevel *ThinkingLevel
	ActiveUI             string
}

// Service loads one settings file.
type Service struct {
	path string
}

// New creates a settings loader for one file.
func New(path string) *Service {
	return &Service{path: path}
}

// settingsFile is the strict YAML representation owned by Host persistence.
type settingsFile struct {
	DefaultProvider      string         `yaml:"defaultProvider"`
	DefaultModel         string         `yaml:"defaultModel"`
	DefaultThinkingLevel *ThinkingLevel `yaml:"defaultThinkingLevel"`
	ActiveUI             *string        `yaml:"activeUI"`
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
	decodeErr := decoder.Decode(&decoded)
	if decodeErr != nil {
		return Settings{}, fmt.Errorf("decode Glyph settings: %w", decodeErr)
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

// validate applies the prototype's closed provider and startup selection rules.
func validate(decoded settingsFile) (Settings, error) {
	if decoded.DefaultProvider != "openai-codex" {
		return Settings{}, errors.New("defaultProvider must be openai-codex")
	}
	if decoded.DefaultModel == "" || decoded.DefaultModel != strings.TrimSpace(decoded.DefaultModel) {
		return Settings{}, errors.New("defaultModel must be a nonempty model identifier without surrounding whitespace")
	}
	if decoded.DefaultThinkingLevel != nil && !isThinkingLevelSupported(*decoded.DefaultThinkingLevel) {
		return Settings{}, fmt.Errorf("defaultThinkingLevel %q is not supported", *decoded.DefaultThinkingLevel)
	}
	activeUI := ""
	if decoded.ActiveUI != nil {
		activeUI = pluginid.Normalize(*decoded.ActiveUI)
		if activeUI == "" {
			return Settings{}, errors.New("activeUI must have a nonempty normalized plugin ID")
		}
	}
	return Settings{
		DefaultProvider:      decoded.DefaultProvider,
		DefaultModel:         decoded.DefaultModel,
		DefaultThinkingLevel: decoded.DefaultThinkingLevel,
		ActiveUI:             activeUI,
	}, nil
}

// isThinkingLevelSupported recognizes the complete OpenAI SDK v3.49 reasoning-effort enum.
func isThinkingLevelSupported(level ThinkingLevel) bool {
	switch level {
	case ThinkingLevelNone,
		ThinkingLevelMinimal,
		ThinkingLevelLow,
		ThinkingLevelMedium,
		ThinkingLevelHigh,
		ThinkingLevelXHigh,
		ThinkingLevelMax:
		return true
	default:
		return false
	}
}
