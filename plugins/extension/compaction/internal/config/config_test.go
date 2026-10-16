//go:build !integration

package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLoad checks configuration loading.
func TestLoad(t *testing.T) {
	// Arrange explicit empty settings for defaults.
	t.Setenv("GLYPH_COMPACTION_RETAINED_TOKENS", "")
	t.Setenv("GLYPH_COMPACTION_GENERATOR_ENABLED", "")
	// Act on defaults, overrides, and invalid values.
	defaults, err := Load()
	// Assert independent generator control and startup validation.
	require.NoError(t, err)
	require.Equal(t, int64(20000), defaults.RetainedTokens)
	require.True(t, defaults.GeneratorEnabled)
	t.Setenv("GLYPH_COMPACTION_RETAINED_TOKENS", "32")
	t.Setenv("GLYPH_COMPACTION_GENERATOR_ENABLED", "false")
	configured, err := Load()
	require.NoError(t, err)
	require.Equal(t, int64(32), configured.RetainedTokens)
	require.False(t, configured.GeneratorEnabled)
	for _, value := range []string{"broken", "0", "-1"} {
		t.Setenv("GLYPH_COMPACTION_RETAINED_TOKENS", value)
		_, err = Load()
		require.Error(t, err)
	}
	t.Setenv("GLYPH_COMPACTION_RETAINED_TOKENS", "32")
	t.Setenv("GLYPH_COMPACTION_GENERATOR_ENABLED", "broken")
	_, err = Load()
	require.Error(t, err)
}
