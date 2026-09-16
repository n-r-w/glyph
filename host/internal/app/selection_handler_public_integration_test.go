//go:build integration

package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// externalSelectionCompositionEnvironment enables the public fixture's ordered selection transforms.
	externalSelectionCompositionEnvironment = "GLYPH_EXTERNAL_SELECTION_COMPOSITION"
)

// TestPublicSelectionHandlersComposeOriginalAndCurrentTargets verifies real handler dispatch for both request kinds.
func TestPublicSelectionHandlersComposeOriginalAndCurrentTargets(t *testing.T) {
	// Arrange one real external process with two composing handlers for each selection request kind.
	t.Setenv(externalSelectionCompositionEnvironment, "1")
	paths := testPaths(t, selectionCompositionSettings())
	writeProgrammaticCredentials(t, paths)
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildPublicExtensionFixture(t))
	defer fixture.closeOwner(t)

	// Act through Programmatic Control while handlers execute over ExtensionService.Open.
	modelCompleted := completeProgrammaticRequest(
		t,
		fixture,
		selectModelRequest("model-selection", "openai-codex", "gpt-test"),
	).GetModelSelection()
	reasoningCompleted := completeProgrammaticRequest(
		t,
		fixture,
		selectReasoningRequest("reasoning-selection", programmaticv1.ReasoningChoice_REASONING_CHOICE_OFF),
	).GetModelSelection()

	// Assert each real chain observed immutable original and successive current targets before replacing with high.
	require.Empty(t, modelCompleted.GetIssues())
	assert.Equal(
		t,
		programmaticv1.ReasoningChoice_REASONING_CHOICE_HIGH,
		modelCompleted.GetSelection().GetReasoningChoice(),
	)
	require.Empty(t, reasoningCompleted.GetIssues())
	assert.Equal(
		t,
		programmaticv1.ReasoningChoice_REASONING_CHOICE_HIGH,
		reasoningCompleted.GetSelection().GetReasoningChoice(),
	)
}

// selectionCompositionSettings returns one model with all fixture reasoning targets.
func selectionCompositionSettings() string {
	return `defaultProvider: openai-codex
defaultModel: gpt-test
providers:
  openai-codex:
    type: openai-codex
    models:
      - id: gpt-test
        input: [text]
        contextWindow: 131072
        maxTokens: 16384
        toolCapabilities: {}
        pricing:
          input: 0
          output: 0
          cacheRead: 0
          cacheWrite: 0
        reasoning:
          supported: true
          choices: [off, low, high]
          default: off
`
}
