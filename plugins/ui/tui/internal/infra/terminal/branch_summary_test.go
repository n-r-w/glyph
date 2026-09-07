//go:build !integration

package terminal

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// TestModelRendersBranchSummaryExpansion preserves collapsed and expanded summary output.
func TestModelRendersBranchSummaryExpansion(t *testing.T) {
	t.Parallel()
	// Arrange one completed summary in an immutable display snapshot.
	model := newRenderModel()
	model.snapshot.Body.Transcript = []presentation.Line{
		renderTextLine(presentation.LineBranchSummary, "## Goal\n\nContinue from the selected branch."),
	}
	// Act by rendering the two application-selected expansion states.
	collapsed := strings.Join(model.visibleBodyLines(0), "\n")
	model.snapshot.BranchSummariesExpanded = true
	expanded := strings.Join(model.visibleBodyLines(0), "\n")
	// Assert expansion retains the complete summary and collapse hides its body.
	assert.Contains(t, collapsed, "[branch]")
	assert.Contains(t, collapsed, "Branch summary (ctrl+o to expand)")
	assert.NotContains(t, collapsed, "Continue from the selected branch.")
	assert.Contains(t, expanded, "[branch]")
	assert.Contains(t, expanded, "Branch Summary")
	assert.Contains(t, expanded, "## Goal")
	assert.Contains(t, expanded, "Continue from the selected branch.")
}
