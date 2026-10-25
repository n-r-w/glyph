//go:build !integration

package ui

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestNavigationCommittedFailurePreservesTerminal verifies actual root metadata survives failure and owning cancellation.
func TestNavigationCommittedFailurePreservesTerminal(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"error", "owning", "mixed", "independent"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			// Arrange a real prepared UI producer with a committed root result and source error.
			control := gomock.NewController(t)
			active := NewMockActiveSessions(control)
			navigator := NewMockNavigator(control)
			gate := NewMockGate(control)
			expectSessionMutationGate(gate, 1)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			source := errors.New("complete postcommit navigation source")
			var failure error = source
			expected := operation.TerminalStateFailed
			if mode == "owning" {
				cancel()
				failure = context.Canceled
				expected = operation.TerminalStateCompleted
			}
			if mode == "independent" {
				failure = context.Canceled
			}
			if mode == "mixed" {
				cancel()
				failure = errors.Join(source, context.Canceled)
			}
			navigator.EXPECT().
				NavigateUI(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(NavigationCompletion{
					Committed: mo.Some(
						NavigationCommit{
							DestinationID:  mo.None[string](),
							ActiveLeafID:   mo.None[string](),
							CreatedSummary: mo.None[session.Entry](),
							NextInput:      mo.Some("root input"),
						},
					),
					Issues: nil,
				}, failure)
			command := newCommandForPreparedTest(controllerui.CommandNavigateSessionTree)
			command.TargetEntryID = mo.Some("root")
			command.SummaryMode = controllerui.SummaryModeNoSummary
			service := treeOperationService(control, active, gate, navigator)
			prepared, err := service.Prepare(t.Context(), command)
			require.NoError(t, err)

			// Act through the public UI terminal producer.
			outcome := prepared.Run(ctx, operation.Reporter[controllerui.Frame]{})
			prepared.Release()

			// Assert independent failure keeps priority and committed None leaf values remain a typed result.
			require.Equal(t, expected, outcome.State())
			require.ErrorIs(t, outcome.SourceError(), failure)
			frame, present := outcome.Result()
			require.True(t, present)
			result := frame.TreeNavigation.MustGet()
			require.True(t, result.Committed.IsSome())
			require.True(t, result.Committed.MustGet().DestinationID.IsNone())
			require.True(t, result.Committed.MustGet().ActiveLeafID.IsNone())
			require.Equal(t, mo.Some("root input"), result.Committed.MustGet().NextInput)
		})
	}
}
