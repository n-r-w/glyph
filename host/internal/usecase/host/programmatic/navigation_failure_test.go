//go:build !integration

package programmatic

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestNavigationFailureRetainsOnlyCommittedResult verifies root commit metadata and cancellation priority at the public producer.
func TestNavigationFailureRetainsOnlyCommittedResult(t *testing.T) {
	t.Parallel()
	for _, committed := range []bool{false, true} {
		for _, mode := range []string{"error", "owning", "mixed", "independent"} {
			if mode == "independent" && !committed {
				continue
			}
			t.Run(map[bool]string{false: "precommit", true: "root commit"}[committed]+"/"+mode, func(t *testing.T) {
				t.Parallel()
				// Arrange an explicit optional navigation commit and a complete error source.
				control := gomock.NewController(t)
				navigator := NewMockNavigator(control)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				source := errors.New("navigation complete original failure")
				var failure error = source
				expected := operation.TerminalStateFailed
				if mode == "owning" {
					cancel()
					failure = context.Canceled
					expected = operation.TerminalStateCanceled
					if committed {
						expected = operation.TerminalStateCompleted
					}
				}
				if mode == "independent" {
					failure = context.Canceled
				}
				if mode == "mixed" {
					cancel()
					failure = errors.Join(source, context.Canceled)
				}
				commit := mo.None[NavigationCommit]()
				if committed {
					commit = mo.Some(NavigationCommit{
						DestinationID:  mo.None[string](),
						ActiveLeafID:   mo.None[string](),
						CreatedSummary: mo.None[session.Entry](),
						NextInput:      mo.Some("root input"),
					})
				}
				navigator.EXPECT().
					NavigateProgrammatic(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(NavigationCompletion{Committed: commit, Issues: nil}, failure)
				service := New(
					NewMockCoordinator(control),
					NewMockModelCatalog(control),
					testStateQuery(t, false),
					NewMockActiveSessions(control),
					navigator,
					NewMockGate(control),
					testRunOutput(t),
					nil,
					nil,
				)
				command := treeCommand("navigation-failure", controller.CommandNavigateSessionTree)
				command.TargetEntryID = mo.Some("root")
				prepared := &commandPrepared{service: service, command: command, release: func() {}, selection: nil}

				// Act through the prepared public producer.
				outcome := prepared.Run(ctx, operation.Reporter[controller.OperationProgress]{})

				// Assert only genuine commits yield a result and independent failure wins cancellation.
				require.Equal(t, expected, outcome.State())
				if mode == "error" || mode == "mixed" {
					require.ErrorIs(t, outcome.SourceError(), source)
				}
				result, available := outcome.Result()
				require.Equal(t, committed, available)
				if committed {
					navigation := result.TreeNavigation.MustGet()
					require.True(t, navigation.Committed.IsSome())
					require.True(t, navigation.Committed.MustGet().ActiveLeafID.IsNone())
					require.Equal(t, mo.Some("root input"), navigation.Committed.MustGet().NextInput)
					require.ErrorIs(t, outcome.SourceError(), failure)
				}
			})
		}
	}
}
