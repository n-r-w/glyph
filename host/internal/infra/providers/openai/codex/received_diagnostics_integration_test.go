//go:build integration

package codex

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// receivedDiagnosticsSuite checks acquired diagnostics at the provider boundary.
type receivedDiagnosticsSuite struct {
	// Suite supplies scenario assertions.
	suite.Suite
}

// TestDriverReceivedDiagnostics exercises rejection capture and streaming cleanup.
func TestDriverReceivedDiagnostics(t *testing.T) {
	// Arrange independent received-diagnostic suite dependencies.
	t.Parallel()
	// Act through rejection capture and provider execution in the suite scenarios.
	// Assert received diagnostics and acquired error identities in the suite scenarios.
	suite.Run(t, new(receivedDiagnosticsSuite))
}

// newReceivedDiagnosticResponse supplies external response metadata.
func newReceivedDiagnosticResponse(request *http.Request, status int, body io.ReadCloser) *http.Response {
	const protocol = "HTTP/1.1"
	return &http.Response{
		Status: http.StatusText(status), StatusCode: status,
		Proto: protocol, ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{diagnosticRetryAfterHeader: []string{"7"}}, Body: body, ContentLength: -1,
		TransferEncoding: nil, Close: false, Uncompressed: false, Trailer: nil, Request: request, TLS: nil,
	}
}

// collectReceivedDiagnostics runs the driver with an external transport.
func collectReceivedDiagnostics(t *testing.T, transport http.RoundTripper) (model.Response, error) {
	t.Helper()
	events, err := collectStreamEvents(
		newTransportFailureDriver(t, transport),
		t.Context(),
		modelexecution.ProviderRequest{
			ReasoningChoice: model.ReasoningChoiceOn, Instructions: "instructions",
			Model: testModelDescriptor("gpt-test"), History: nil, Tools: nil,
		},
		nil,
	)
	return terminalResponse(events), err
}

// TestPartialRejection checks received metadata and bytes after incomplete reception.
func (s *receivedDiagnosticsSuite) TestPartialRejection() {
	t := s.T()
	const partial = `{"error":{"message":"partial received rejection source"`
	// Arrange a rejection with independent read and close failures.
	readCause := io.ErrUnexpectedEOF
	closeCause := errors.New("partial body cleanup source")
	controller := gomock.NewController(t)
	body := NewMockIOReadCloser(controller)
	body.EXPECT().
		Read(gomock.Any()).
		DoAndReturn(func(buffer []byte) (int, error) { return copy(buffer, partial), readCause })
	body.EXPECT().Close().Return(closeCause)
	transport := NewMockHTTPRoundTripper(controller)
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		return newReceivedDiagnosticResponse(request, http.StatusTooManyRequests, body), nil
	})
	// Act through the provider and SDK.
	response, err := collectReceivedDiagnostics(t, transport)
	// Assert incomplete bytes, metadata and both acquired causes survive.
	require.ErrorIs(t, err, readCause)
	require.ErrorIs(t, err, closeCause)
	assert.Contains(t, err.Error(), partial)
	assert.Contains(t, response.ErrorMessage.OrEmpty(), partial)
	assert.Contains(t, err.Error(), "429")
	failure, ok := errors.AsType[*modelexecution.ProviderFailureError](err)
	require.True(t, ok)
	assert.Equal(t, modelexecution.ProviderFailureTransient, failure.Classification)
	assert.Equal(t, 7*time.Second, failure.RetryDelay.OrEmpty())
}

// TestSDKEnvelopeDiagnostics checks the full envelope and original SDK identity.
func (s *receivedDiagnosticsSuite) TestSDKEnvelopeDiagnostics() {
	t := s.T()
	const payload = `{"error":{"message":"rejected","code":"rate_limit_exceeded"},` +
		`"diagnostic":"outer received diagnostic source"}`
	// Arrange a complete SDK rejection with outer diagnostic data.
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		return newReceivedDiagnosticResponse(
			request,
			http.StatusTooManyRequests,
			io.NopCloser(strings.NewReader(payload)),
		), nil
	})
	// Act through the normal SDK rejection route.
	response, err := collectReceivedDiagnostics(t, transport)
	// Assert the received envelope occurs once and the SDK error remains reachable.
	require.Error(t, err)
	assert.Contains(t, err.Error(), payload)
	assert.Contains(t, response.ErrorMessage.OrEmpty(), payload)
	assert.Equal(t, 1, strings.Count(err.Error(), "outer received diagnostic source"))
	assert.Equal(t, 1, strings.Count(err.Error(), "rejected"))
	assert.Equal(t, 1, strings.Count(response.ErrorMessage.OrEmpty(), "rejected"))
	apiError, ok := errors.AsType[*openai.Error](err)
	require.True(t, ok)
	assert.Equal(t, "rate_limit_exceeded", apiError.Code)
	failure, ok := errors.AsType[*modelexecution.ProviderFailureError](err)
	require.True(t, ok)
	assert.Equal(t, modelexecution.ProviderFailureTransient, failure.Classification)
	assert.Equal(t, 7*time.Second, failure.RetryDelay.OrEmpty())
}

// TestStructuredCodeClassification checks code classification when rejection cleanup fails.
func (s *receivedDiagnosticsSuite) TestStructuredCodeClassification() {
	t := s.T()
	const payload = `{"error":{"code":"context_length_exceeded","message":"context limit source"}}`
	// Arrange a coded rejection and an active caller.
	closeCause := errors.New("coded rejection cleanup source")
	controller := gomock.NewController(t)
	body := NewMockIOReadCloser(controller)
	body.EXPECT().
		Read(gomock.Any()).
		DoAndReturn(func(buffer []byte) (int, error) { return copy(buffer, payload), io.EOF })
	body.EXPECT().Close().Return(closeCause)
	transport := NewMockHTTPRoundTripper(controller)
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		return newReceivedDiagnosticResponse(request, http.StatusBadRequest, body), nil
	})
	// Act through captured-rejection handling.
	response, err := collectReceivedDiagnostics(t, transport)
	// Assert source classification, metadata and diagnostics survive.
	require.ErrorIs(t, err, closeCause)
	failure, ok := errors.AsType[*modelexecution.ProviderFailureError](err)
	require.True(t, ok)
	assert.Equal(t, modelexecution.ProviderFailureContextOverflow, failure.Classification)
	assert.Equal(t, 7*time.Second, failure.RetryDelay.OrEmpty())
	assert.Contains(t, response.ErrorMessage.OrEmpty(), payload)
}

// TestStreamingCleanupCause checks both read and close causes from HTTP success.
func (s *receivedDiagnosticsSuite) TestStreamingCleanupCause() {
	t := s.T()
	// Arrange an SSE body with independent execution and cleanup failures.
	readCause := errors.New("SSE body read source")
	closeCause := errors.New("SSE body cleanup source")
	controller := gomock.NewController(t)
	body := NewMockIOReadCloser(controller)
	body.EXPECT().Read(gomock.Any()).Return(0, readCause)
	body.EXPECT().Close().Return(closeCause)
	transport := NewMockHTTPRoundTripper(controller)
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		return newReceivedDiagnosticResponse(request, http.StatusOK, body), nil
	})
	// Act through streaming and cleanup.
	response, err := collectReceivedDiagnostics(t, transport)
	// Assert both acquired causes are present at terminal publication.
	require.ErrorIs(t, err, readCause)
	require.ErrorIs(t, err, closeCause)
	assert.Contains(t, response.ErrorMessage.OrEmpty(), closeCause.Error())
	assert.NotErrorIs(t, err, context.Canceled)
}
