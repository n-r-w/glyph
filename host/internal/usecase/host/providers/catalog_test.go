//nolint:exhaustruct // Catalog tests set optional delegates only when behavior needs them.
package providers

import (
	"context"
	"errors"
	"testing"
	"time"

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
		{Descriptor: descriptor("z-provider", "z-first", model.ReasoningLevelNone), Provider: providerA},
		{Descriptor: descriptor("a-provider", "a-first", model.ReasoningLevelLow, model.ReasoningLevelHigh), Provider: providerA},
		{Descriptor: descriptor("a-provider", "a-second", model.ReasoningLevelNone), Provider: providerA},
	}
	selection := model.Selection{
		Provider: "a-provider", Model: "a-first", ReasoningLevel: model.ReasoningLevelHigh,
	}
	catalog, err := New(entries, selection)
	require.NoError(t, err)

	models := catalog.Models()
	require.Len(t, models, 3)
	assert.Equal(t, []model.ID{"a-first", "a-second", "z-first"}, []model.ID{
		models[0].Model, models[1].Model, models[2].Model,
	})
	assert.Equal(t, selection, catalog.Selection())
	current := catalog.Current()
	require.Equal(t, models[0], current.Model)
	assert.Equal(t, model.ReasoningLevelHigh, current.ReasoningLevel)
	assert.Equal(t, providerA, current.Provider)

	models[0].Model = "changed"
	current.Model.SupportedReasoningLevels[0] = model.ReasoningLevelMax
	models[0].SupportedReasoningLevels[0] = model.ReasoningLevelMax
	fresh := catalog.Models()
	assert.Equal(t, model.ID("a-first"), fresh[0].Model)
	assert.Equal(t, []model.ReasoningLevel{model.ReasoningLevelLow, model.ReasoningLevelHigh}, fresh[0].SupportedReasoningLevels)
	assert.Equal(t, model.ReasoningLevelLow, catalog.Current().Model.SupportedReasoningLevels[0])
}

// TestCatalogSelectModelAppliesReasoningFallback verifies model changes preserve or lower reasoning deterministically.
func TestCatalogSelectModelAppliesReasoningFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		active    model.ReasoningLevel
		supported []model.ReasoningLevel
		expected  model.ReasoningLevel
	}{
		{name: "preserved", active: model.ReasoningLevelHigh, supported: []model.ReasoningLevel{model.ReasoningLevelLow, model.ReasoningLevelHigh}, expected: model.ReasoningLevelHigh},
		{name: "greatest lower", active: model.ReasoningLevelHigh, supported: []model.ReasoningLevel{model.ReasoningLevelNone, model.ReasoningLevelMinimal, model.ReasoningLevelMedium}, expected: model.ReasoningLevelMedium},
		{name: "lowest when no lower", active: model.ReasoningLevelLow, supported: []model.ReasoningLevel{model.ReasoningLevelHigh, model.ReasoningLevelMax}, expected: model.ReasoningLevelHigh},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			provider := agentrun.NewMockModelProvider(gomock.NewController(t))
			catalog, err := New([]Entry{
				{Descriptor: descriptor("provider", "active", test.active), Provider: provider},
				{Descriptor: descriptor("provider", "target", test.supported...), Provider: provider},
			}, model.Selection{Provider: "provider", Model: "active", ReasoningLevel: test.active})
			require.NoError(t, err)

			selected, err := catalog.SelectModel(t.Context(), "provider", "target")

			require.NoError(t, err)
			assert.Equal(t, model.Selection{
				Provider: "provider", Model: "target", ReasoningLevel: test.expected,
			}, selected)
			assert.Equal(t, selected, catalog.Selection())
		})
	}
}

// TestCatalogInvalidSelectionsReturnTypedErrorsAndPreserveSelection verifies atomic selection failures.
func TestCatalogInvalidSelectionsReturnTypedErrorsAndPreserveSelection(t *testing.T) {
	t.Parallel()

	active := model.Selection{
		Provider: "provider", Model: "active", ReasoningLevel: model.ReasoningLevelLow,
	}
	provider := agentrun.NewMockModelProvider(gomock.NewController(t))
	catalog, err := New([]Entry{
		{Descriptor: descriptor(
			"provider", "active", model.ReasoningLevelLow, model.ReasoningLevelHigh,
		), Provider: provider},
	}, active)
	require.NoError(t, err)

	_, err = catalog.SelectModel(t.Context(), "missing", "model")
	var selectionErr *SelectionError
	require.ErrorAs(t, err, &selectionErr)
	assert.Equal(t, ErrorCodeNotFound, selectionErr.Code)
	assert.Equal(t, active, catalog.Selection())

	selected, err := catalog.SelectReasoningLevel(model.ReasoningLevelHigh)
	require.NoError(t, err)
	active = model.Selection{
		Provider: "provider", Model: "active", ReasoningLevel: model.ReasoningLevelHigh,
	}
	assert.Equal(t, active, selected)
	assert.Equal(t, active, catalog.Selection())

	_, err = catalog.SelectReasoningLevel(model.ReasoningLevelMax)
	require.ErrorAs(t, err, &selectionErr)
	assert.Equal(t, ErrorCodeReasoningUnsupported, selectionErr.Code)
	assert.Equal(t, active, catalog.Selection())
}

// TestCatalogRejectsEntryWithoutProvider prevents a selectable model from producing a nil runtime.
func TestCatalogRejectsEntryWithoutProvider(t *testing.T) {
	t.Parallel()

	selection := model.Selection{
		Provider: "provider", Model: "model", ReasoningLevel: model.ReasoningLevelNone,
	}
	_, err := New([]Entry{{
		Descriptor: descriptor("provider", "model", model.ReasoningLevelNone), Provider: nil,
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
	validator := NewMockSelectionCredentialValidator(controller)
	started := make(chan struct{})
	release := make(chan struct{})
	validator.EXPECT().ValidateSelectionCredentials(gomock.Any()).DoAndReturn(func(context.Context) error {
		close(started)
		<-release
		return nil
	})
	catalog, err := New([]Entry{
		{Descriptor: descriptor("provider", "active", model.ReasoningLevelLow, model.ReasoningLevelHigh), Provider: provider},
		{Descriptor: descriptor("provider", "target", model.ReasoningLevelNone, model.ReasoningLevelLow), Provider: provider, SelectionCredentialValidator: validator},
	}, model.Selection{Provider: "provider", Model: "active", ReasoningLevel: model.ReasoningLevelHigh})
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

	current := make(chan agentrun.RuntimeSelection, 1)
	go func() { current <- catalog.Current() }()
	select {
	case snapshot := <-current:
		assert.Equal(t, model.ID("active"), snapshot.Model.Model)
	case <-time.After(time.Second):
		t.Fatal("Current blocked during credential resolution")
	}
	selected, err := catalog.SelectReasoningLevel(model.ReasoningLevelLow)
	require.NoError(t, err)
	assert.Equal(t, model.ReasoningLevelLow, selected.ReasoningLevel)

	close(release)
	require.NoError(t, <-selectionErrors)
	selected = <-result
	assert.Equal(t, model.Selection{
		Provider: "provider", Model: "target", ReasoningLevel: model.ReasoningLevelLow,
	}, selected)
	assert.Equal(t, selected, catalog.Selection())
}

// TestCatalogCredentialFailureIsSafeAndPreservesSelection verifies atomic preflight failure.
func TestCatalogCredentialFailureIsSafeAndPreservesSelection(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	provider := agentrun.NewMockModelProvider(controller)
	validator := NewMockSelectionCredentialValidator(controller)
	safeCause := errors.New(`resolve environment API key "PROVIDER_API_KEY": unavailable`)
	validator.EXPECT().ValidateSelectionCredentials(gomock.Any()).Return(safeCause)
	active := model.Selection{Provider: "provider", Model: "active", ReasoningLevel: model.ReasoningLevelHigh}
	catalog, err := New([]Entry{
		{Descriptor: descriptor("provider", "active", model.ReasoningLevelHigh), Provider: provider},
		{Descriptor: descriptor("provider", "target", model.ReasoningLevelNone), Provider: provider, SelectionCredentialValidator: validator},
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
	assert.Equal(t, active, catalog.Selection())
}

// TestCatalogAuthenticationDelegatesOnlyToActiveProvider verifies provider-owned UI authentication.
func TestCatalogAuthenticationDelegatesOnlyToActiveProvider(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	provider := agentrun.NewMockModelProvider(controller)
	authentication := NewMockProviderAuthentication(controller)
	catalog, err := New([]Entry{
		{Descriptor: descriptor("compatible", "model", model.ReasoningLevelNone), Provider: provider},
		{Descriptor: descriptor("openai-codex", "model", model.ReasoningLevelNone), Provider: provider, Authentication: authentication},
	}, model.Selection{Provider: "compatible", Model: "model", ReasoningLevel: model.ReasoningLevelNone})
	require.NoError(t, err)

	require.NoError(t, catalog.CheckAuthentication(t.Context()))
	require.NoError(t, catalog.SignIn(t.Context()))
	assert.False(t, catalog.IsSignInRequired(errors.New("other")))

	_, err = catalog.SelectModel(t.Context(), "openai-codex", "model")
	require.NoError(t, err)
	signInRequired := errors.New("sign in required")
	authentication.EXPECT().CheckProviderAuthentication(gomock.Any()).Return(signInRequired)
	authentication.EXPECT().SignInProvider(gomock.Any()).Return(nil)
	authentication.EXPECT().IsProviderSignInRequired(signInRequired).Return(true)
	require.ErrorIs(t, catalog.CheckAuthentication(t.Context()), signInRequired)
	require.NoError(t, catalog.SignIn(t.Context()))
	assert.True(t, catalog.IsSignInRequired(signInRequired))
}

func descriptor(provider model.ProviderID, modelID model.ID, levels ...model.ReasoningLevel) model.Descriptor {
	return model.Descriptor{
		Provider: provider, Model: modelID,
		SupportedReasoningLevels: levels,
		ToolCapabilities: model.ToolCapabilities{
			StrictJSONSchema: false,
			Grammar:          model.GrammarCapabilities{Lark: false, Regex: false},
		},
	}
}
