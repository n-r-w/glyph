package providers

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestCatalogReturnsOrderedDefensiveModelsAndSelection verifies immutable catalog queries.
func TestCatalogReturnsOrderedDefensiveModelsAndSelection(t *testing.T) {
	t.Parallel()

	providerA := agentrun.NewMockModelProvider(gomock.NewController(t))
	entries := []Entry{
		{
			Descriptor:        descriptor("z-provider", "z-first", model.ReasoningChoiceOff),
			Provider:          providerA,
			CredentialChecker: nil,
			Authentication:    nil,
		},
		{
			Descriptor: descriptor(
				"a-provider",
				"a-first",
				model.ReasoningChoiceLow,
				model.ReasoningChoiceHigh,
			),
			Provider:          providerA,
			CredentialChecker: nil,
			Authentication:    nil,
		},
		{
			Descriptor:        descriptor("a-provider", "a-second", model.ReasoningChoiceOff),
			Provider:          providerA,
			CredentialChecker: nil,
			Authentication:    nil,
		},
	}
	selection := model.Selection{
		Provider:        "a-provider",
		Model:           "a-first",
		ReasoningChoice: model.ReasoningChoiceHigh,
	}
	catalog, err := New(entries, selection)
	require.NoError(t, err)

	models := catalog.Models()
	require.Len(t, models, 3)
	assert.Equal(t, []model.ID{"a-first", "a-second", "z-first"}, []model.ID{
		models[0].Model, models[1].Model, models[2].Model,
	})
	assert.Equal(t, selection, catalog.ActiveSelection())
	snapshot := catalog.Snapshot()
	require.Equal(t, models[0], snapshot.Model)
	assert.Equal(t, model.ReasoningChoiceHigh, snapshot.ReasoningChoice)
	assert.Equal(t, providerA, snapshot.Provider)

	models[0].Model = "changed"
	models[0].Input[0] = model.InputModalityImage
	snapshot.Model.Input[0] = model.InputModalityImage
	snapshot.Model.ReasoningCapabilities.Choices[0] = model.ReasoningChoiceMax
	models[0].ReasoningCapabilities.Choices[0] = model.ReasoningChoiceMax
	fresh := catalog.Models()
	assert.Equal(t, model.ID("a-first"), fresh[0].Model)
	assert.Equal(t, []model.InputModality{model.InputModalityText, model.InputModalityImage}, fresh[0].Input)
	assert.Equal(t, model.InputModalityText, catalog.Snapshot().Model.Input[0])
	assert.Equal(
		t,
		[]model.ReasoningChoice{model.ReasoningChoiceLow, model.ReasoningChoiceHigh},
		fresh[0].ReasoningCapabilities.Choices,
	)
	assert.Equal(t, model.ReasoningChoiceLow, catalog.Snapshot().Model.ReasoningCapabilities.Choices[0])
}

// TestCatalogSelectModelAppliesReasoningFallback verifies model changes preserve or lower reasoning deterministically.
func TestCatalogSelectModelAppliesReasoningFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		active    model.ReasoningChoice
		supported []model.ReasoningChoice
		expected  model.ReasoningChoice
	}{
		{
			name:      "preserved",
			active:    model.ReasoningChoiceHigh,
			supported: []model.ReasoningChoice{model.ReasoningChoiceLow, model.ReasoningChoiceHigh},
			expected:  model.ReasoningChoiceHigh,
		},
		{
			name:   "greatest lower",
			active: model.ReasoningChoiceHigh,
			supported: []model.ReasoningChoice{
				model.ReasoningChoiceOff,
				model.ReasoningChoiceMinimal,
				model.ReasoningChoiceMedium,
			},
			expected: model.ReasoningChoiceMedium,
		},
		{
			name:      "lowest when no lower",
			active:    model.ReasoningChoiceLow,
			supported: []model.ReasoningChoice{model.ReasoningChoiceHigh, model.ReasoningChoiceMax},
			expected:  model.ReasoningChoiceHigh,
		},
		{
			name:      "lower effort on equal distance",
			active:    model.ReasoningChoiceMedium,
			supported: []model.ReasoningChoice{model.ReasoningChoiceLow, model.ReasoningChoiceHigh},
			expected:  model.ReasoningChoiceLow,
		},
		{
			name:      "effort maps to toggle on",
			active:    model.ReasoningChoiceHigh,
			supported: []model.ReasoningChoice{model.ReasoningChoiceOff, model.ReasoningChoiceOn},
			expected:  model.ReasoningChoiceOn,
		},
		{
			name:      "effort maps to fixed on",
			active:    model.ReasoningChoiceHigh,
			supported: []model.ReasoningChoice{model.ReasoningChoiceOn},
			expected:  model.ReasoningChoiceOn,
		},
		{
			name:      "on maps to effort default",
			active:    model.ReasoningChoiceOn,
			supported: []model.ReasoningChoice{model.ReasoningChoiceLow, model.ReasoningChoiceHigh},
			expected:  model.ReasoningChoiceLow,
		},
		{
			name:      "off is preserved when available",
			active:    model.ReasoningChoiceOff,
			supported: []model.ReasoningChoice{model.ReasoningChoiceHigh, model.ReasoningChoiceOff},
			expected:  model.ReasoningChoiceOff,
		},
		{
			name:      "off uses target default when unavailable",
			active:    model.ReasoningChoiceOff,
			supported: []model.ReasoningChoice{model.ReasoningChoiceHigh, model.ReasoningChoiceLow},
			expected:  model.ReasoningChoiceHigh,
		},
		{
			name:      "effort uses non-reasoning default",
			active:    model.ReasoningChoiceHigh,
			supported: []model.ReasoningChoice{model.ReasoningChoiceOff},
			expected:  model.ReasoningChoiceOff,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			provider := agentrun.NewMockModelProvider(gomock.NewController(t))
			catalog, err := New([]Entry{
				{
					Descriptor:        descriptor("provider", "active", test.active),
					Provider:          provider,
					CredentialChecker: nil,
					Authentication:    nil,
				},
				{
					Descriptor:        descriptor("provider", "target", test.supported...),
					Provider:          provider,
					CredentialChecker: nil,
					Authentication:    nil,
				},
			}, model.Selection{
				Provider:        "provider",
				Model:           "active",
				ReasoningChoice: test.active,
			})
			require.NoError(t, err)

			selected, err := catalog.SelectModel(t.Context(), "provider", "target")

			require.NoError(t, err)
			assert.Equal(t, model.Selection{
				Provider:        "provider",
				Model:           "target",
				ReasoningChoice: test.expected,
			}, selected)
			assert.Equal(t, selected, catalog.ActiveSelection())
		})
	}
}

// TestCatalogInvalidSelectionsReturnTypedErrorsAndPreserveSelection verifies atomic selection failures.
func TestCatalogInvalidSelectionsReturnTypedErrorsAndPreserveSelection(t *testing.T) {
	t.Parallel()

	active := model.Selection{
		Provider:        "provider",
		Model:           "active",
		ReasoningChoice: model.ReasoningChoiceLow,
	}
	provider := agentrun.NewMockModelProvider(gomock.NewController(t))
	catalog, err := New([]Entry{
		{
			Descriptor: descriptor(
				"provider", "active", model.ReasoningChoiceLow, model.ReasoningChoiceHigh,
			),
			Provider:          provider,
			CredentialChecker: nil,
			Authentication:    nil,
		},
	}, active)
	require.NoError(t, err)

	_, err = catalog.SelectModel(t.Context(), "missing", "model")
	var selectionErr *SelectionError
	require.ErrorAs(t, err, &selectionErr)
	assert.Equal(t, ErrorCodeNotFound, selectionErr.Code)
	assert.Equal(t, active, catalog.ActiveSelection())

	selected, err := catalog.SelectReasoningChoice(model.ReasoningChoiceHigh)
	require.NoError(t, err)
	active = model.Selection{
		Provider:        "provider",
		Model:           "active",
		ReasoningChoice: model.ReasoningChoiceHigh,
	}
	assert.Equal(t, active, selected)
	assert.Equal(t, active, catalog.ActiveSelection())

	_, err = catalog.SelectReasoningChoice(model.ReasoningChoiceMax)
	require.ErrorAs(t, err, &selectionErr)
	assert.Equal(t, ErrorCodeReasoningUnsupported, selectionErr.Code)
	assert.Equal(t, active, catalog.ActiveSelection())
}

// TestCatalogRejectsInvalidExecutionCapabilities checks source-independent descriptor validation.
func TestCatalogRejectsInvalidExecutionCapabilities(t *testing.T) {
	t.Parallel()

	testCases := map[string]func(*model.Descriptor){
		"empty input": func(configured *model.Descriptor) {
			configured.Input = nil
		},
		"missing text": func(configured *model.Descriptor) {
			configured.Input = []model.InputModality{model.InputModalityImage}
		},
		"duplicate modality": func(configured *model.Descriptor) {
			configured.Input = []model.InputModality{model.InputModalityText, model.InputModalityText}
		},
		"unknown modality": func(configured *model.Descriptor) {
			configured.Input = []model.InputModality{model.InputModalityText, "audio"}
		},
		"nonpositive context window": func(configured *model.Descriptor) {
			configured.ContextWindow = 0
		},
		"nonpositive max tokens": func(configured *model.Descriptor) {
			configured.MaxTokens = -1
		},
		"max tokens above context window": func(configured *model.Descriptor) {
			configured.MaxTokens = configured.ContextWindow + 1
		},
	}
	for name, mutate := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange one descriptor with the named invalid capability.
			configured := descriptor("provider", "model", model.ReasoningChoiceOff)
			mutate(&configured)
			provider := agentrun.NewMockModelProvider(gomock.NewController(t))

			// Act by constructing the source-independent catalog.
			_, err := New([]Entry{{
				Descriptor:        configured,
				Provider:          provider,
				CredentialChecker: nil,
				Authentication:    nil,
			}}, model.Selection{
				Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff,
			})

			// Assert invalid execution capabilities cannot enter catalog state.
			var selectionErr *SelectionError
			require.ErrorAs(t, err, &selectionErr)
			assert.Equal(t, ErrorCodeInvalidConfiguration, selectionErr.Code)
		})
	}
}

func TestCatalogRejectsEntryWithoutProvider(t *testing.T) {
	t.Parallel()

	selection := model.Selection{
		Provider:        "provider",
		Model:           "model",
		ReasoningChoice: model.ReasoningChoiceOff,
	}
	_, err := New([]Entry{{
		Descriptor:        descriptor("provider", "model", model.ReasoningChoiceOff),
		Provider:          nil,
		CredentialChecker: nil,
		Authentication:    nil,
	}}, selection)

	var selectionErr *SelectionError
	require.ErrorAs(t, err, &selectionErr)
	assert.Equal(t, ErrorCodeInvalidConfiguration, selectionErr.Code)
}

// TestCatalogCredentialPreflightDoesNotBlockSnapshots verifies I/O occurs outside the catalog lock.
func TestCatalogCredentialPreflightDoesNotBlockSnapshots(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	provider := agentrun.NewMockModelProvider(controller)
	validator := NewMockCredentialChecker(controller)
	started := make(chan struct{})
	release := make(chan struct{})
	validator.EXPECT().CheckCredentials(gomock.Any()).DoAndReturn(func(context.Context) error {
		close(started)
		<-release
		return nil
	})
	catalog, err := New([]Entry{
		{
			Descriptor: descriptor(
				"provider",
				"active",
				model.ReasoningChoiceLow,
				model.ReasoningChoiceHigh,
			),
			Provider:          provider,
			CredentialChecker: nil,
			Authentication:    nil,
		},
		{
			Descriptor: descriptor(
				"provider",
				"target",
				model.ReasoningChoiceOff,
				model.ReasoningChoiceLow,
			),
			Provider:          provider,
			CredentialChecker: validator,
			Authentication:    nil,
		},
	}, model.Selection{
		Provider:        "provider",
		Model:           "active",
		ReasoningChoice: model.ReasoningChoiceHigh,
	})
	require.NoError(t, err)

	result := make(chan model.Selection, 1)
	selectionErrors := make(chan error, 1)
	go func() {
		selected, selectErr := catalog.SelectModel(t.Context(), "provider", "target")
		result <- selected
		selectionErrors <- selectErr
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("credential validation did not start")
	}

	snapshots := make(chan agentrun.RequestSnapshot, 1)
	go func() { snapshots <- catalog.Snapshot() }()
	select {
	case snapshot := <-snapshots:
		assert.Equal(t, model.ID("active"), snapshot.Model.Model)
	case <-time.After(time.Second):
		t.Fatal("Snapshot blocked during credential resolution")
	}
	selected, err := catalog.SelectReasoningChoice(model.ReasoningChoiceLow)
	require.NoError(t, err)
	assert.Equal(t, model.ReasoningChoiceLow, selected.ReasoningChoice)

	close(release)
	require.NoError(t, <-selectionErrors)
	selected = <-result
	assert.Equal(t, model.Selection{
		Provider:        "provider",
		Model:           "target",
		ReasoningChoice: model.ReasoningChoiceLow,
	}, selected)
	assert.Equal(t, selected, catalog.ActiveSelection())
}

// TestCatalogCredentialFailureIsSafeAndPreservesSelection verifies atomic preflight failure.
func TestCatalogCredentialFailureIsSafeAndPreservesSelection(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	provider := agentrun.NewMockModelProvider(controller)
	validator := NewMockCredentialChecker(controller)
	safeCause := errors.New(`resolve environment API key "PROVIDER_API_KEY": unavailable`)
	validator.EXPECT().CheckCredentials(gomock.Any()).Return(safeCause)
	active := model.Selection{
		Provider:        "provider",
		Model:           "active",
		ReasoningChoice: model.ReasoningChoiceHigh,
	}
	catalog, err := New([]Entry{
		{
			Descriptor:        descriptor("provider", "active", model.ReasoningChoiceHigh),
			Provider:          provider,
			CredentialChecker: nil,
			Authentication:    nil,
		},
		{
			Descriptor:        descriptor("provider", "target", model.ReasoningChoiceOff),
			Provider:          provider,
			CredentialChecker: validator,
			Authentication:    nil,
		},
	}, active)
	require.NoError(t, err)

	_, err = catalog.SelectModel(t.Context(), "provider", "target")

	var selectionErr *SelectionError
	require.ErrorAs(t, err, &selectionErr)
	assert.Equal(t, ErrorCodeCredentialUnavailable, selectionErr.Code)
	require.ErrorIs(t, err, safeCause)
	require.ErrorContains(t, err, "PROVIDER_API_KEY")
	require.ErrorContains(t, err, "unavailable")
	assert.NotContains(t, err.Error(), "resolved-key-material")
	assert.Equal(t, active, catalog.ActiveSelection())
}

// TestCatalogAuthenticationDelegatesOnlyToActiveProvider verifies provider-owned UI authentication.
func TestCatalogAuthenticationDelegatesOnlyToActiveProvider(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	provider := agentrun.NewMockModelProvider(controller)
	authentication := NewMockProviderAuthentication(controller)
	catalog, err := New([]Entry{
		{
			Descriptor:        descriptor("compatible", "model", model.ReasoningChoiceOff),
			Provider:          provider,
			CredentialChecker: nil,
			Authentication:    nil,
		},
		{
			Descriptor:        descriptor("openai-codex", "model", model.ReasoningChoiceOff),
			Provider:          provider,
			Authentication:    authentication,
			CredentialChecker: nil,
		},
	}, model.Selection{
		Provider:        "compatible",
		Model:           "model",
		ReasoningChoice: model.ReasoningChoiceOff,
	})
	require.NoError(t, err)

	require.NoError(t, catalog.CheckAuthentication(t.Context()))
	require.NoError(t, catalog.SignIn(t.Context()))
	assert.False(t, catalog.IsSignInRequired(errors.New("other")))

	_, err = catalog.SelectModel(t.Context(), "openai-codex", "model")
	require.NoError(t, err)
	signInRequired := errors.New("sign in required")
	authentication.EXPECT().CheckCredentials(gomock.Any()).Return(signInRequired)
	authentication.EXPECT().SignIn(gomock.Any()).Return(nil)
	authentication.EXPECT().IsSignInRequired(signInRequired).Return(true)
	require.ErrorIs(t, catalog.CheckAuthentication(t.Context()), signInRequired)
	require.NoError(t, catalog.SignIn(t.Context()))
	assert.True(t, catalog.IsSignInRequired(signInRequired))
}

// TestCatalogPricingUsesExactProviderModelPair verifies overlapping model IDs cannot cross provider boundaries.
func TestCatalogPricingUsesExactProviderModelPair(t *testing.T) {
	t.Parallel()

	// Arrange two configured providers with the same model ID and different prices.
	provider := agentrun.NewMockModelProvider(gomock.NewController(t))
	priceA := model.Pricing{
		Input: 1, Output: 2, CacheRead: 0.1, CacheWrite: 0.5,
		Tiers: []model.PricingTier{{
			InputTokensAbove: 100, Input: 3, Output: 4, CacheRead: 0.3, CacheWrite: 0.7,
		}},
	}
	priceB := model.Pricing{Input: 5, Output: 6, CacheRead: 0.5, CacheWrite: 0.9, Tiers: nil}
	catalog, err := New([]Entry{
		{
			Descriptor: model.Descriptor{
				Provider: "provider-a", Model: "shared",
				Input: []model.InputModality{model.InputModalityText}, ContextWindow: 131072, MaxTokens: 16384,
				Pricing: mo.Some(priceA),
				ReasoningCapabilities: model.ReasoningCapabilities{
					Supported: false, Choices: []model.ReasoningChoice{model.ReasoningChoiceOff},
					Default: model.ReasoningChoiceOff,
				},
				ToolCapabilities: model.ToolCapabilities{
					StrictJSONSchema: false, Grammar: model.GrammarCapabilities{Lark: false, Regex: false},
				},
			},
			Provider: provider, CredentialChecker: nil, Authentication: nil,
		},
		{
			Descriptor: model.Descriptor{
				Provider: "provider-b", Model: "shared",
				Input: []model.InputModality{model.InputModalityText}, ContextWindow: 131072, MaxTokens: 16384,
				Pricing: mo.Some(priceB),
				ReasoningCapabilities: model.ReasoningCapabilities{
					Supported: false, Choices: []model.ReasoningChoice{model.ReasoningChoiceOff},
					Default: model.ReasoningChoiceOff,
				},
				ToolCapabilities: model.ToolCapabilities{
					StrictJSONSchema: false, Grammar: model.GrammarCapabilities{Lark: false, Regex: false},
				},
			},
			Provider: provider, CredentialChecker: nil, Authentication: nil,
		},
	}, model.Selection{Provider: "provider-a", Model: "shared", ReasoningChoice: model.ReasoningChoiceOff})
	require.NoError(t, err)

	// Act by resolving exact and unknown provider-model pairs.
	actualA := catalog.Pricing("provider-a", "shared")
	actualB := catalog.Pricing("provider-b", "shared")
	unknownProvider := catalog.Pricing("provider-c", "shared")
	unknownModel := catalog.Pricing("provider-a", "missing")

	// Assert each exact pair owns its price and unknown pairs remain absent.
	assert.Equal(t, mo.Some(priceA), actualA)
	assert.Equal(t, mo.Some(priceB), actualB)
	assert.True(t, unknownProvider.IsAbsent())
	assert.True(t, unknownModel.IsAbsent())
}

func descriptor(provider model.ProviderID, modelID model.ID, choices ...model.ReasoningChoice) model.Descriptor {
	return model.Descriptor{
		Provider:      provider,
		Model:         modelID,
		Input:         []model.InputModality{model.InputModalityText, model.InputModalityImage},
		ContextWindow: 131072,
		MaxTokens:     16384,
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: choices[0] != model.ReasoningChoiceOff || len(choices) > 1,
			Choices:   slices.Clone(choices),
			Default:   choices[0],
		},
		ToolCapabilities: model.ToolCapabilities{
			StrictJSONSchema: false,
			Grammar: model.GrammarCapabilities{
				Lark:  false,
				Regex: false,
			},
		}, Pricing: mo.None[model.Pricing](),
	}
}
