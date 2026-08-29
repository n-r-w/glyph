package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/stretchr/testify/assert"
)

// TestEllipsizeUsesTerminalCellWidth verifies wide runes respect the cell-width limit.
func TestEllipsizeUsesTerminalCellWidth(t *testing.T) {
	t.Parallel()

	// Arrange wide Unicode text and a four-cell limit.
	text := "界界界"

	// Act by ellipsizing the text to the terminal width.
	result := ellipsize(text, 4)

	// Assert the result fits the cell limit and ends with an ellipsis.
	assert.LessOrEqual(t, ansi.StringWidth(result), 4)
	assert.True(t, strings.HasSuffix(result, "…"))
}
