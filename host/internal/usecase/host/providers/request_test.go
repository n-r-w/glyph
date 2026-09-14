//go:build !integration

package providers

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestResolveConfiguredBindingExecutesExactSelectionWithoutMutation verifies explicit resolution and preflight.
func TestResolveConfiguredBindingExecutesExactSelectionWithoutMutation(t *testing.T) {
	t.Parallel()

	// Arrange active and alternate entries with credentials on the alternate entry.
	controller := gomock.NewController(t)
	activeProvider := modelexecution.NewMockProviderAttempt(controller)
	alternateProvider := modelexecution.NewMockProviderAttempt(controller)
	validator := NewMockCredentialChecker(controller)
	validator.EXPECT().CheckCredentials(gomock.Any()).Return(nil)
	active := model.Selection{Provider: "active", Model: "main", ReasoningChoice: model.ReasoningChoiceLow}
	alternate := model.Selection{Provider: "alternate", Model: "summary", ReasoningChoice: model.ReasoningChoiceHigh}
	catalog, err := New([]Entry{
		{
			Descriptor: descriptor("active", "main", model.ReasoningChoiceLow), Provider: activeProvider,
			CredentialChecker: nil, Authentication: nil,
		},
		{
			Descriptor: descriptor("alternate", "summary", model.ReasoningChoiceHigh), Provider: alternateProvider,
			CredentialChecker: validator, Authentication: nil,
		},
	}, active)
	require.NoError(t, err)

	// Act with the alternate explicit selection.
	binding, err := catalog.ResolveConfiguredBinding(t.Context(), alternate)

	// Assert exact raw binding resolution and unchanged active selection.
	require.NoError(t, err)
	assert.Equal(t, alternate.Provider, binding.Model.Provider)
	assert.Equal(t, alternate.Model, binding.Model.Model)
	assert.Equal(t, alternate.ReasoningChoice, binding.ReasoningChoice)
	assert.Same(t, alternateProvider, binding.Provider)
	assert.Equal(t, active, catalog.ActiveSelection())
}

// TestResolveConfiguredBindingChecksCredentialsWithoutMutation verifies provider credentials run before dispatch.
func TestResolveConfiguredBindingChecksCredentialsWithoutMutation(t *testing.T) {
	t.Parallel()

	// Arrange one entry whose provider authentication rejects credentials.
	controller := gomock.NewController(t)
	provider := modelexecution.NewMockProviderAttempt(controller)
	authentication := NewMockProviderAuthentication(controller)
	authentication.EXPECT().CheckCredentials(gomock.Any()).Return(context.Canceled)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog, err := New([]Entry{{
		Descriptor: descriptor("provider", "model", model.ReasoningChoiceOff), Provider: provider,
		CredentialChecker: nil, Authentication: authentication,
	}}, selection)
	require.NoError(t, err)

	// Act by resolving one configured request.
	_, err = catalog.ResolveConfiguredBinding(t.Context(), selection)

	// Assert credential classification and unchanged active selection.
	var selectionErr *SelectionError
	require.ErrorAs(t, err, &selectionErr)
	assert.Equal(t, ErrorCodeCredentialUnavailable, selectionErr.Code)
	assert.Equal(t, selection, catalog.ActiveSelection())
}

// TestResolveConfiguredBindingPreservesCredentialFailure verifies complete credential causes remain available.
func TestResolveConfiguredBindingPreservesCredentialFailure(t *testing.T) {
	t.Parallel()

	// Arrange one explicit selection with a failing credential checker.
	controller := gomock.NewController(t)
	provider := modelexecution.NewMockProviderAttempt(controller)
	validator := NewMockCredentialChecker(controller)
	credentialErr := errors.New("complete credential failure")
	validator.EXPECT().CheckCredentials(gomock.Any()).Return(credentialErr)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog, err := New([]Entry{{
		Descriptor: descriptor("provider", "model", model.ReasoningChoiceOff), Provider: provider,
		CredentialChecker: validator, Authentication: nil,
	}}, selection)
	require.NoError(t, err)

	// Act through configured binding resolution.
	_, err = catalog.ResolveConfiguredBinding(t.Context(), selection)

	// Assert the complete credential cause remains exposed and active state is unchanged.
	require.ErrorIs(t, err, credentialErr)
	assert.Contains(t, err.Error(), credentialErr.Error())
	assert.Equal(t, selection, catalog.ActiveSelection())
}

// TestResolveBindingRejectsUnavailableSelectionWithoutMutation verifies exact model and reasoning validation.
func TestResolveBindingRejectsUnavailableSelectionWithoutMutation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		// name identifies the invalid selection field.
		name string
		// selection contains the exact invalid request.
		selection model.Selection
		// code identifies the expected catalogue failure.
		code ErrorCode
	}{
		{
			name: "model",
			selection: model.Selection{
				Provider: "missing", Model: "model", ReasoningChoice: model.ReasoningChoiceOff,
			},
			code: ErrorCodeNotFound,
		},
		{
			name: "reasoning",
			selection: model.Selection{
				Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh,
			},
			code: ErrorCodeReasoningUnsupported,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one configured model and active selection.
			provider := modelexecution.NewMockProviderAttempt(gomock.NewController(t))
			active := model.Selection{
				Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff,
			}
			catalog, err := New([]Entry{{
				Descriptor: descriptor("provider", "model", model.ReasoningChoiceOff), Provider: provider,
				CredentialChecker: nil, Authentication: nil,
			}}, active)
			require.NoError(t, err)

			// Act with an unavailable explicit selection.
			_, err = catalog.ResolveBinding(test.selection)

			// Assert rejection and unchanged active state.
			var selectionErr *SelectionError
			require.ErrorAs(t, err, &selectionErr)
			assert.Equal(t, test.code, selectionErr.Code)
			assert.Equal(t, active, catalog.ActiveSelection())
		})
	}
}
