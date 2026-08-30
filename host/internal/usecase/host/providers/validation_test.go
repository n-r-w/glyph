package providers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestValidateConfiguredChecksSelectionAndCredentialsWithoutExecution verifies final handler selection validation has
// no model side effect.
func TestValidateConfiguredChecksSelectionAndCredentialsWithoutExecution(t *testing.T) {
	t.Parallel()

	// Arrange one configured model with valid provider-owned credentials and a strict unused provider.
	controller := gomock.NewController(t)
	provider := agentrun.NewMockModelProvider(controller)
	authentication := NewMockProviderAuthentication(controller)
	authentication.EXPECT().CheckProviderAuthentication(gomock.Any()).Return(nil)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog, err := New([]Entry{{
		Descriptor: descriptor("provider", "model", model.ReasoningChoiceOff), Provider: provider,
		SelectionCredentialValidator: nil, Authentication: authentication,
	}}, selection)
	require.NoError(t, err)

	// Act by validating the exact selection.
	err = catalog.ValidateConfigured(t.Context(), selection)

	// Assert validation succeeds without provider execution or active-selection mutation.
	require.NoError(t, err)
	assert.Equal(t, selection, catalog.Selection())
}

// TestValidateConfiguredClassifiesUnavailableState verifies missing models, reasoning, and credentials use existing
// codes.
func TestValidateConfiguredClassifiesUnavailableState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		selection      model.Selection
		authentication func(*MockProviderAuthentication)
		expected       ErrorCode
	}{
		{
			name: "model",
			selection: model.Selection{
				Provider:        "missing",
				Model:           "model",
				ReasoningChoice: model.ReasoningChoiceOff,
			},
			authentication: func(*MockProviderAuthentication) {},
			expected:       ErrorCodeNotFound,
		},
		{
			name: "reasoning",
			selection: model.Selection{
				Provider:        "provider",
				Model:           "model",
				ReasoningChoice: model.ReasoningChoiceHigh,
			},
			authentication: func(*MockProviderAuthentication) {},
			expected:       ErrorCodeReasoningUnsupported,
		},
		{
			name:      "credentials",
			selection: model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff},
			authentication: func(authentication *MockProviderAuthentication) {
				authentication.EXPECT().CheckProviderAuthentication(gomock.Any()).Return(context.Canceled)
			},
			expected: ErrorCodeCredentialUnavailable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one configured model and strict provider dependencies.
			controller := gomock.NewController(t)
			provider := agentrun.NewMockModelProvider(controller)
			authentication := NewMockProviderAuthentication(controller)
			test.authentication(authentication)
			active := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
			catalog, err := New([]Entry{{
				Descriptor: descriptor("provider", "model", model.ReasoningChoiceOff), Provider: provider,
				SelectionCredentialValidator: nil, Authentication: authentication,
			}}, active)
			require.NoError(t, err)

			// Act by validating unavailable state.
			err = catalog.ValidateConfigured(t.Context(), test.selection)

			// Assert the existing selection classification is preserved without mutation.
			var selectionErr *SelectionError
			require.ErrorAs(t, err, &selectionErr)
			assert.Equal(t, test.expected, selectionErr.Code)
			assert.Equal(t, active, catalog.Selection())
		})
	}
}
