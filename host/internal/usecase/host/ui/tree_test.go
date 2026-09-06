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
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
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
	// Arrange SessionControl to return an empty current tree.

	controller := gomock.NewController(t)
	control := NewMockSessionControl(controller)
	tree, err := session.NewTree(nil, mo.None[string](), nil)
	require.NoError(t, err)
	control.EXPECT().Tree().Return(tree)
	service := treeOperationService(controller, control)

	// Act by running the prepared GetSessionTree command.
	frame, err := runPreparedCommand(t, service, newCommandForPreparedTest(controllerui.CommandGetSessionTree))

	// Assert the completed frame contains the same empty tree and absent active leaf.
	require.NoError(t, err)
	assert.Equal(t, controllerui.FrameSessionTree, frame.Kind)
	mapped := frame.SessionTree.MustGet()
	assert.Empty(t, mapped.Entries)
	assert.True(t, mapped.ActiveLeafID.IsNone())
}

// TestTreeNavigationPreservesSummaryModeAndCustomFocus verifies exact prepared-operation forwarding.
func TestTreeNavigationPreservesSummaryModeAndCustomFocus(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		publicMode   controllerui.SummaryMode
		internalMode sessionnavigation.SummaryMode
		focus        mo.Option[string]
	}{
		{
			name: "built in", publicMode: controllerui.SummaryModeSummarize,
			internalMode: sessionnavigation.SummaryModeSummarize, focus: mo.None[string](),
		},
		{
			name: "custom focus", publicMode: controllerui.SummaryModeSummarizeWithCustomPrompt,
			internalMode: sessionnavigation.SummaryModeSummarizeWithCustomPrompt, focus: mo.Some("focus"),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange a navigation command with the case-specific summary mode and custom focus.
			controller := gomock.NewController(t)
			control := NewMockSessionControl(controller)
			expectSessionMutationGate(control, 1)
			control.EXPECT().Navigate(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(
					_ context.Context,
					request sessionnavigation.Request,
					_ func(sessionnavigation.Progress) error,
				) (sessionnavigation.Result, error) {
					assert.Equal(t, "target", request.TargetEntryID)
					assert.Equal(t, test.internalMode, request.SummaryMode)
					assert.Equal(t, test.focus, request.CustomFocus)
					return sessionnavigation.Result{
						Canceled:       false,
						DestinationID:  mo.None[string](),
						ActiveLeafID:   mo.None[string](),
						CreatedSummary: mo.None[session.Entry](),
						NextInput:      mo.None[string](),
						Issues:         nil,
					}, nil
				},
			)
			command := newCommandForPreparedTest(controllerui.CommandNavigateSessionTree)
			command.TargetEntryID = mo.Some("target")
			command.SummaryMode = test.publicMode
			command.CustomFocus = test.focus

			// Act by running the prepared navigation command.
			frame, err := runPreparedCommand(t, treeOperationService(controller, control), command)

			// Assert SessionControl receives the exact options and returns one committed frame.
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
	control := NewMockSessionControl(controller)
	expectSessionMutationGate(control, 1)
	control.EXPECT().Navigate(gomock.Any(), gomock.Any(), gomock.Any()).Return(sessionnavigation.Result{
		Canceled:       true,
		DestinationID:  mo.None[string](),
		ActiveLeafID:   mo.None[string](),
		CreatedSummary: mo.None[session.Entry](),
		NextInput:      mo.None[string](),
		Issues: []sessionnavigation.OperationIssue{{
			Code: sessionnavigation.OperationIssueObserverError, ExtensionID: "extension",
			HandlerID: "handler", Message: "observer failed",
		}},
	}, nil)
	command := newCommandForPreparedTest(controllerui.CommandNavigateSessionTree)
	command.TargetEntryID = mo.Some("target")
	command.SummaryMode = controllerui.SummaryModeNoSummary

	// Act through the prepared Host UI operation.
	frame, err := runPreparedCommand(t, treeOperationService(controller, control), command)

	// Assert canceled data has no speculative committed state and keeps issues.
	require.NoError(t, err)
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
		{name: "model unavailable", sentinel: sessionnavigation.ErrModelUnavailable, code: controllerui.FailureCodeModelUnavailable},
		{name: "credential unavailable", sentinel: sessionnavigation.ErrCredentialUnavailable, code: controllerui.FailureCodeProviderAuth},
		{name: "model failed", sentinel: sessionnavigation.ErrModelFailed, code: controllerui.FailureCodeModelFailed},
		{name: "extension invalid result", sentinel: sessionnavigation.ErrExtensionInvalidResult, code: controllerui.FailureCodeExtensionInvalid},
		{name: "extension unavailable", sentinel: sessionnavigation.ErrExtensionUnavailable, code: controllerui.FailureCodeExtension},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one admitted navigation with a classified source failure.
			controller := gomock.NewController(t)
			control := NewMockSessionControl(controller)
			expectSessionMutationGate(control, 1)
			source := fmt.Errorf("navigate target: %w", test.sentinel)
			control.EXPECT().
				Navigate(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(sessionnavigation.Result{}, source)
			command := newCommandForPreparedTest(controllerui.CommandNavigateSessionTree)
			command.TargetEntryID = mo.Some("target")
			command.SummaryMode = controllerui.SummaryModeNoSummary
			service := treeOperationService(controller, control)
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
	control := NewMockSessionControl(controller)
	control.EXPECT().TryAcquire().Return(func() {}, false)
	service := treeOperationService(controller, control)
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
func treeOperationService(controller *gomock.Controller, control *MockSessionControl) *Session {
	service := NewSession(
		NewMockOutput(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller),
		NewMockModelCatalog(controller), control, func(context.Context) {},

		Initialization{},
	)
	service.setOperationAvailability(AvailabilityIdle)
	return service
}
