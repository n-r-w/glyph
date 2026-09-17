//go:build integration

package terminal

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// TestFrameworkEmitsCompleteHyperlink checks that real Bubble Tea output retains the full OSC 8 target.
func TestFrameworkEmitsCompleteHyperlink(t *testing.T) {
	t.Parallel()
	// Arrange a real renderer with only the end of a long authorization URL visible.
	const target = "https://example.test/oauth?redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fcallback&state=long-token"
	model := Model{
		snapshot: presentation.Snapshot{
			AuthenticationSelector: false,
			Body: presentation.DisplayBody{
				AuthorizationCode: mo.None[string](),
				Startup:           nil,
				Transcript:        nil,
				ActiveModel:       nil,
				ActiveToolCalls:   nil,
				Models:            nil,
				Sessions:          nil,
				Availability:      mo.Some(presentation.AvailabilityAuthenticating),
				AuthorizationURL:  mo.Some(target),
				ModelSelection:    mo.None[presentation.ModelSelection](),
			},
			Input: nil, Cursor: 0, SelectorOpen: false, SessionSelector: false, ResumeStatus: "", SelectorRow: 0,
			ReasoningExpanded: false, BranchSummariesExpanded: false, ReasoningSelectionVisible: true,
			TreePanel: mo.None[presentation.TreeView](), TreeMode: presentation.TreeClosed,
			TreeSummaryIndex: 0, TreeInput: nil, TreeCursor: 0, TreeStatus: "",
		},
		input: nil, plugin: nil, width: 40, height: fixedViewLineCount + 1, failure: nil, notificationEnded: false,
	}
	output := newNotifyingWriter()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	program := tea.NewProgram(&model, tea.WithInput(nil), tea.WithOutput(output), tea.WithContext(ctx),
		tea.WithWindowSize(model.width, model.height),
		tea.WithEnvironment([]string{"TERM=xterm-256color", "CLICOLOR_FORCE=1"}),
		tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()
	// Act by waiting for a complete frame; the pipe uses an explicit terminal color profile.
	for !strings.Contains(output.String(), "Request:") {
		select {
		case <-output.written:
		case <-ctx.Done():
			t.Fatal("renderer did not emit a frame before the test deadline")
		}
	}
	program.Quit()
	// Assert emitted bytes contain OSC metadata, not only the wrapped visible URL suffix.
	require.NoError(t, <-done)
	rendered := output.String()
	require.Contains(t, rendered, hyperlinkPrefix)
	require.Contains(t, rendered, ";"+target)
	require.Contains(t, rendered, "Request:")
}
