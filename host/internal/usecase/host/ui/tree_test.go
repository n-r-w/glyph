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

// TestApplySessionTreeQuerySendsCompleteFrame verifies the UI query returns the complete projected tree.
func TestApplySessionTreeQuerySendsCompleteFrame(t *testing.T) {
	t.Parallel()

	// Arrange strict dependencies and one active-session tree snapshot.
	mockController := gomock.NewController(t)
	channel := NewMockChannel(mockController)
	runner := NewMockAgentRunner(mockController)
	authenticator := NewMockAuthenticator(mockController)
	catalog := NewMockModelCatalog(mockController)
	control := NewMockSessionControl(mockController)
	tree, err := session.NewTree(nil, mo.None[string](), nil)
	require.NoError(t, err)
	control.EXPECT().Tree().Return(tree)
	channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
		require.Equal(t, domainui.FrameSessionTree, frame.Kind)
		require.True(t, frame.SessionTree.IsSome())
		return nil
	})
	service := NewSession(channel, runner, authenticator, catalog, control, func(context.Context) {})

	// Act by applying the tree query.
	handled, err := service.applySessionCommand(t.Context(), uiTreeCommand(domainui.CommandGetSessionTree))

	// Assert the command is handled without starting another operation.
	require.NoError(t, err)
	require.True(t, handled)
}

// TestApplyCommittedNavigationSendsExactInput verifies committed navigation returns tree, transcript, and editable input.
func TestApplyCommittedNavigationSendsExactInput(t *testing.T) {
	t.Parallel()

	// Arrange strict dependencies and one committed user-target result.
	mockController := gomock.NewController(t)
	channel := NewMockChannel(mockController)
	runner := NewMockAgentRunner(mockController)
	authenticator := NewMockAuthenticator(mockController)
	catalog := NewMockModelCatalog(mockController)
	control := NewMockSessionControl(mockController)
	tree, err := session.NewTree(nil, mo.None[string](), nil)
	require.NoError(t, err)
	committed := sessionnavigation.Result{
		Canceled: false, Tree: tree, ActiveLeafID: mo.None[string](), ActiveBranch: nil,
		NextInput: mo.Some("exact input"), Issues: []sessionnavigation.OperationIssue{{
			Code: sessionnavigation.OperationIssueObserverError, ExtensionID: "extension",
			HandlerID: "observer", Message: "safe message",
		}},
	}
	control.EXPECT().Navigate(gomock.Any(), gomock.Any()).Return(committed, nil)
	channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
		require.Equal(t, domainui.FrameSessionTreeNavigation, frame.Kind)
		result := frame.TreeNavigation.MustGet()
		require.Equal(t, domainui.TreeNavigationStatusCommitted, result.Status)
		require.Equal(t, mo.Some("exact input"), result.Committed.MustGet().NextInput)
		require.True(t, result.Committed.MustGet().Tree.ActiveLeafID.IsNone())
		require.Equal(t, []domainui.OperationIssue{{
			Code: domainui.OperationIssueObserverError, ExtensionID: "extension",
			HandlerID: "observer", Message: "safe message",
		}}, result.Issues)
		return nil
	})
	service := NewSession(channel, runner, authenticator, catalog, control, func(context.Context) {})
	command := uiTreeCommand(domainui.CommandNavigateSessionTree)
	command.TargetEntryID = mo.Some("target")

	// Act by applying no-summary navigation.
	handled, err := service.applySessionCommand(t.Context(), command)

	// Assert committed state is sent as a typed result.
	require.NoError(t, err)
	require.True(t, handled)
}

// TestApplyCanceledNavigationSendsStateFreeResult verifies cancellation returns no speculative state.
func TestApplyCanceledNavigationSendsStateFreeResult(t *testing.T) {
	t.Parallel()

	// Arrange strict dependencies and a facade cancellation before commit.
	mockController := gomock.NewController(t)
	channel := NewMockChannel(mockController)
	runner := NewMockAgentRunner(mockController)
	authenticator := NewMockAuthenticator(mockController)
	catalog := NewMockModelCatalog(mockController)
	control := NewMockSessionControl(mockController)
	control.EXPECT().Navigate(gomock.Any(), gomock.Any()).Return(sessionnavigation.Result{
		Canceled: true, Tree: session.Tree{}, ActiveLeafID: mo.None[string](), ActiveBranch: nil,
		NextInput: mo.None[string](), Issues: []sessionnavigation.OperationIssue{{
			Code: sessionnavigation.OperationIssueHandlerError, ExtensionID: "extension",
			HandlerID: "handler", Message: "safe message",
		}},
	}, nil)
	channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
		require.Equal(t, domainui.FrameSessionTreeNavigation, frame.Kind)
		result := frame.TreeNavigation.MustGet()
		require.Equal(t, domainui.TreeNavigationStatusCanceled, result.Status)
		require.True(t, result.Committed.IsNone())
		require.Equal(t, []domainui.OperationIssue{{
			Code: domainui.OperationIssueHandlerError, ExtensionID: "extension",
			HandlerID: "handler", Message: "safe message",
		}}, result.Issues)
		return nil
	})
	service := NewSession(channel, runner, authenticator, catalog, control, func(context.Context) {})
	command := uiTreeCommand(domainui.CommandNavigateSessionTree)
	command.TargetEntryID = mo.Some("target")

	// Act by applying no-summary navigation.
	handled, err := service.applySessionCommand(t.Context(), command)

	// Assert cancellation is handled as a typed result.
	require.NoError(t, err)
	require.True(t, handled)
}

// TestApplyNavigationFailuresSendsClosedCodes verifies invalid, missing, busy, and persistence failures use typed UI frames.
func TestApplyNavigationFailuresSendsClosedCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		target        mo.Option[string]
		navigationErr error
		expected      domainui.TreeFailureCode
		summaryMode   domainui.SummaryMode
	}{
		{
			name:          "invalid",
			target:        mo.None[string](),
			navigationErr: nil,
			expected:      domainui.TreeFailureInvalidArgument,
			summaryMode:   domainui.SummaryModeNoSummary,
		},
		{
			name:          "missing",
			target:        mo.Some("target"),
			navigationErr: session.ErrEntryNotFound,
			expected:      domainui.TreeFailureNotFound,
			summaryMode:   domainui.SummaryModeNoSummary,
		},
		{
			name:          "busy",
			target:        mo.Some("target"),
			navigationErr: session.ErrBusy,
			expected:      domainui.TreeFailureBusy,
			summaryMode:   domainui.SummaryModeNoSummary,
		},
		{
			name:          "model unavailable",
			target:        mo.Some("target"),
			navigationErr: sessionnavigation.ErrModelUnavailable,
			expected:      domainui.TreeFailureModelUnavailable,
			summaryMode:   domainui.SummaryModeSummarize,
		},
		{
			name:          "credential unavailable",
			target:        mo.Some("target"),
			navigationErr: sessionnavigation.ErrCredentialUnavailable,
			expected:      domainui.TreeFailureCredentialUnavailable,
			summaryMode:   domainui.SummaryModeSummarize,
		},
		{
			name:          "model failed",
			target:        mo.Some("target"),
			navigationErr: sessionnavigation.ErrModelFailed,
			expected:      domainui.TreeFailureModelFailed,
			summaryMode:   domainui.SummaryModeSummarize,
		},
		{
			name:          "extension invalid result",
			target:        mo.Some("target"),
			navigationErr: sessionnavigation.ErrExtensionInvalidResult,
			expected:      domainui.TreeFailureExtensionInvalidResult,
			summaryMode:   domainui.SummaryModeSummarize,
		},
		{
			name:          "extension unavailable",
			target:        mo.Some("target"),
			navigationErr: sessionnavigation.ErrExtensionUnavailable,
			expected:      domainui.TreeFailureExtensionUnavailable,
			summaryMode:   domainui.SummaryModeSummarize,
		},
		{
			name:          "persistence",
			target:        mo.Some("target"),
			navigationErr: session.ErrPersistenceUnavailable,
			expected:      domainui.TreeFailurePersistenceUnavailable,
			summaryMode:   domainui.SummaryModeNoSummary,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange strict dependencies and one navigation terminal path.
			mockController := gomock.NewController(t)
			channel := NewMockChannel(mockController)
			runner := NewMockAgentRunner(mockController)
			authenticator := NewMockAuthenticator(mockController)
			catalog := NewMockModelCatalog(mockController)
			control := NewMockSessionControl(mockController)
			if _, present := test.target.Get(); present {
				control.EXPECT().
					Navigate(gomock.Any(), gomock.Any()).
					Return(sessionnavigation.Result{}, test.navigationErr)
			}
			channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
				require.Equal(t, domainui.FrameSessionTreeFailed, frame.Kind)
				require.Equal(t, test.expected, frame.TreeFailure.MustGet().Code)
				require.True(t, frame.SessionTree.IsNone())
				require.True(t, frame.TreeNavigation.IsNone())
				return nil
			})
			service := NewSession(channel, runner, authenticator, catalog, control, func(context.Context) {})
			command := uiTreeCommand(domainui.CommandNavigateSessionTree)
			command.TargetEntryID = test.target
			command.SummaryMode = test.summaryMode

			// Act by applying the failing navigation.
			handled, err := service.applySessionCommand(t.Context(), command)

			// Assert the typed failure is sent and the command is handled.
			require.NoError(t, err)
			require.True(t, handled)
		})
	}
}

// uiTreeCommand creates one fully initialized tree command.
func uiTreeCommand(kind domainui.CommandKind) domainui.Command {
	return domainui.Command{
		Kind: kind, Text: mo.None[string](), ProviderID: mo.None[string](), ModelID: mo.None[string](),
		ReasoningChoice: mo.None[domainui.ReasoningChoice](), SessionID: mo.None[string](),
		SessionName: mo.None[string](), TargetEntryID: mo.None[string](),
		SummaryMode: domainui.SummaryModeNoSummary, CustomFocus: mo.None[string](), EntryLabel: mo.None[string](),
	}
}
