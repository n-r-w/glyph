//go:build integration

package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestSDKWirePreserveKeepsExactBytes preserves original bytes and private replay for header-only changes.
func TestSDKWirePreserveKeepsExactBytes(t *testing.T) {
	t.Parallel()
	// Arrange non-normalized request bytes containing private Responses replay.
	original := []byte(
		"{\n  \"input\" : [{ \"type\" : \"reasoning\", \"encrypted_content\" : \"private-cipher\" }], \"value\":1.00\n}",
	)
	var received []byte
	var receivedHeader string
	bodyObserved := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		receivedHeader = r.Header.Get("X-Test")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	callbacks := modelexecution.WireCallbacks{
		Headers: func(
			_ context.Context,
			h modelexecution.WireHeaders,
			_ func(modelexecution.WireHeaders) error,
		) (modelexecution.WireHeaders, error) {
			h["X-Test"] = []string{"header-only"}
			return h, nil
		},
		Body: func(_ context.Context, b []byte, _ func([]byte) error) (modelexecution.WireBodyResult, error) {
			bodyObserved = true
			require.NotContains(t, string(b), "private-cipher")
			return modelexecution.WireBodyResult{Body: b, Replaced: false}, nil
		},
		Response: func(context.Context, modelexecution.WireResponse) error { return nil },
		Failed:   func(err error) { t.Errorf("unexpected bridge failure: %v", err) },
	}
	client := sdk.NewClient(
		option.WithBaseURL(server.URL),
		option.WithAPIKey("secret"),
		option.WithMaxRetries(0),
		new(WireDiagnostics).NewWireOption(mo.Some(callbacks)),
	)
	// Act through the SDK with the exact provider-owned serialized request.
	err := client.Post(t.Context(), "/wire", nil, nil, option.WithRequestBody("application/json", original))
	// Assert preservation does not reserialize the body or discard private replay.
	require.NoError(t, err)
	require.Equal(t, original, received)
	require.Equal(t, "header-only", receivedHeader)
	require.True(t, bodyObserved)
}

// TestSDKWireInvalidReplacementPreventsDispatch rejects protected headers and malformed private references before HTTP.
func TestSDKWireInvalidReplacementPreventsDispatch(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"protected-header", "malformed-reference", "non-object"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			// Arrange a real SDK request with one invalid public replacement.
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
			defer server.Close()
			var acquired error
			callbacks := modelexecution.WireCallbacks{
				Headers: func(
					_ context.Context,
					h modelexecution.WireHeaders,
					_ func(modelexecution.WireHeaders) error,
				) (modelexecution.WireHeaders, error) {
					if scenario == "protected-header" {
						h["authorization"] = []string{"overwrite"}
					}
					return h, nil
				},
				Body: func(_ context.Context, b []byte, _ func([]byte) error) (modelexecution.WireBodyResult, error) {
					replacement := []byte(`{"input":[{"$glyph_replay":"unknown"}]}`)
					if scenario == "non-object" {
						replacement = []byte(`[]`)
					}
					return modelexecution.WireBodyResult{Body: replacement, Replaced: true}, nil
				},
				Response: func(context.Context, modelexecution.WireResponse) error {
					t.Error("observer must not run before dispatch")
					return nil
				},
				Failed: func(err error) { acquired = err },
			}
			client := sdk.NewClient(
				option.WithBaseURL(server.URL),
				option.WithAPIKey("secret"),
				option.WithMaxRetries(0),
				new(WireDiagnostics).NewWireOption(mo.Some(callbacks)),
			)
			// Act at the actual serialization and HTTP dispatch boundary.
			err := client.Post(t.Context(), "/wire", map[string]any{"input": []any{}}, nil)
			// Assert validation failure is independently acquired and no provider sees a request.
			require.Error(t, err)
			require.Error(t, acquired)
			require.Zero(t, calls)
		})
	}
}

// TestSDKWireAcquiresFailureBeforeCancellation retains acquired middleware causes across SDK cancellation.
func TestSDKWireAcquiresFailureBeforeCancellation(t *testing.T) {
	t.Parallel()
	// Arrange an observer that acquires a distinct cause concurrently with cancellation.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }),
	)
	defer server.Close()
	source := errors.New("acquired observer failure: unexpected EOF")
	var acquired error
	callbacks := modelexecution.WireCallbacks{
		Headers: func(
			_ context.Context,
			h modelexecution.WireHeaders,
			_ func(modelexecution.WireHeaders) error,
		) (modelexecution.WireHeaders, error) {
			return h, nil
		},
		Body: func(_ context.Context, b []byte, _ func([]byte) error) (modelexecution.WireBodyResult, error) {
			return modelexecution.WireBodyResult{Body: b, Replaced: false}, nil
		},
		Response: func(context.Context, modelexecution.WireResponse) error { cancel(); return source },
		Failed:   func(err error) { acquired = err },
	}
	client := sdk.NewClient(
		option.WithBaseURL(server.URL),
		option.WithAPIKey("secret"),
		option.WithMaxRetries(0),
		new(WireDiagnostics).NewWireOption(mo.Some(callbacks)),
	)
	// Act through the SDK while cancellation races its error reduction.
	err := client.Post(ctx, "/wire", map[string]any{}, nil)
	// Assert acquisition remains independent of whichever error the SDK returned.
	require.Error(t, err)
	require.ErrorIs(t, acquired, source)
	require.Contains(t, acquired.Error(), source.Error())
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}
