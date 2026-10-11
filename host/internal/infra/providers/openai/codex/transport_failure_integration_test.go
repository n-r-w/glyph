//go:build integration

package codex

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestDriverStreamCancellationRetainsTransportFailure checks diagnostics acquired after caller cancellation.
func TestDriverStreamCancellationRetainsTransportFailure(t *testing.T) {
	t.Parallel()

	// Arrange authenticated credentials and a transport with one unique failure.
	accountID := "transport-account"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(
		testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil,
	)
	interaction := NewMockInteraction(gomock.NewController(t))
	transportErr := fmt.Errorf("unique Codex transport failure: %w", syscall.ECONNRESET)
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(_ *http.Request) (*http.Response, error) {
		cancel()
		return nil, errors.Join(ctx.Err(), transportErr)
	})
	options := defaultDriverOptions()
	options.modelBaseURL = "https://transport.invalid"
	options.httpClient = &http.Client{
		Transport:     transport,
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       0,
	}
	service := newDriver(testConfig(), credentials, interaction, options)

	// Act by starting one model stream.
	events, err := collectStreamEvents(service, ctx, modelexecution.ProviderRequest{
		ReasoningChoice: model.ReasoningChoiceOn,
		Instructions:    "instructions",
		Model:           testModelDescriptor("gpt-test"),
		History:         nil,
		Tools:           nil,
	}, nil)
	response := terminalResponse(events)

	// Assert the raw transport cause remains classifiable and visible at both boundaries.
	require.Error(t, err)
	require.ErrorIs(t, err, transportErr)
	assert.Contains(t, err.Error(), transportErr.Error())
	assert.Contains(t, response.ErrorMessage.OrEmpty(), transportErr.Error())
	require.NotErrorIs(t, err, ErrSignInRequired)
	var providerFailure *modelexecution.ProviderFailureError
	require.ErrorAs(t, err, &providerFailure)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, strings.Count(err.Error(), transportErr.Error()))
	assert.Equal(t, model.OutcomeAborted, response.Outcome.OrEmpty())
}
