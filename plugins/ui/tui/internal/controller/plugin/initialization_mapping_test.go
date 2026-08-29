package plugin

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/proto"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestMapInitializationRequiresScalarPresence verifies initialization keeps its handwritten required fields.
func TestMapInitializationRequiresScalarPresence(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*uiv1.Initialization){
		"selected UI ID": func(initialization *uiv1.Initialization) { initialization.ClearSelectedUiId() },
		"availability":   func(initialization *uiv1.Initialization) { initialization.ClearAvailability() },
		"startup severity": func(initialization *uiv1.Initialization) {
			initialization.GetStartupContent()[0].ClearSeverity()
		},
		"startup text": func(initialization *uiv1.Initialization) {
			initialization.GetStartupContent()[0].ClearText()
		},
		"extension plugin ID": func(initialization *uiv1.Initialization) {
			initialization.GetExtensions()[0].ClearPluginId()
		},
		"extension path": func(initialization *uiv1.Initialization) {
			initialization.GetExtensions()[0].ClearPath()
		},
		"configured provider ID": func(initialization *uiv1.Initialization) {
			initialization.GetModels()[0].ClearProviderId()
		},
		"configured model ID": func(initialization *uiv1.Initialization) {
			initialization.GetModels()[0].ClearModelId()
		},
		"reasoning supported": func(initialization *uiv1.Initialization) {
			initialization.GetModels()[0].GetReasoning().ClearSupported()
		},
		"reasoning default choice": func(initialization *uiv1.Initialization) {
			initialization.GetModels()[0].GetReasoning().ClearDefaultChoice()
		},
		"selection provider ID": func(initialization *uiv1.Initialization) {
			initialization.GetModelSelection().ClearProviderId()
		},
		"selection model ID": func(initialization *uiv1.Initialization) {
			initialization.GetModelSelection().ClearModelId()
		},
		"selection reasoning choice": func(initialization *uiv1.Initialization) {
			initialization.GetModelSelection().ClearReasoningChoice()
		},
	}

	for name, clear := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			request := proto.Clone(initializationRequest()).(*uiv1.OpenRequest)
			initialization := request.GetInitialization()
			clear(initialization)
			_, err := mapInitialization(initialization)
			require.Error(t, err)
		})
	}
}

// TestMapInitializationPreservesPresentZeroScalars verifies valid zero values stay concrete.
func TestMapInitializationPreservesPresentZeroScalars(t *testing.T) {
	t.Parallel()

	initialization := proto.Clone(initializationRequest().GetInitialization()).(*uiv1.Initialization)
	initialization.SetSelectedUiId("")
	initialization.GetStartupContent()[0].SetText("")
	initialization.GetExtensions()[0].SetPluginId("")
	initialization.GetExtensions()[0].SetPath("")
	initialization.GetModels()[0].SetProviderId("")
	initialization.GetModels()[0].SetModelId("")
	initialization.GetModels()[0].GetReasoning().SetSupported(false)

	event, err := mapInitialization(initialization)
	require.NoError(t, err)
	assert.Equal(t, mo.Some(""), event.Startup[0].Text)
	assert.Empty(t, event.Extensions[0].ID)
	assert.Empty(t, event.Extensions[0].Path)
	assert.Empty(t, event.Models[0].ProviderID)
	assert.Empty(t, event.Models[0].ModelID)
	assert.False(t, event.Models[0].Reasoning.Supported)
}

// TestReasoningMappingsCoverEveryValue verifies public and presentation enums stay exact.
func TestReasoningMappingsCoverEveryValue(t *testing.T) {
	t.Parallel()

	values := []struct {
		public       uiv1.ReasoningChoice
		presentation presentationdomain.ReasoningChoice
	}{
		{uiv1.ReasoningChoice_REASONING_CHOICE_OFF, presentationdomain.ReasoningChoiceOff},
		{uiv1.ReasoningChoice_REASONING_CHOICE_MINIMAL, presentationdomain.ReasoningChoiceMinimal},
		{uiv1.ReasoningChoice_REASONING_CHOICE_LOW, presentationdomain.ReasoningChoiceLow},
		{uiv1.ReasoningChoice_REASONING_CHOICE_MEDIUM, presentationdomain.ReasoningChoiceMedium},
		{uiv1.ReasoningChoice_REASONING_CHOICE_HIGH, presentationdomain.ReasoningChoiceHigh},
		{uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH, presentationdomain.ReasoningChoiceXHigh},
		{uiv1.ReasoningChoice_REASONING_CHOICE_MAX, presentationdomain.ReasoningChoiceMax},
	}
	for _, value := range values {
		mapped, err := mapReasoningChoice(value.public)
		require.NoError(t, err)
		assert.Equal(t, value.presentation, mapped)
		assert.Equal(t, value.public, mapReasoningChoiceToProto(value.presentation))
	}
	_, err := mapReasoningChoice(uiv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED)
	require.Error(t, err)
	_, err = mapReasoningChoice(uiv1.ReasoningChoice(99))
	require.Error(t, err)
}
