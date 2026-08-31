//go:build !integration

package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestModelTogglesBranchSummaries verifies collapsed summaries expand through the current fixed key.
func TestModelTogglesBranchSummaries(t *testing.T) {
	t.Parallel()

	// Arrange one restored branch summary in an idle transcript.
	model := newTestModel(t, presentationdomain.AvailabilityIdle, nil)
	model.state.Transcript = []presentationdomain.Line{{
		Kind:     presentationdomain.LineBranchSummary,
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Text:     mo.Some("## Goal\n\nContinue from the selected branch."),
		Contents: mo.None[[]presentationdomain.Content](),
	}}
	collapsed := strings.Join(model.visibleBodyLines(0), "\n")
	assert.Contains(t, collapsed, "[branch]")
	assert.Contains(t, collapsed, "Branch summary (ctrl+o to expand)")
	assert.NotContains(t, collapsed, "Continue from the selected branch.")

	model.emitting = true
	// Act by applying the fixed expansion key during an active command.
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code:        'o',
		Mod:         tea.ModCtrl,
		Text:        "",
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))

	// Assert the complete summary is visible and the same key restores the collapsed view.
	expanded := strings.Join(model.visibleBodyLines(0), "\n")
	assert.Contains(t, expanded, "[branch]")
	assert.Contains(t, expanded, "Branch Summary")
	assert.Contains(t, expanded, "## Goal")
	assert.Contains(t, expanded, "Continue from the selected branch.")
	assert.True(t, model.emitting)
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code:        'o',
		Mod:         tea.ModCtrl,
		Text:        "",
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))
	assert.Equal(t, collapsed, strings.Join(model.visibleBodyLines(0), "\n"))
}
