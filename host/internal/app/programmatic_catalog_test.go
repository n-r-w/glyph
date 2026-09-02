//go:build integration

package app

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestModelCommandsUseSharedCatalog proves the Unix-socket query exposes exact model capabilities.
func (testSuite *ProgrammaticAppSuite) TestModelCommandsUseSharedCatalog() {
	// Arrange a programmatic fixture backed by text-only and text-and-image models.
	t := testSuite.T()
	fixture := startProgrammaticFixture(t, testPaths(t, programmaticModelCatalogSettings()))

	// Act by querying models and selecting the model and reasoning choice.
	models := completeProgrammaticRequest(t, fixture, getModelsRequest("models")).GetModels()

	// Assert every response uses the same catalog and confirmed selection.
	require.Len(t, models.GetModels(), 2)
	assert.Equal(t, "openai-codex", models.GetModels()[0].GetProviderId())
	assert.Equal(t, "gpt-test", models.GetModels()[0].GetModelId())
	assert.Equal(t, []programmaticv1.InputModality{
		programmaticv1.InputModality_INPUT_MODALITY_TEXT,
	}, models.GetModels()[0].GetInputModalities())
	assert.Equal(t, int64(131072), models.GetModels()[0].GetContextWindow())
	assert.Equal(t, int64(16384), models.GetModels()[0].GetMaxTokens())
	assert.Equal(t, "openai-codex", models.GetModels()[1].GetProviderId())
	assert.Equal(t, "gpt-vision", models.GetModels()[1].GetModelId())
	assert.Equal(t, []programmaticv1.InputModality{
		programmaticv1.InputModality_INPUT_MODALITY_TEXT,
		programmaticv1.InputModality_INPUT_MODALITY_IMAGE,
	}, models.GetModels()[1].GetInputModalities())
	assert.Equal(t, int64(262144), models.GetModels()[1].GetContextWindow())
	assert.Equal(t, int64(32768), models.GetModels()[1].GetMaxTokens())
	assert.Equal(t, []programmaticv1.ReasoningChoice{
		programmaticv1.ReasoningChoice_REASONING_CHOICE_OFF,
	}, models.GetModels()[0].GetReasoning().GetChoices())
	assert.Equal(
		t,
		programmaticv1.ReasoningChoice_REASONING_CHOICE_OFF,
		models.GetActiveSelection().GetReasoningChoice(),
	)

	modelSelection := completeProgrammaticRequest(
		t, fixture, selectModelRequest("model", "openai-codex", "gpt-test"),
	).GetModelSelection().GetSelection()
	assert.Equal(t, "openai-codex", modelSelection.GetProviderId())
	assert.Equal(t, "gpt-test", modelSelection.GetModelId())

	reasoning := completeProgrammaticRequest(t, fixture, selectReasoningRequest(
		"reasoning", programmaticv1.ReasoningChoice_REASONING_CHOICE_OFF,
	)).GetModelSelection().GetSelection()
	assert.Equal(t, programmaticv1.ReasoningChoice_REASONING_CHOICE_OFF, reasoning.GetReasoningChoice())

	fixture.closeOwner(t)
}
