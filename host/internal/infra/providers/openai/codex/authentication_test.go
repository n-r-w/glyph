//go:build !integration

package codex

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestDriverCheckCredentialsUsesProviderOwnedClassification verifies Host credential-check ownership.
func TestDriverCheckCredentialsUsesProviderOwnedClassification(t *testing.T) {
	t.Parallel()

	t.Run("usable credentials", func(t *testing.T) {
		t.Parallel()
		accountID := "account"
		accessToken := testJWT(t, map[string]any{
			"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
		})
		credentials := NewMockCredentials(gomock.NewController(t))
		credentials.EXPECT().Load().Return(
			testCredentialPayload(
				t,
				accessToken,
				"refresh",
				accountID,
				time.Now().Add(time.Hour),
			), true, nil,
		)
		interaction := NewMockInteraction(gomock.NewController(t))
		service := newDriver(testConfig(), credentials, interaction, defaultDriverOptions())

		err := service.CheckCredentials(t.Context())

		require.NoError(t, err)
	})

	t.Run("missing credentials", func(t *testing.T) {
		t.Parallel()
		credentials := NewMockCredentials(gomock.NewController(t))
		credentials.EXPECT().Load().Return(nil, false, nil)
		interaction := NewMockInteraction(gomock.NewController(t))
		service := newDriver(testConfig(), credentials, interaction, defaultDriverOptions())

		err := service.CheckCredentials(t.Context())

		require.ErrorIs(t, err, ErrSignInRequired)
		assert.True(t, service.IsSignInRequired(err))
	})

	t.Run("malformed credentials", func(t *testing.T) {
		t.Parallel()
		credentials := NewMockCredentials(gomock.NewController(t))
		credentials.EXPECT().Load().Return([]byte("not-json"), true, nil)
		interaction := NewMockInteraction(gomock.NewController(t))
		service := newDriver(testConfig(), credentials, interaction, defaultDriverOptions())

		err := service.CheckCredentials(t.Context())

		require.ErrorIs(t, err, ErrSignInRequired)
		assert.True(t, service.IsSignInRequired(err))
	})
}
