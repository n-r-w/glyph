//go:build !integration

package terminal

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// TestModelRendersCompactionSummary verifies restored compaction markers remain identifiable.
func TestModelRendersCompactionSummary(t *testing.T) {
	t.Parallel()

	// Arrange one restored compaction summary in the immutable display snapshot.
	model := newRenderModel()
	model.snapshot.Body.Transcript = []presentation.Line{
		renderTextLine(presentation.LineCompaction, "retained context summary"),
	}

	// Act by rendering the visible transcript.
	rendered := strings.Join(model.visibleBodyLines(0), "\n")

	// Assert the standard TUI identifies the compaction and preserves its summary.
	require.Contains(t, rendered, "[compact]")
	require.Contains(t, rendered, "retained context summary")
}
