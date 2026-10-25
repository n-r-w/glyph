//go:build integration

package app

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	// ordinaryNestedEnvironment enables a consumed configured child in the real observer fixture.
	ordinaryNestedEnvironment = "GLYPH_ORDINARY_OBSERVER_NESTED"
	// retryFailureOnceMode leaves outer retry handling healthy after the child's first handler failure.
	retryFailureOnceMode = "fail-once"
)

// TestPublicObserverNestedFailureStopsOuterRecovery checks retained child failure stops a healthy outer retry path.
func TestPublicObserverNestedFailureStopsOuterRecovery(t *testing.T) {
	// Arrange an ordinary observer that consumes its configured child's failed retry handler.
	signals := t.TempDir()
	t.Setenv(externalSignalsEnvironment, signals)
	t.Setenv(ordinaryObserverEnvironment, extensionpb.HandlerKind_HANDLER_KIND_MESSAGE_UPDATE.String())
	t.Setenv(ordinaryNestedEnvironment, "true")
	t.Setenv(retryHandlerEnvironment, retryFailureOnceMode)
	paths := testPaths(t, retryModeSettings)
	writeProgrammaticCredentials(t, paths)
	bodies := new(requestBodies)
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		position := bodies.append(body)
		payload := finalResponseSSE
		if position == 2 {
			payload = retryPartialFailureSSE
		}
		return &http.Response{
			StatusCode:       http.StatusOK,
			Body:             io.NopCloser(strings.NewReader(payload)),
			Header:           make(http.Header),
			Status:           "",
			Proto:            "",
			ProtoMajor:       0,
			ProtoMinor:       0,
			ContentLength:    0,
			TransferEncoding: nil,
			Close:            false,
			Uncompressed:     false,
			Trailer:          nil,
			Request:          nil,
			TLS:              nil,
		}, nil
	}).AnyTimes()
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildPublicExtensionFixture(t))
	defer fixture.closeOwner(t)
	// Act through the public operation with working reset delivery and a retry handler that now preserves decisions.
	failure, progress := requestProgrammaticFailure(t, fixture, userRequest("nested-observer", "first request"))
	// Assert retained parent failure stops before outer recovery or another provider request.
	require.Equal(t, "EXTENSION_FAILED", failure.Code)
	require.Contains(t, failure.Message, consumedRetryFailureText)
	require.Contains(t, failure.Message, "temporary provider failure")
	require.Zero(t, progress)
	require.Len(t, bodies.snapshot(), 2)
	_, err := os.Stat(filepath.Join(signals, ordinaryObserverLaterSignal))
	require.ErrorIs(t, err, os.ErrNotExist)
	encoded, err := os.ReadFile(filepath.Join(signals, consumedConfiguredFixtureReport))
	require.NoError(t, err)
	require.Contains(t, string(encoded), consumedRetryFailureText)
	completeProgrammaticRequest(t, fixture, userRequest("nested-healthy", "second request"))
	require.Len(t, bodies.snapshot(), 3)
	_, err = os.Stat(filepath.Join(signals, ordinaryObserverLaterSignal))
	require.NoError(t, err)
}
