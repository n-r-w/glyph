//go:build integration

package codex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"
)

const (
	// diagnosticHTTPProtocol is the HTTP version for response diagnostic fixtures.
	diagnosticHTTPProtocol = "HTTP/1.1"
	// diagnosticTooManyRequestsStatus is the rate-limit status for response diagnostic fixtures.
	diagnosticTooManyRequestsStatus = "429 Too Many Requests"
	// diagnosticRetryAfterHeader is the retry-delay header for response diagnostic fixtures.
	diagnosticRetryAfterHeader = "Retry-After"
)

// TestErrorCaptureTransportJoinsMissingCause checks transport and terminal error identity.
func TestErrorCaptureTransportJoinsMissingCause(t *testing.T) {
	t.Parallel()
	// Arrange distinct transport and terminal causes.
	// Act by calling joinError with a separate terminal error.
	// Assert both identities in the suite scenario.
	suite.Run(t, new(transportMissingCauseSuite))
}

// transportMissingCauseSuite checks restoration of acquired causes.
type transportMissingCauseSuite struct {
	// Suite owns assertions for restored causes.
	suite.Suite
}

// TestScenario checks transport and terminal cause identity.
func (s *transportMissingCauseSuite) TestScenario() {
	t := s.T()

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
	joined := transport.joinError(terminalErr)

	// Assert both acquired causes remain inspectable.
	require.ErrorIs(t, joined, transportErr)
	require.ErrorIs(t, joined, terminalErr)
	assert.ErrorContains(t, joined, transportErr.Error())
}

// TestErrorCaptureTransportKeepsExistingCause checks SDK errors and pure cancellation stay intact.
func TestErrorCaptureTransportKeepsExistingCause(t *testing.T) {
	t.Parallel()
	// Arrange acquired causes already exposed by the SDK.
	// Act by calling joinError and providerStreamFailure in the suite scenarios.
	// Assert SDK identity and unrepeated diagnostics in the suite scenarios.
	suite.Run(t, new(transportExistingCauseSuite))
}

// transportExistingCauseSuite checks SDK cause identity.
type transportExistingCauseSuite struct {
	// Suite owns assertions for SDK error identity.
	suite.Suite
}

// TestScenario checks SDK errors and pure cancellation remain intact.
func (s *transportExistingCauseSuite) TestScenario() {
	t := s.T()

	for _, cause := range []error{errors.New("transport source"), context.Canceled} {
		t.Run(cause.Error(), func(t *testing.T) {
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
			joined := transport.joinError(sdkErr)

			// Assert the full SDK error retains its identity without repetition.
			assert.Same(t, sdkErr, joined)
			assert.Equal(t, sdkErr.Error(), joined.Error())
		})
	}
}

// TestErrorCaptureTransportRetainsReadAndCloseCauses checks both acquired body failures.
func TestErrorCaptureTransportRetainsReadAndCloseCauses(t *testing.T) {
	t.Parallel()
	// Arrange failed response-body reading and cleanup.
	// Act by calling RoundTrip and joining the captured failures with the SDK error.
	// Assert both acquired causes in the suite scenario.
	suite.Run(t, new(bodyFailureCaptureSuite))
}

// bodyFailureCaptureSuite checks response-body cause preservation.
type bodyFailureCaptureSuite struct {
	// Suite owns assertions for acquired body causes.
	suite.Suite
}

// TestScenario checks read and cleanup identities through capture and SDK joining.
func (s *bodyFailureCaptureSuite) TestScenario() {
	t := s.T()
	const partial = `{"error":{"message":"partial capture source"`
	// Arrange an incomplete rejection with two acquired body failures.
	readCause := errors.New("provider body read source")
	closeCause := errors.New("provider body cleanup source")
	body := NewMockIOReadCloser(gomock.NewController(t))
	body.EXPECT().Read(gomock.Any()).DoAndReturn(func(buffer []byte) (int, error) {
		return copy(buffer, partial), readCause
	})
	body.EXPECT().Close().Return(closeCause)
	base := NewMockHTTPRoundTripper(gomock.NewController(t))
	base.EXPECT().RoundTrip(gomock.Any()).Return(&http.Response{
		Status: diagnosticTooManyRequestsStatus, StatusCode: http.StatusTooManyRequests,
		Proto: diagnosticHTTPProtocol, ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{diagnosticRetryAfterHeader: []string{"7"}}, Body: body, ContentLength: -1,
		TransferEncoding: nil, Close: false, Uncompressed: false,
		Trailer: nil, Request: nil, TLS: nil,
	}, nil)
	transport := newErrorCaptureTransport(base)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://cleanup.invalid", nil)
	require.NoError(t, err)

	// Act at capture and restore a cause omitted by the SDK.
	response, err := transport.RoundTrip(request)
	if response != nil {
		require.NoError(t, response.Body.Close())
	}
	joined := transport.joinError(context.Canceled)

	// Assert both causes remain inspectable at each boundary.
	require.ErrorIs(t, err, readCause)
	require.ErrorIs(t, err, closeCause)
	require.ErrorIs(t, joined, readCause)
	require.ErrorIs(t, joined, closeCause)
	require.ErrorIs(t, joined, context.Canceled)
	assert.Same(t, err, transport.joinError(err))
	//nolint:testifylint // Partial received bytes are not a complete JSON document.
	assert.Equal(t, []byte(partial), transport.ErrorBody())
	//nolint:bodyclose // Capture owns the original body; this accessor borrows metadata.
	rejection := transport.errorResponse()
	require.NotNil(t, rejection)
	assert.Equal(t, http.StatusTooManyRequests, rejection.StatusCode)
	assert.Equal(t, "7", rejection.Header.Get(diagnosticRetryAfterHeader))
}

// TestReceivedRejection checks SDK identity and diagnostics for a captured rejection.
func (s *transportExistingCauseSuite) TestReceivedRejection() {
	t := s.T()
	const rejectionBody = `{"error":{"message":"SDK rejection source"}}`
	// Arrange a captured rejection already exposed by the SDK.
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://rejection.invalid", nil)
	require.NoError(t, err)
	base := NewMockHTTPRoundTripper(gomock.NewController(t))
	base.EXPECT().RoundTrip(gomock.Any()).Return(&http.Response{
		Status: diagnosticTooManyRequestsStatus, StatusCode: http.StatusTooManyRequests,
		Proto: diagnosticHTTPProtocol, ProtoMajor: 1, ProtoMinor: 1,
		Header: make(http.Header), Body: io.NopCloser(strings.NewReader(rejectionBody)),
		ContentLength: int64(len(rejectionBody)), TransferEncoding: nil,
		Close: false, Uncompressed: false, Trailer: nil, Request: request, TLS: nil,
	}, nil)
	transport := newErrorCaptureTransport(base)
	response, err := transport.RoundTrip(request)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, response.Body.Close()) })
	var apiError openai.Error
	require.NoError(t, apiError.UnmarshalJSON([]byte(`{"message":"SDK rejection source"}`)))
	apiError.Request = request
	apiError.Response = response
	apiError.StatusCode = response.StatusCode
	sdkError := fmt.Errorf("SDK request: %w", &apiError)

	// Act through capture joining and provider rejection mapping.
	joined := transport.joinError(sdkError)
	service := new(Driver)
	terminal, failure := service.providerStreamFailure(joined, transport)

	// Assert the SDK identity remains and rejection diagnostics occur once per output.
	assert.Same(t, sdkError, joined)
	var acquired *openai.Error
	require.ErrorAs(t, failure, &acquired)
	assert.Same(t, &apiError, acquired)
	assert.Equal(t, 1, strings.Count(failure.Error(), "SDK rejection source"))
	assert.Equal(t, 1, strings.Count(terminal.ErrorMessage.OrEmpty(), "SDK rejection source"))
}

// TestCompleteRejectionAndCloseFailure checks capture retains received diagnostics after cleanup failure.
func (s *bodyFailureCaptureSuite) TestCompleteRejectionAndCloseFailure() {
	t := s.T()
	const rejectionBody = `{"error":{"message":"received rejection before cleanup failure"}}`
	// Arrange a complete rejected response and one acquired cleanup cause.
	closeCause := errors.New("provider body cleanup source")
	body := NewMockIOReadCloser(gomock.NewController(t))
	body.EXPECT().Read(gomock.Any()).DoAndReturn(func(buffer []byte) (int, error) {
		return copy(buffer, rejectionBody), io.EOF
	})
	body.EXPECT().Close().Return(closeCause)
	base := NewMockHTTPRoundTripper(gomock.NewController(t))
	base.EXPECT().RoundTrip(gomock.Any()).Return(&http.Response{
		Status: diagnosticTooManyRequestsStatus, StatusCode: http.StatusTooManyRequests,
		Proto: diagnosticHTTPProtocol, ProtoMajor: 1, ProtoMinor: 1,
		Header: make(http.Header), Body: body, ContentLength: int64(len(rejectionBody)),
		TransferEncoding: nil, Close: false, Uncompressed: false,
		Trailer: nil, Request: nil, TLS: nil,
	}, nil)
	transport := newErrorCaptureTransport(base)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://cleanup.invalid", nil)
	require.NoError(t, err)

	// Act through capture and restore the cleanup cause omitted by the SDK.
	response, err := transport.RoundTrip(request)
	if response != nil {
		require.NoError(t, response.Body.Close())
	}
	joined := transport.joinError(context.Canceled)

	// Assert the received status and complete body survive beside the cleanup cause.
	require.ErrorIs(t, err, closeCause)
	require.ErrorIs(t, joined, closeCause)
	require.ErrorIs(t, joined, context.Canceled)
	//nolint:testifylint // Capture preserves exact received bytes, including JSON formatting.
	assert.Equal(t, []byte(rejectionBody), transport.ErrorBody())
	//nolint:bodyclose // RoundTrip already called Close on this borrowed body.
	metadata := transport.errorResponse()
	require.NotNil(t, metadata)
	assert.Equal(t, http.StatusTooManyRequests, metadata.StatusCode)
}
