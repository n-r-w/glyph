//go:build integration

package openai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestWireRetainedFailureUsesSecretRedaction preserves source identity while redacting private values.
func TestWireRetainedFailureUsesSecretRedaction(t *testing.T) {
	t.Parallel()
	// Arrange the shared execution owner and a neutral substitute using the real SDK bridge.
	ctrl := gomock.NewController(t)
	owner := modelexecution.NewMockWireMiddleware(ctrl)
	provider := modelexecution.NewMockProviderAttempt(ctrl)
	catalog := modelexecution.NewMockCatalogResolver(ctrl)
	source := errors.New("wire rejected token-private: complete original cause")
	owner.EXPECT().NewWireCallbacks().Return(modelexecution.WireCallbacks{
		Headers: func(
			context.Context,
			modelexecution.WireHeaders,
			func(modelexecution.WireHeaders) error,
		) (modelexecution.WireHeaders, error) {
			return nil, source
		},
		Body: func(_ context.Context, b []byte, _ func([]byte) error) (modelexecution.WireBodyResult, error) {
			return modelexecution.WireBodyResult{Body: b, Replaced: false}, nil
		},
		Response: func(context.Context, modelexecution.WireResponse) error { return nil },
		Failed:   nil,
	})
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	provider.EXPECT().
		Stream(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, r modelexecution.ProviderRequest, _ modelexecution.StreamHandler) error {
			client := sdk.NewClient(
				option.WithBaseURL(server.URL),
				option.WithAPIKey("token-private"),
				option.WithMaxRetries(0),
				new(WireDiagnostics).NewWireOption(r.Wire),
			)
			return client.Post(ctx, "/wire", map[string]any{}, nil)
		})
	catalog.EXPECT().
		ResolveConfiguredBinding(gomock.Any(), gomock.Any()).
		Return(modelexecution.CatalogBinding{
			Model: model.Descriptor{}, ReasoningChoice: model.ReasoningChoiceOff, Provider: provider,
		}, nil)
	execution := modelexecution.New(
		catalog,
		modelexecution.RetryPolicy{
			Enabled:          true,
			MaxRetries:       1,
			Delays:           []time.Duration{0},
			MaxProviderDelay: time.Second,
		},
		nil,
		nil,
		nil,
	)
	require.NoError(t, execution.BindWireMiddleware(owner))
	require.NoError(t, execution.BindResponseFinalizer(passThroughFinalizer(ctrl)))
	// Act through the configured caller and actual SDK failure reduction boundary.
	_, err := execution.RequestConfigured(t.Context(), model.Selection{}, "", nil, nil)
	// Assert complete non-secret text and original identity survive without leaking the acquired secret.
	require.Error(t, err)
	require.ErrorIs(t, err, source)
	require.NotContains(t, err.Error(), "token-private")
	require.Contains(t, err.Error(), "complete original cause")
	failure, ok := errors.AsType[*modelexecution.LogicalFailureError](err)
	require.True(t, ok)
	require.Equal(t, modelexecution.FailureExtensionFailed, failure.Category)
	require.Zero(t, calls)
}

// passThroughFinalizer returns a configured-response finalizer that returns each response unchanged.
func passThroughFinalizer(ctrl *gomock.Controller) *modelexecution.MockResponseFinalizer {
	finalizer := modelexecution.NewMockResponseFinalizer(ctrl)
	finalizer.EXPECT().FinalizeConfiguredResponse(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, response model.Response) (model.Response, error) { return response, nil },
	).AnyTimes()
	return finalizer
}
