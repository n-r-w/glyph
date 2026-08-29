package app

import (
	"os"
	"os/exec"
	"path/filepath"

	"testing"

	"github.com/samber/lo"

	"github.com/stretchr/testify/require"

	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
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

// semanticToolResultContents keeps typed result blocks stable in the shared lifecycle fixture.
func semanticToolResultContents(contents []*uipb.ToolResultContent) []map[string]any {
	return lo.FilterMap(contents, func(content *uipb.ToolResultContent, _ int) (map[string]any, bool) {
		switch content.WhichContent() {
		case uipb.ToolResultContent_Text_case:
			return map[string]any{"text": content.GetText()}, true
		case uipb.ToolResultContent_Image_case:
			image := content.GetImage()
			return map[string]any{"image": map[string]any{
				"media_type": image.GetMediaType(), "data": image.GetData(),
			}}, true
		case uipb.ToolResultContent_Content_not_set_case:
			return nil, false
		}
		return nil, false
	})
}

// buildToolsExecutable compiles the real tools command into a test-owned temporary directory.
func buildToolsExecutable(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	output := filepath.Join(t.TempDir(), "glyph-tools")
	command := exec.CommandContext(t.Context(), "go", "build", "-o", output, "./plugins/extension/tools/cmd/glyph-tools")
	command.Dir = root
	outputBytes, err := command.CombinedOutput()
	require.NoError(t, err, string(outputBytes))
	return filepath.Dir(output)
}

// repoRoot resolves the repository from the test package working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	require.NoError(t, err)
	return filepath.Clean(filepath.Join(directory, "..", "..", ".."))
}
