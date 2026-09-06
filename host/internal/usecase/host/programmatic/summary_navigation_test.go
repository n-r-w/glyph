//go:build !integration

package programmatic

import (
	"context"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestSummaryNavigationModesForwardEquivalentInternalRequests verifies both Programmatic Control summary modes reach
// Host unchanged.
func TestSummaryNavigationModesForwardEquivalentInternalRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		publicMode   controller.SummaryMode
		focus        mo.Option[string]
		internalMode controller.SummaryMode
		cancel       bool
		expected     controller.TreeNavigationStatus
	}{
		{
			name:         "built in committed",
			publicMode:   controller.SummaryModeSummarize,
			focus:        mo.None[string](),
			internalMode: controller.SummaryModeSummarize,
			cancel:       false,
			expected:     controller.TreeNavigationStatusCommitted,
		},
		{
			name:         "custom canceled",
			publicMode:   controller.SummaryModeSummarizeWithCustomPrompt,
			focus:        mo.Some("focus"),
			internalMode: controller.SummaryModeSummarizeWithCustomPrompt,
			cancel:       true,
			expected:     controller.TreeNavigationStatusCanceled,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange strict dependencies and capture the internal request before a canceled terminal result.
			mockController := gomock.NewController(t)
			coordinator := NewMockCoordinator(mockController)
			catalog := NewMockModelCatalog(mockController)
			control := NewMockActiveSessions(mockController)
			navigator := NewMockNavigator(mockController)
			gate := NewMockGate(mockController)
			navigator.EXPECT().NavigateProgrammatic(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(
					_ context.Context,
					request NavigationIntent,
					_ func(session.Tree) error,
				) (NavigationCompletion, error) {
					require.Equal(t, "target", request.TargetEntryID)
					require.Equal(t, test.internalMode, request.SummaryMode)
					require.Equal(t, test.focus, request.CustomFocus)
					if test.cancel {
						return NavigationCompletion{}, context.Canceled
					}
					return NavigationCompletion{Committed: mo.Some(
						NavigationCommit{
							DestinationID:  mo.None[string](),
							ActiveLeafID:   mo.None[string](),
							CreatedSummary: mo.None[session.Entry](),
							NextInput:      mo.None[string](),
						},
					), Issues: nil}, nil
				},
			)
			service := New(
				coordinator,
				catalog,
				testStateQuery(t, false),
				control, navigator,
				gate, testRunOutput(t),
			)
			command := treeCommand(test.name, controller.CommandNavigateSessionTree)
			command.TargetEntryID = mo.Some("target")
			command.SummaryMode = test.publicMode
			command.CustomFocus = test.focus

			// Act through Programmatic Control.
			response, operation, err := handleTreeCommandForTest(t, service, t.Context(), command)

			// Assert the exact summary intent produces the committed or canceled terminal result.
			require.NoError(t, err)
			require.Nil(t, operation)
			require.Equal(t, test.expected, response.TreeNavigation.MustGet().Status)
		})
	}
}
