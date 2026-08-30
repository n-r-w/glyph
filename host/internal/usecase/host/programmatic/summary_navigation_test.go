package programmatic

import (
	"context"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

// TestSummaryNavigationModesForwardEquivalentInternalRequests verifies both Programmatic Control summary modes reach
// Host unchanged.
func TestSummaryNavigationModesForwardEquivalentInternalRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		publicMode   controller.SummaryMode
		focus        mo.Option[string]
		internalMode sessionnavigation.SummaryMode
		cancel       bool
		expected     controller.TreeNavigationStatus
	}{
		{
			name:         "built in committed",
			publicMode:   controller.SummaryModeSummarize,
			focus:        mo.None[string](),
			internalMode: sessionnavigation.SummaryModeSummarize,
			cancel:       false,
			expected:     controller.TreeNavigationStatusCommitted,
		},
		{
			name:         "custom canceled",
			publicMode:   controller.SummaryModeSummarizeWithCustomPrompt,
			focus:        mo.Some("focus"),
			internalMode: sessionnavigation.SummaryModeSummarizeWithCustomPrompt,
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
			control := NewMockSessionControl(mockController)
			control.EXPECT().Navigate(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, request sessionnavigation.Request) (sessionnavigation.Result, error) {
					require.Equal(t, "target", request.TargetEntryID)
					require.Equal(t, test.internalMode, request.SummaryMode)
					require.Equal(t, test.focus, request.CustomFocus)
					if test.cancel {
						return sessionnavigation.Result{}, context.Canceled
					}
					tree, err := session.NewTree(nil, mo.None[string](), nil)
					require.NoError(t, err)
					return sessionnavigation.Result{
						Canceled: false, Tree: tree, ActiveLeafID: mo.None[string](), ActiveBranch: nil,
						NextInput: mo.None[string](), Issues: nil,
					}, nil
				},
			)
			service := New(coordinator, catalog, idleStateSnapshot, emptyHistorySnapshot, control, NewDelivery())
			command := treeCommand(test.name, controller.CommandNavigateSessionTree)
			command.TargetEntryID = mo.Some("target")
			command.SummaryMode = test.publicMode
			command.CustomFocus = test.focus

			// Act through Programmatic Control.
			response, operation, err := service.Handle(t.Context(), command)

			// Assert the committed or canceled terminal result survives equivalent request forwarding.
			require.NoError(t, err)
			require.Nil(t, operation)
			require.Equal(t, test.expected, response.TreeNavigation.MustGet().Status)
		})
	}
}
