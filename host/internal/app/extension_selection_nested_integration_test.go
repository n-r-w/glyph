//go:build integration

package app

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const (
	// externalSelectionNestedEnvironment enables nested public operations from a real selection handler.
	externalSelectionNestedEnvironment = "GLYPH_EXTERNAL_SELECTION_NESTED"
	// externalSignalsEnvironment supplies the fixture's process-visible synchronization directory.
	externalSignalsEnvironment = "GLYPH_EXTERNAL_SIGNALS"
	// nestedSelectionCompleteSignal identifies successful completion of every nested operation assertion.
	nestedSelectionCompleteSignal = "nested-selection-complete"
)

// TestPublicSelectionHandlerRunsNestedOperations verifies BUSY selection and live unrelated context operations.
func TestPublicSelectionHandlerRunsNestedOperations(t *testing.T) {
	// Arrange: enable one real handler that checks nested selection, catalog, configured-model, and append operations.
	t.Setenv(externalSelectionNestedEnvironment, "1")
	signals := t.TempDir()
	t.Setenv(externalSignalsEnvironment, signals)
	paths := testPaths(t, selectionCompositionSettings())
	writeProgrammaticCredentials(t, paths)
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(configuredModelResponseSSE)),
			Header: make(http.Header), Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0,
			ContentLength: 0, TransferEncoding: nil, Close: false, Uncompressed: false,
			Trailer: nil, Request: nil, TLS: nil,
		}, nil
	}).Times(1)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildPublicExtensionFixture(t))
	defer fixture.closeOwner(t)

	// Act: select a model while the real external handler starts and awaits nested Host operations.
	completeProgrammaticRequest(t, fixture, selectModelRequest("nested-selection", "openai-codex", "gpt-test"))

	// Assert: the child process reached its signal only after all nested outcomes matched the public contract.
	_, err := os.Stat(signals + "/" + nestedSelectionCompleteSignal)
	require.NoError(t, err)
}
