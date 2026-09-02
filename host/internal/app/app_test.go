package app

//go:generate go tool mockgen -build_constraint=integration -destination=http_roundtripper_mock_test.go -package=app -mock_names=RoundTripper=MockHTTPRoundTripper net/http RoundTripper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	appUIHelperEnvironment         = "GLYPH_APP_UI_HELPER"
	appUITraceEnvironment          = "GLYPH_APP_UI_TRACE"
	appUITerminalEnvironment       = "GLYPH_APP_UI_TERMINAL"
	appUIBehaviorEnvironment       = "GLYPH_APP_UI_BEHAVIOR"
	appUICostStateEnvironment      = "GLYPH_APP_UI_COST_STATE"
	appUIPTYInnerEnvironment       = "GLYPH_APP_PTY_INNER"
	appUIRuntimeDataEnvironment    = "GLYPH_APP_UI_RUNTIME_DATA"
	appUIRuntimeEffectEnvironment  = "GLYPH_APP_UI_RUNTIME_EFFECT"
	appUIRuntimeReleaseEnvironment = "GLYPH_APP_UI_RUNTIME_RELEASE"
)

// repoRoot resolves the repository from the test package working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	require.NoError(t, err)
	return filepath.Clean(filepath.Join(directory, "..", "..", ".."))
}
