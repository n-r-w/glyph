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

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// externalSelectionNestedEnvironment enables nested public operations from a real selection handler.
	externalSelectionNestedEnvironment = "GLYPH_EXTERNAL_SELECTION_NESTED"
	// externalSignalsEnvironment supplies the fixture's process-visible synchronization directory.
	externalSignalsEnvironment = "GLYPH_EXTERNAL_SIGNALS"
	// nestedSelectionCompleteSignal identifies successful completion of every nested handler assertion.
	nestedSelectionCompleteSignal = "nested-selection-complete"
	// externalSelectionObserverNestedEnvironment enables nested operations from a selection observer.
	externalSelectionObserverNestedEnvironment = "GLYPH_EXTERNAL_SELECTION_OBSERVER_NESTED"
	// nestedSelectionObserverCompleteSignal identifies successful nested observer assertions.
	nestedSelectionObserverCompleteSignal = "nested-selection-observer-complete"
)

// TestPublicSelectionObserverRunsNestedOperations verifies BUSY selection and live unrelated observer operations.
func TestPublicSelectionObserverRunsNestedOperations(t *testing.T) {
	// Arrange one real reasoning observer that checks nested selection, catalog, configured-model, and append operations.
	t.Setenv(externalSelectionObserversEnvironment, "1")
	t.Setenv(externalSelectionObserverNestedEnvironment, "1")
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

	// Act through a changed selection while the real observer starts nested Host operations.
	completeProgrammaticRequest(
		t, fixture, selectReasoningRequest(
			"nested-observer", programmaticv1.ReasoningChoice_REASONING_CHOICE_HIGH,
		),
	)

	// Assert the observer reached its signal only after all nested outcomes matched the public contract.
	_, err := os.Stat(signals + "/" + nestedSelectionObserverCompleteSignal)
	require.NoError(t, err)
}

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
