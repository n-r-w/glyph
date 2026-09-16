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

// TestCatalogResolvesValidatesAndCommitsCompleteSelection verifies the non-mutating and atomic boundaries.
func TestCatalogResolvesValidatesAndCommitsCompleteSelection(t *testing.T) {
	t.Parallel()
	// Arrange an active model and a credential-protected target model.
	controller := gomock.NewController(t)
	provider := modelexecution.NewMockProviderAttempt(controller)
	credentials := NewMockCredentialChecker(controller)
	credentials.EXPECT().CheckCredentials(gomock.Any()).Return(nil)
	active := model.Selection{Provider: "provider", Model: "active", ReasoningChoice: model.ReasoningChoiceHigh}
	catalog, err := New([]Entry{
		{
			Descriptor: descriptor("provider", "active", model.ReasoningChoiceHigh), Provider: provider,
			CredentialChecker: nil, Authentication: nil,
		},
		{
			Descriptor: descriptor("provider", "target", model.ReasoningChoiceLow), Provider: provider,
			CredentialChecker: credentials, Authentication: nil,
		},
	}, active)
	require.NoError(t, err)

	// Act by resolving and validating before one complete commit.
	target, err := catalog.ResolveModel("provider", "target")
	require.NoError(t, err)
	assert.Equal(t, active, catalog.ActiveSelection())
	require.NoError(t, catalog.ValidateSelection(t.Context(), target))
	assert.Equal(t, active, catalog.ActiveSelection())
	preceding, committed, err := catalog.CommitSelection(t.Context(), target)

	// Assert every selection field changes together and both snapshots are returned.
	require.NoError(t, err)
	assert.Equal(t, active, preceding)
	assert.Equal(t, target, committed)
	assert.Equal(t, target, catalog.ActiveSelection())
	assert.Equal(t, model.ID("target"), catalog.ActiveBinding().Model.Model)
}

// TestCatalogCommitCancellationPreservesSelection verifies the final cancellation check occurs before mutation.
func TestCatalogCommitCancellationPreservesSelection(t *testing.T) {
	t.Parallel()
	// Arrange a resolved target and a canceled commit context.
	provider := modelexecution.NewMockProviderAttempt(gomock.NewController(t))
	active := model.Selection{Provider: "provider", Model: "active", ReasoningChoice: model.ReasoningChoiceHigh}
	catalog, err := New([]Entry{
		{
			Descriptor: descriptor("provider", "active", model.ReasoningChoiceHigh), Provider: provider,
			CredentialChecker: nil, Authentication: nil,
		},
		{
			Descriptor: descriptor("provider", "target", model.ReasoningChoiceLow), Provider: provider,
			CredentialChecker: nil, Authentication: nil,
		},
	}, active)
	require.NoError(t, err)
	target, err := catalog.ResolveModel("provider", "target")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// Act by attempting the final atomic commit.
	_, _, err = catalog.CommitSelection(ctx, target)

	// Assert cancellation is exposed and no active field changed.
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, active, catalog.ActiveSelection())
}

// TestCatalogFinalValidationFailurePreservesSelection verifies credential failure cannot mutate active state.
func TestCatalogFinalValidationFailurePreservesSelection(t *testing.T) {
	t.Parallel()
	// Arrange a target whose credential check fails.
	controller := gomock.NewController(t)
	provider := modelexecution.NewMockProviderAttempt(controller)
	credentials := NewMockCredentialChecker(controller)
	credentialErr := errors.New("credentials unavailable")
	credentials.EXPECT().CheckCredentials(gomock.Any()).Return(credentialErr)
	active := model.Selection{Provider: "provider", Model: "active", ReasoningChoice: model.ReasoningChoiceOff}
	catalog, err := New([]Entry{
		{
			Descriptor: descriptor("provider", "active", model.ReasoningChoiceOff), Provider: provider,
			CredentialChecker: nil, Authentication: nil,
		},
		{
			Descriptor: descriptor("provider", "target", model.ReasoningChoiceOn), Provider: provider,
			CredentialChecker: credentials, Authentication: nil,
		},
	}, active)
	require.NoError(t, err)
	target, err := catalog.ResolveModel("provider", "target")
	require.NoError(t, err)

	// Act by validating the final target.
	err = catalog.ValidateSelection(t.Context(), target)

	// Assert the typed complete failure and unchanged active selection.
	var selectionErr *SelectionError
	require.ErrorAs(t, err, &selectionErr)
	assert.Equal(t, ErrorCodeCredentialUnavailable, selectionErr.Code)
	assert.ErrorIs(t, err, credentialErr)
	assert.Equal(t, active, catalog.ActiveSelection())
}
