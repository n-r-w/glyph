//go:build !integration

package ui

import (
	"context"
	"errors"
	"testing"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestSelectionOperationCommitsAndReturnsSelection verifies retained model-selection behavior.
func TestSelectionOperationCommitsAndReturnsSelection(t *testing.T) {
	t.Parallel()
	// Arrange controller, catalog, and descriptor for Prepared.Run to verify retained model-selection behavior.

	controller := gomock.NewController(t)
	catalog := NewMockModelCatalog(controller)
	selectionOwner := NewMockModelSelection(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	expectUISelection(t, controller, selectionOwner, ModelSelectionCommand{
		Kind: ModelSelectionCommandModel, Provider: "provider", Model: "model", ReasoningChoice: "",
	}, ModelSelectionResult{Selection: selection, Committed: true, Issues: nil, Source: nil})
	service := NewSession(
		NewMockOutput(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller), catalog, nil, nil,
		nil, nil, selectionOwner, nil,
	)
	service.setOperationAvailability(AvailabilityIdle)
	command := newCommandForPreparedTest(controllerui.CommandSelectModel)
	command.ProviderID = mo.Some("provider")
	command.ModelID = mo.Some("model")

	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)
	// Act by invoking Prepared.Run to exercise retained model-selection behavior.
	outcome := prepared.Run(t.Context(), operation.Reporter[controllerui.Frame]{})
	prepared.Release()

	// Assert retained model-selection behavior.
	assert.Equal(t, operation.TerminalStateCompleted, outcome.State())
	frame, ok := outcome.Result()
	require.True(t, ok)
	assert.Equal(t, controllerui.FrameModelSelectionChanged, frame.Kind)
	assert.Equal(t, model.ProviderID("provider"), frame.ModelSelection.MustGet().Provider)
}

// TestSelectionPublicationFailureCompletesWithCommittedState verifies post-commit diagnostics do not report failure.
func TestSelectionPublicationFailureCompletesWithCommittedState(t *testing.T) {
	t.Parallel()
	// Arrange a shared operation that committed before full delivery failed.
	controller := gomock.NewController(t)
	selectionOwner := NewMockModelSelection(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh}
	deliveryErr := errors.New("selection delivery failed")
	expectUISelection(t, controller, selectionOwner, ModelSelectionCommand{
		Kind: ModelSelectionCommandModel, Provider: selection.Provider, Model: selection.Model, ReasoningChoice: "",
	}, ModelSelectionResult{
		Selection: selection, Committed: true,
		Issues: []ModelSelectionIssue{{
			Kind: ModelSelectionIssueDeliveryFailed, ExtensionID: "", HandlerID: "", Message: deliveryErr.Error(),
		}},
		Source: deliveryErr,
	})
	service := NewSession(
		NewMockOutput(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller),
		NewMockModelCatalog(controller), nil, nil, nil, nil, selectionOwner, nil,
	)
	service.setOperationAvailability(AvailabilityIdle)
	command := newCommandForPreparedTest(controllerui.CommandSelectModel)
	command.ProviderID = mo.Some(string(selection.Provider))
	command.ModelID = mo.Some(string(selection.Model))
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)
	defer prepared.Release()

	// Act by executing the admitted selection.
	outcome := prepared.Run(t.Context(), operation.Reporter[controllerui.Frame]{})

	// Assert terminal completion retains both committed state and complete diagnostics.
	assert.Equal(t, operation.TerminalStateCompleted, outcome.State())
	frame, present := outcome.Result()
	require.True(t, present)
	assert.Equal(t, selection, frame.ModelSelection.MustGet())
	require.Len(t, frame.SelectionIssues, 1)
	assert.Equal(t, controllerui.OperationIssueDeliveryFailed, frame.SelectionIssues[0].Code)
	assert.Equal(t, deliveryErr.Error(), frame.SelectionIssues[0].Message)
	assert.ErrorIs(t, outcome.SourceError(), deliveryErr)
}

// TestSelectionHandlerDiagnosticsRemainOrdered verifies explicit result diagnostics reach the UI completion.
func TestSelectionHandlerDiagnosticsRemainOrdered(t *testing.T) {
	t.Parallel()
	// Arrange one committed result with ordered handler and delivery diagnostics.
	controller := gomock.NewController(t)
	selectionOwner := NewMockModelSelection(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh}
	source := errors.New("ordinary failed\ninvalid action\nselection event delivery failed")
	resultIssues := []ModelSelectionIssue{
		{
			Kind:        ModelSelectionIssueHandlerError,
			ExtensionID: "first",
			HandlerID:   "ordinary",
			Message:     "ordinary failed",
		},
		{
			Kind:        ModelSelectionIssueInvalidHandlerAction,
			ExtensionID: "second",
			HandlerID:   "invalid",
			Message:     "invalid action",
		},
		{
			Kind:        ModelSelectionIssueDeliveryFailed,
			ExtensionID: "",
			HandlerID:   "",
			Message:     "selection event delivery failed",
		},
	}
	expectedIssues := projectModelSelectionIssues(resultIssues)
	expectUISelection(t, controller, selectionOwner, ModelSelectionCommand{
		Kind: ModelSelectionCommandModel, Provider: selection.Provider, Model: selection.Model, ReasoningChoice: "",
	}, ModelSelectionResult{Selection: selection, Committed: true, Issues: resultIssues, Source: source})
	service := NewSession(
		NewMockOutput(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller),
		NewMockModelCatalog(controller), nil, nil, nil, nil, selectionOwner, nil,
	)
	service.setOperationAvailability(AvailabilityIdle)
	command := newCommandForPreparedTest(controllerui.CommandSelectModel)
	command.ProviderID = mo.Some(string(selection.Provider))
	command.ModelID = mo.Some(string(selection.Model))
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)
	defer prepared.Release()

	// Act by executing the admitted selection.
	outcome := prepared.Run(t.Context(), operation.Reporter[controllerui.Frame]{})

	// Assert the UI result uses the explicit ordered diagnostic list without decoding an error protocol.
	frame, present := outcome.Result()
	require.True(t, present)
	assert.Equal(t, expectedIssues, frame.SelectionIssues)
	assert.ErrorIs(t, outcome.SourceError(), source)
}

// TestCommittedSelectionCancellationCompletes verifies cancellation after commit remains a completed result.
func TestCommittedSelectionCancellationCompletes(t *testing.T) {
	t.Parallel()
	// Arrange a shared operation whose delivery wait is canceled after commit.
	controller := gomock.NewController(t)
	selectionOwner := NewMockModelSelection(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh}
	expectUISelection(t, controller, selectionOwner, ModelSelectionCommand{
		Kind: ModelSelectionCommandModel, Provider: selection.Provider, Model: selection.Model, ReasoningChoice: "",
	}, ModelSelectionResult{Selection: selection, Committed: true, Issues: nil, Source: context.Canceled})
	service := NewSession(
		NewMockOutput(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller),
		NewMockModelCatalog(controller), nil, nil, nil, nil, selectionOwner, nil,
	)
	service.setOperationAvailability(AvailabilityIdle)
	command := newCommandForPreparedTest(controllerui.CommandSelectModel)
	command.ProviderID = mo.Some(string(selection.Provider))
	command.ModelID = mo.Some(string(selection.Model))
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)
	defer prepared.Release()

	// Act by executing the admitted selection.
	outcome := prepared.Run(t.Context(), operation.Reporter[controllerui.Frame]{})

	// Assert cancellation is diagnostic because state already committed.
	assert.Equal(t, operation.TerminalStateCompleted, outcome.State())
	frame, present := outcome.Result()
	require.True(t, present)
	assert.Equal(t, selection, frame.ModelSelection.MustGet())
	assert.ErrorIs(t, outcome.SourceError(), context.Canceled)
}

// TestSelectionReadinessAndActiveRunIndependence verifies retained selection admission states.
func TestSelectionReadinessAndActiveRunIndependence(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		availability Availability
		accepted     bool
	}{
		{name: "checking authentication", availability: AvailabilityCheckingAuthentication, accepted: false},
		{name: "authentication failed", availability: AvailabilityAuthenticationFailed, accepted: true},
		{name: "idle", availability: AvailabilityIdle, accepted: true},
		{name: "active run", availability: AvailabilityRunning, accepted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one valid selection at the selected readiness state.
			controller := gomock.NewController(t)
			catalog := NewMockModelCatalog(controller)
			selectionOwner := NewMockModelSelection(controller)
			selection := model.Selection{
				Provider:        "provider",
				Model:           "model",
				ReasoningChoice: model.ReasoningChoiceOff,
			}
			if test.accepted {
				expectUISelection(t, controller, selectionOwner, ModelSelectionCommand{
					Kind: ModelSelectionCommandModel, Provider: "provider", Model: "model", ReasoningChoice: "",
				}, ModelSelectionResult{Selection: selection, Committed: true, Issues: nil, Source: nil})
			}
			service := NewSession(
				NewMockOutput(
					controller,
				),
				NewMockAgentRunner(controller),
				NewMockAuthenticator(controller),
				catalog,
				nil, nil,
				nil, nil, selectionOwner, nil,
			)
			service.setOperationAvailability(test.availability)
			command := newCommandForPreparedTest(controllerui.CommandSelectModel)
			command.ProviderID = mo.Some("provider")
			command.ModelID = mo.Some("model")

			// Act through selection preparation and execution when admitted.
			prepared, err := service.Prepare(t.Context(), command)
			if !test.accepted {
				var rejection *PreparationError
				require.ErrorAs(t, err, &rejection)
				assert.Equal(t, controllerui.RejectionCodeNotReady, rejection.PreparationCode())
				return
			}
			require.NoError(t, err)
			outcome := prepared.Run(t.Context(), operation.Reporter[controllerui.Frame]{})
			prepared.Release()

			// Assert selection completion does not change active-run availability.
			assert.Equal(t, operation.TerminalStateCompleted, outcome.State())
			assert.Equal(t, test.availability, service.operationAvailabilitySnapshot())
		})
	}
}

// TestSelectionPreparationRejectsConcurrentCommit verifies one selection reservation at a time.
func TestSelectionPreparationRejectsConcurrentCommit(t *testing.T) {
	t.Parallel()
	// Arrange controller, catalog, and selection for service.Prepare to verify one selection reservation at a time.

	controller := gomock.NewController(t)
	catalog := NewMockModelCatalog(controller)
	selectionOwner := NewMockModelSelection(controller)
	selectionCommand := ModelSelectionCommand{
		Kind: ModelSelectionCommandModel, Provider: "provider", Model: "model", ReasoningChoice: "",
	}
	firstSelection := NewMockPreparedModelSelection(controller)
	selectionOwner.EXPECT().PrepareUISelection(selectionCommand).Return(firstSelection, nil)
	firstSelection.EXPECT().Release()
	selectionOwner.EXPECT().PrepareUISelection(selectionCommand).Return(nil, selectionCodeTestError("busy"))
	service := NewSession(
		NewMockOutput(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller), catalog, nil, nil,
		nil, nil, selectionOwner, nil,
	)
	service.setOperationAvailability(AvailabilityIdle)
	command := newCommandForPreparedTest(controllerui.CommandSelectModel)
	command.ProviderID = mo.Some("provider")
	command.ModelID = mo.Some("model")
	// Act by invoking service.Prepare to exercise one selection reservation at a time.
	first, err := service.Prepare(t.Context(), command)
	// Assert one selection reservation at a time.
	require.NoError(t, err)
	defer first.Release()

	_, err = service.Prepare(t.Context(), command)

	var rejection *PreparationError
	require.ErrorAs(t, err, &rejection)
	assert.Equal(t, controllerui.RejectionCodeBusy, rejection.PreparationCode())
}

// expectUISelection configures generated mocks for one explicit consumer-owned result.
func expectUISelection(
	t *testing.T,
	controller *gomock.Controller,
	owner *MockModelSelection,
	command ModelSelectionCommand,
	result ModelSelectionResult,
) {
	t.Helper()
	prepared := NewMockPreparedModelSelection(controller)
	owner.EXPECT().PrepareUISelection(command).Return(prepared, nil)
	prepared.EXPECT().Run(gomock.Any()).Return(result)
	prepared.EXPECT().Release()
}

// selectionDescriptor creates one complete configured model for selection validation.
func selectionDescriptor() model.Descriptor {
	return model.Descriptor{
		Provider: "provider", Model: "model", Input: []model.InputModality{model.InputModalityText},
		ContextWindow: 1, MaxTokens: 1,
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: false, Choices: nil, Default: model.ReasoningChoiceOff,
		},
		ToolCapabilities: model.ToolCapabilities{}, Pricing: mo.None[model.Pricing](),
	}
}
