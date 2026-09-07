//go:build !integration

package ui

import (
	"context"
	"fmt"
	"testing"
	"time"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestSessionTreeMapsExtensionMessageContentAndVisibility verifies complete UI state retains both visibility values.
func TestSessionTreeMapsExtensionMessageContentAndVisibility(t *testing.T) {
	t.Parallel()

	// Arrange one complete tree with a hidden-client model-visible extension message.
	entry := session.Entry{
		ID: "message", ParentID: mo.None[string](), CreatedAt: time.Unix(1, 0).UTC(),
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		ExtensionMessage: mo.Some(session.ExtensionMessage{
			ExtensionID: "example", EntryType: "note", Text: "exact text", Visibility: session.ClientVisibilityHidden,
		}), BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
	tree, err := session.NewTree([]session.Entry{entry}, mo.Some("message"), nil)
	require.NoError(t, err)

	// Act by mapping complete tree state for the UI contract.
	mapped, err := mapSessionTree(tree)

	// Assert exact text and hidden visibility remain in complete state.
	require.NoError(t, err)
	require.Len(t, mapped.Entries, 1)
	require.Equal(t, controllerui.SessionTreeEntryExtensionMessage, mapped.Entries[0].Kind)
	require.Equal(t, "exact text", mapped.Entries[0].ExtensionMessage.MustGet().Text)
	require.Equal(t, session.ClientVisibilityHidden, mapped.Entries[0].ExtensionMessage.MustGet().Visibility)
}

// TestGetSessionTreeOperationReturnsCurrentTree verifies retained tree retrieval.
func TestGetSessionTreeOperationReturnsCurrentTree(t *testing.T) {
	t.Parallel()
	// Arrange ActiveSessions to return an empty current tree.

	controller := gomock.NewController(t)
	control := NewMockActiveSessions(controller)
	gate := NewMockGate(controller)
	tree, err := session.NewTree(nil, mo.None[string](), nil)
	require.NoError(t, err)
	control.EXPECT().Tree().Return(tree)
	service := treeOperationService(controller, control, gate, nil)

	// Act by running the prepared GetSessionTree command.
	frame, err := runPreparedCommand(t, service, newCommandForPreparedTest(controllerui.CommandGetSessionTree))

	// Assert the completed frame contains the same empty tree and absent active leaf.
	require.NoError(t, err)
	assert.Equal(t, controllerui.FrameSessionTree, frame.Kind)
	mapped := frame.SessionTree.MustGet()
	assert.Empty(t, mapped.Entries)
	assert.True(t, mapped.ActiveLeafID.IsNone())
}

// TestTreeNavigationPreservesSummaryModeAndCustomFocus verifies the admitted navigation intent.
func TestTreeNavigationPreservesSummaryModeAndCustomFocus(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		publicMode   controllerui.SummaryMode
		internalMode controllerui.SummaryMode
		focus        mo.Option[string]
	}{
		{
			name: "built in", publicMode: controllerui.SummaryModeSummarize,
			internalMode: controllerui.SummaryModeSummarize, focus: mo.None[string](),
		},
		{
			name: "custom focus", publicMode: controllerui.SummaryModeSummarizeWithCustomPrompt,
			internalMode: controllerui.SummaryModeSummarizeWithCustomPrompt, focus: mo.Some("focus"),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange a navigation command with the case-specific summary mode and custom focus.
			controller := gomock.NewController(t)
			control := NewMockActiveSessions(controller)
			navigator := NewMockNavigator(controller)
			gate := NewMockGate(controller)
			expectSessionMutationGate(gate, 1)
			navigator.EXPECT().NavigateUI(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(
					_ context.Context,
					request NavigationIntent,
					_ func(session.Tree) error,
				) (NavigationCompletion, error) {
					assert.Equal(t, "target", request.TargetEntryID)
					assert.Equal(t, test.internalMode, request.SummaryMode)
					assert.Equal(t, test.focus, request.CustomFocus)
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
			command := newCommandForPreparedTest(controllerui.CommandNavigateSessionTree)
			command.TargetEntryID = mo.Some("target")
			command.SummaryMode = test.publicMode
			command.CustomFocus = test.focus

			// Act by running the prepared navigation command.
			frame, err := runPreparedCommand(t, treeOperationService(controller, control, gate, navigator), command)

			// Assert ActiveSessions receives the exact options and returns one committed frame.
			require.NoError(t, err)
			assert.Equal(t, controllerui.FrameSessionTreeNavigation, frame.Kind)
			assert.Equal(t, controllerui.TreeNavigationStatusCommitted, frame.TreeNavigation.MustGet().Status)
		})
	}
}

// TestCanceledTreeNavigationReturnsStateFreeData verifies domain cancellation projection.
func TestCanceledTreeNavigationReturnsStateFreeData(t *testing.T) {
	t.Parallel()

	// Arrange one admitted navigation that cancels before commit with one issue.
	controller := gomock.NewController(t)
	control := NewMockActiveSessions(controller)
	navigator := NewMockNavigator(controller)
	gate := NewMockGate(controller)
	expectSessionMutationGate(gate, 1)
	navigator.EXPECT().
		NavigateUI(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(NavigationCompletion{Committed: mo.None[NavigationCommit](), Issues: []NavigationIssue{{
			Kind: NavigationObserverError, ExtensionID: "extension",
			HandlerID: "handler", Text: "observer failed",
		}}}, nil)
	command := newCommandForPreparedTest(controllerui.CommandNavigateSessionTree)
	command.TargetEntryID = mo.Some("target")
	command.SummaryMode = controllerui.SummaryModeNoSummary

	// Act through the prepared Host UI operation and retain its complete outcome.
	prepared, err := treeOperationService(controller, control, gate, navigator).Prepare(t.Context(), command)
	require.NoError(t, err)
	outcome := prepared.Run(t.Context(), operation.Reporter[controllerui.Frame]{})
	prepared.Release()
	frame, completed := outcome.Result()

	// Assert canceled navigation data remains Completed and declares its nonfatal issue before delivery.
	require.True(t, completed)
	require.NoError(t, outcome.Err())
	require.ErrorContains(t, outcome.SourceError(), "observer failed")
	result := frame.TreeNavigation.MustGet()
	assert.Equal(t, controllerui.TreeNavigationStatusCanceled, result.Status)
	assert.True(t, result.Committed.IsNone())
	require.Len(t, result.Issues, 1)
	assert.Equal(t, "observer failed", result.Issues[0].Message)
}

// TestTreeNavigationFailureCategoriesPreserveCauses verifies every closed navigation category.
func TestTreeNavigationFailureCategoriesPreserveCauses(t *testing.T) {
	t.Parallel()

	// Arrange the closed navigation failure categories accepted by the operation terminal.
	allowedCodes := []string{
		controllerui.FailureCodeSession,
		controllerui.FailureCodeModelUnavailable,
		controllerui.FailureCodeProviderAuth,
		controllerui.FailureCodeModelFailed,
		controllerui.FailureCodeExtensionInvalid,
		controllerui.FailureCodeExtension,
		controllerui.FailureCodePersistence,
		controllerui.FailureCodeInternal,
	}
	for _, test := range []struct {
		name     string
		sentinel error
		code     string
	}{
		{
			name: "model unavailable", sentinel: navigationFailure(t, controllerui.FailureCodeModelUnavailable),
			code: controllerui.FailureCodeModelUnavailable,
		},
		{
			name: "credential unavailable", sentinel: navigationFailure(t, controllerui.FailureCodeProviderAuth),
			code: controllerui.FailureCodeProviderAuth,
		},
		{
			name: "model failed", sentinel: navigationFailure(t, controllerui.FailureCodeModelFailed),
			code: controllerui.FailureCodeModelFailed,
		},
		{
			name: "extension invalid result", sentinel: navigationFailure(t, controllerui.FailureCodeExtensionInvalid),
			code: controllerui.FailureCodeExtensionInvalid,
		},
		{
			name: "extension unavailable", sentinel: navigationFailure(t, controllerui.FailureCodeExtension),
			code: controllerui.FailureCodeExtension,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one admitted navigation with a classified source failure.
			controller := gomock.NewController(t)
			control := NewMockActiveSessions(controller)
			navigator := NewMockNavigator(controller)
			gate := NewMockGate(controller)
			expectSessionMutationGate(gate, 1)
			source := fmt.Errorf("navigate target: %w", test.sentinel)
			navigator.EXPECT().
				NavigateUI(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(NavigationCompletion{}, source)
			command := newCommandForPreparedTest(controllerui.CommandNavigateSessionTree)
			command.TargetEntryID = mo.Some("target")
			command.SummaryMode = controllerui.SummaryModeNoSummary
			service := treeOperationService(controller, control, gate, navigator)
			prepared, err := service.Prepare(t.Context(), command)
			require.NoError(t, err)

			// Act through navigation failure classification.
			outcome := prepared.Run(t.Context(), operation.Reporter[controllerui.Frame]{})
			prepared.Release()

			// Assert one failed terminal with an allowed category, complete text, and source identity.
			assert.Equal(t, operation.TerminalStateFailed, outcome.State())
			assert.Contains(t, allowedCodes, outcome.Code())
			assert.Equal(t, test.code, outcome.Code())
			assert.ErrorIs(t, outcome.Err(), test.sentinel)
			assert.ErrorContains(t, outcome.Err(), source.Error())
		})
	}
}

// TestTreeMutationBusyRejectsBeforeAcceptance verifies tree mutations reserve the shared gate.
func TestTreeMutationBusyRejectsBeforeAcceptance(t *testing.T) {
	t.Parallel()
	// Arrange controller, control, and service for service.Prepare to verify tree mutations reserve the shared gate.

	controller := gomock.NewController(t)
	control := NewMockActiveSessions(controller)
	gate := NewMockGate(controller)
	gate.EXPECT().TryAcquire().Return(func() {}, false)
	service := treeOperationService(controller, control, gate, nil)
	command := newCommandForPreparedTest(controllerui.CommandSetEntryLabel)
	command.TargetEntryID = mo.Some("entry")
	command.EntryLabel = mo.Some("label")

	// Act by invoking service.Prepare to exercise tree mutations reserve the shared gate.
	_, err := service.Prepare(t.Context(), command)

	var rejection *PreparationError
	// Assert tree mutations reserve the shared gate.
	require.ErrorAs(t, err, &rejection)
	assert.Equal(t, controllerui.RejectionCodeBusy, rejection.PreparationCode())
}

// treeOperationService creates one session service for tree operation tests.
func treeOperationService(
	controller *gomock.Controller,
	control *MockActiveSessions,
	gate *MockGate,
	navigator Navigator,
) *Session {
	service := NewSession(
		NewMockOutput(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller),
		NewMockModelCatalog(controller), control, navigator, gate, nil,
	)
	service.setOperationAvailability(AvailabilityIdle)
	return service
}
