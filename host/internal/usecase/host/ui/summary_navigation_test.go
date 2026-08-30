package ui

import (
	"context"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

// TestSummaryNavigationModesForwardEquivalentInternalRequests verifies both UI Plugin Contract summary modes reach Host
// unchanged.
func TestSummaryNavigationModesForwardEquivalentInternalRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		publicMode   domainui.SummaryMode
		focus        mo.Option[string]
		internalMode sessionnavigation.SummaryMode
		cancel       bool
		expected     domainui.TreeNavigationStatus
	}{
		{
			name:         "built in committed",
			publicMode:   domainui.SummaryModeSummarize,
			focus:        mo.None[string](),
			internalMode: sessionnavigation.SummaryModeSummarize,
			cancel:       false,
			expected:     domainui.TreeNavigationStatusCommitted,
		},
		{
			name:         "custom canceled",
			publicMode:   domainui.SummaryModeSummarizeWithCustomPrompt,
			focus:        mo.Some("focus"),
			internalMode: sessionnavigation.SummaryModeSummarizeWithCustomPrompt,
			cancel:       true,
			expected:     domainui.TreeNavigationStatusCanceled,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange strict dependencies and capture the internal request before a canceled terminal result.
			mockController := gomock.NewController(t)
			channel := NewMockChannel(mockController)
			runner := NewMockAgentRunner(mockController)
			authenticator := NewMockAuthenticator(mockController)
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
			channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
				require.Equal(t, domainui.FrameSessionTreeNavigation, frame.Kind)
				require.Equal(t, test.expected, frame.TreeNavigation.MustGet().Status)
				return nil
			})
			service := NewSession(channel, runner, authenticator, catalog, control, func(context.Context) {})
			command := uiTreeCommand(domainui.CommandNavigateSessionTree)
			command.TargetEntryID = mo.Some("target")
			command.SummaryMode = test.publicMode
			command.CustomFocus = test.focus

			// Act through the UI Plugin Contract.
			handled, err := service.applySessionCommand(t.Context(), command)

			// Assert the committed or canceled terminal result survives equivalent request forwarding.
			require.NoError(t, err)
			require.True(t, handled)
		})
	}
}
