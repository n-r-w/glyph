//go:build !integration

package ui

import (
	"context"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestSelectionOperationCommitsAndReturnsSelection verifies retained model-selection behavior.
func TestSelectionOperationCommitsAndReturnsSelection(t *testing.T) {
	t.Parallel()
	// Arrange controller, catalog, and descriptor for Prepared.Run to verify retained model-selection behavior.

	controller := gomock.NewController(t)
	catalog := NewMockModelCatalog(controller)
	descriptor := selectionDescriptor()
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog.EXPECT().Models().Return([]model.Descriptor{descriptor})
	catalog.EXPECT().ActiveSelection().Return(selection)
	catalog.EXPECT().SelectModel(gomock.Any(), model.ProviderID("provider"), model.ID("model")).Return(selection, nil)
	service := NewSession(
		NewMockChannel(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller), catalog, nil,
		func(context.Context) {},
	)
	service.setOperationAvailability(domainui.AvailabilityIdle)
	command := newCommandForPreparedTest(domainui.CommandSelectModel)
	command.ProviderID = mo.Some("provider")
	command.ModelID = mo.Some("model")

	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)
	// Act by invoking Prepared.Run to exercise retained model-selection behavior.
	outcome := prepared.Run(t.Context(), operation.Reporter[domainui.Frame]{})
	prepared.Release()

	// Assert retained model-selection behavior.
	assert.Equal(t, operation.TerminalStateCompleted, outcome.State())
	frame, ok := outcome.Result()
	require.True(t, ok)
	assert.Equal(t, domainui.FrameModelSelectionChanged, frame.Kind)
	assert.Equal(t, "provider", frame.ModelSelection.MustGet().ProviderID)
}

// TestSelectionReadinessAndActiveRunIndependence verifies retained selection admission states.
func TestSelectionReadinessAndActiveRunIndependence(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		availability domainui.Availability
		accepted     bool
	}{
		{name: "checking authentication", availability: domainui.AvailabilityCheckingAuthentication, accepted: false},
		{name: "authentication failed", availability: domainui.AvailabilityAuthenticationFailed, accepted: true},
		{name: "idle", availability: domainui.AvailabilityIdle, accepted: true},
		{name: "active run", availability: domainui.AvailabilityRunning, accepted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one valid selection at the selected readiness state.
			controller := gomock.NewController(t)
			catalog := NewMockModelCatalog(controller)
			selection := model.Selection{
				Provider:        "provider",
				Model:           "model",
				ReasoningChoice: model.ReasoningChoiceOff,
			}
			if test.accepted {
				catalog.EXPECT().Models().Return([]model.Descriptor{selectionDescriptor()})
				catalog.EXPECT().ActiveSelection().Return(selection)
				catalog.EXPECT().SelectModel(
					gomock.Any(), model.ProviderID("provider"), model.ID("model"),
				).Return(selection, nil)
			}
			service := NewSession(
				NewMockChannel(
					controller,
				),
				NewMockAgentRunner(controller),
				NewMockAuthenticator(controller),
				catalog,
				nil,
				func(context.Context) {},
			)
			service.setOperationAvailability(test.availability)
			command := newCommandForPreparedTest(domainui.CommandSelectModel)
			command.ProviderID = mo.Some("provider")
			command.ModelID = mo.Some("model")

			// Act through selection preparation and execution when admitted.
			prepared, err := service.Prepare(t.Context(), command)
			if !test.accepted {
				var rejection *PreparationError
				require.ErrorAs(t, err, &rejection)
				assert.Equal(t, rejectionCodeNotReady, rejection.Code())
				return
			}
			require.NoError(t, err)
			outcome := prepared.Run(t.Context(), operation.Reporter[domainui.Frame]{})
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
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog.EXPECT().Models().Return([]model.Descriptor{selectionDescriptor()})
	catalog.EXPECT().ActiveSelection().Return(selection)
	service := NewSession(
		NewMockChannel(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller), catalog, nil,
		func(context.Context) {},
	)
	service.setOperationAvailability(domainui.AvailabilityIdle)
	command := newCommandForPreparedTest(domainui.CommandSelectModel)
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
	assert.Equal(t, rejectionCodeBusy, rejection.Code())
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
