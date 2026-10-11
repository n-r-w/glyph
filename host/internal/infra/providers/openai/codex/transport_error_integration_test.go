//go:build integration

package codex

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestErrorCaptureTransportJoinsMissingCause checks transport and terminal error identity.
func TestErrorCaptureTransportJoinsMissingCause(t *testing.T) {
	t.Parallel()

	// Arrange an acquired transport failure and a separate terminal failure.
	transportErr := errors.New("acquired transport source")
	terminalErr := errors.New("terminal source")
	base := NewMockHTTPRoundTripper(gomock.NewController(t))
	base.EXPECT().RoundTrip(gomock.Any()).Return(nil, transportErr)
	transport := newErrorCaptureTransport(base)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://transport.invalid", nil)
	require.NoError(t, err)
	response, err := transport.RoundTrip(request)
	if response != nil {
		require.NoError(t, response.Body.Close())
	}
	require.ErrorIs(t, err, transportErr)

	// Act by restoring the cause the SDK omitted.
	joined := transport.JoinError(terminalErr)

	// Assert both acquired causes remain inspectable.
	require.ErrorIs(t, joined, transportErr)
	require.ErrorIs(t, joined, terminalErr)
	assert.ErrorContains(t, joined, transportErr.Error())
}

// TestErrorCaptureTransportKeepsExistingCause checks SDK errors and pure cancellation stay intact.
func TestErrorCaptureTransportKeepsExistingCause(t *testing.T) {
	t.Parallel()

	for _, cause := range []error{errors.New("transport source"), context.Canceled} {
		t.Run(cause.Error(), func(t *testing.T) {
			t.Parallel()
			// Arrange an SDK error that already contains the acquired cause.
			base := NewMockHTTPRoundTripper(gomock.NewController(t))
			base.EXPECT().RoundTrip(gomock.Any()).Return(nil, cause)
			transport := newErrorCaptureTransport(base)
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://transport.invalid", nil)
			require.NoError(t, err)
			response, err := transport.RoundTrip(request)
			if response != nil {
				require.NoError(t, response.Body.Close())
			}
			require.ErrorIs(t, err, cause)
			sdkErr := fmt.Errorf("SDK request: %w", cause)

			// Act by combining the SDK and capture results.
			joined := transport.JoinError(sdkErr)

			// Assert the full SDK error retains its identity without repetition.
			assert.Same(t, sdkErr, joined)
			assert.Equal(t, sdkErr.Error(), joined.Error())
		})
	}
}
