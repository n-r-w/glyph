// Package config loads extension-local startup configuration.
package config

import (
	"errors"
	"fmt"

	"github.com/caarlos0/env/v11"
)

// Config contains process-startup compaction settings.
type Config struct {
	// RetainedTokens is the recent-history target.
	RetainedTokens int64 `env:"GLYPH_COMPACTION_RETAINED_TOKENS" envDefault:"20000"`
	// GeneratorEnabled indicates whether to register the default generator.
	GeneratorEnabled bool `env:"GLYPH_COMPACTION_GENERATOR_ENABLED" envDefault:"true"`
}

// Load supplies the extension's startup configuration.
func Load() (Config, error) {
	value, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("load compaction configuration: %w", err)
	}
	if value.RetainedTokens <= 0 {
		return Config{}, errors.New("GLYPH_COMPACTION_RETAINED_TOKENS must be positive")
	}
	return value, nil
}
