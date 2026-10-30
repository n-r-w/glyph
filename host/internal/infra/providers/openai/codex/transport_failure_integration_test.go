//go:build integration

package codex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestDriverStreamCancellationRetainsTransportFailure checks diagnostics acquired after caller cancellation.
func TestDriverStreamCancellationRetainsTransportFailure(t *testing.T) {
	t.Parallel()
	// Arrange a transport failure acquired during cancellation.
	// Act through the provider stream.
	// Assert acquired identities and terminal diagnostics in the suite scenario.
	suite.Run(t, new(canceledTransportFailureSuite))
}

// canceledTransportFailureSuite checks diagnostics after caller cancellation.
type canceledTransportFailureSuite struct {
	// Suite owns assertions for canceled transport failures.
	suite.Suite
}

// TestScenario checks acquired transport causes at the provider boundary.
func (s *canceledTransportFailureSuite) TestScenario() {
	t := s.T()

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
		Tools:           nil, Wire: mo.None[modelexecution.
					WireCallbacks](),
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

// TestDriverStreamCancellationRetainsHTTPRejection checks received rejection diagnostics.
func TestDriverStreamCancellationRetainsHTTPRejection(t *testing.T) {
	t.Parallel()
	// Arrange received rejections followed by cancellation.
	// Act through the provider and SDK.
	// Assert status and complete diagnostics in the suite scenarios.
	suite.Run(t, new(canceledHTTPRejectionSuite))
}

// canceledHTTPRejectionSuite checks HTTP rejection capture during cancellation.
type canceledHTTPRejectionSuite struct {
	// Suite owns assertions for captured HTTP rejections.
	suite.Suite
}

// TestScenario checks status and full body text in both provider outputs.
func (s *canceledHTTPRejectionSuite) TestScenario() {
	t := s.T()
	for _, body := range []string{
		`{"error":{"message":"captured HTTP rejection source","code":"rate_limit_exceeded"},` +
			`"diagnostic":"complete rejection details"}`,
		"malformed captured HTTP rejection source: complete rejection details",
	} {
		t.Run(body, func(t *testing.T) {
			// Arrange a rejection acquired as cancellation becomes visible to the SDK.
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			transport := NewMockHTTPRoundTripper(gomock.NewController(t))
			transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
				cancel()
				return &http.Response{
					Status: diagnosticTooManyRequestsStatus, StatusCode: http.StatusTooManyRequests,
					Proto: diagnosticHTTPProtocol, ProtoMajor: 1, ProtoMinor: 1,
					Header: http.Header{diagnosticRetryAfterHeader: []string{"7"}},
					Body:   io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)),
					TransferEncoding: nil, Close: false, Uncompressed: false,
					Trailer: nil, Request: request, TLS: nil,
				}, nil
			})
			service := newTransportFailureDriver(t, transport)

			// Act through the production provider and SDK.
			events, err := collectStreamEvents(service, ctx, modelexecution.ProviderRequest{
				ReasoningChoice: model.ReasoningChoiceOn, Instructions: "instructions",
				Model: testModelDescriptor("gpt-test"), History: nil, Tools: nil, Wire: mo.None[modelexecution.
					WireCallbacks](),
			}, nil)
			response := terminalResponse(events)

			// Assert cancellation, status classification and complete rejection diagnostics.
			require.ErrorIs(t, err, context.Canceled)
			require.ErrorContains(t, err, body)
			assert.Contains(t, response.ErrorMessage.OrEmpty(), body)
			assert.Contains(t, err.Error(), "429")
			assert.Contains(t, response.ErrorMessage.OrEmpty(), "429")
			assert.Equal(t, 1, strings.Count(err.Error(), body))
			assert.Equal(t, model.OutcomeAborted, response.Outcome.OrEmpty())
			var failure *modelexecution.ProviderFailureError
			require.ErrorAs(t, err, &failure)
			assert.Equal(t, modelexecution.ProviderFailureTransient, failure.Classification)
			assert.Equal(t, 7*time.Second, failure.RetryDelay.OrEmpty())
		})
	}
}

// TestDriverStreamRetainsReadAndCloseCauses checks body failures through model execution.
func TestDriverStreamRetainsReadAndCloseCauses(t *testing.T) {
	t.Parallel()
	// Arrange acquired response-body read and cleanup failures.
	// Act through provider execution.
	// Assert both identities and terminal diagnostics in the suite scenario.
	suite.Run(t, new(bodyFailureStreamSuite))
}

// bodyFailureStreamSuite checks acquired response-body causes.
type bodyFailureStreamSuite struct {
	// Suite owns assertions for streamed body failures.
	suite.Suite
}

// TestScenario checks both response-body causes in the returned error and terminal response.
func (s *bodyFailureStreamSuite) TestScenario() {
	const internalServerErrorStatus = "500 Internal Server Error"
	t := s.T()
	// Arrange a rejected response whose body read and close both fail.
	readCause := errors.New("provider body read source")
	closeCause := errors.New("provider body cleanup source")
	body := NewMockIOReadCloser(gomock.NewController(t))
	body.EXPECT().Read(gomock.Any()).Return(0, readCause)
	body.EXPECT().Close().Return(closeCause)
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).Return(&http.Response{
		Status: internalServerErrorStatus, StatusCode: http.StatusInternalServerError,
		Proto: diagnosticHTTPProtocol, ProtoMajor: 1, ProtoMinor: 1,
		Header: make(http.Header), Body: body, ContentLength: -1,
		TransferEncoding: nil, Close: false, Uncompressed: false,
		Trailer: nil, Request: nil, TLS: nil,
	}, nil)
	service := newTransportFailureDriver(t, transport)

	// Act through the provider and SDK.
	events, err := collectStreamEvents(service, t.Context(), modelexecution.ProviderRequest{
		ReasoningChoice: model.ReasoningChoiceOn, Instructions: "instructions",
		Model: testModelDescriptor("gpt-test"), History: nil, Tools: nil, Wire: mo.None[modelexecution.
			WireCallbacks](),
	}, nil)

	// Assert both acquired identities and their text survive model execution.
	require.ErrorIs(t, err, readCause)
	require.ErrorIs(t, err, closeCause)
	response := terminalResponse(events)
	assert.Contains(t, response.ErrorMessage.OrEmpty(), readCause.Error())
	assert.Contains(t, response.ErrorMessage.OrEmpty(), closeCause.Error())
}

// newTransportFailureDriver prepares authenticated provider execution with the external transport.
func newTransportFailureDriver(t *testing.T, transport http.RoundTripper) *Driver {
	t.Helper()
	accountID := "failure-account"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(
		testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil,
	)
	interaction := NewMockInteraction(gomock.NewController(t))
	options := defaultDriverOptions()
	options.modelBaseURL = "https://failure.invalid"
	options.httpClient = &http.Client{Transport: transport, CheckRedirect: nil, Jar: nil, Timeout: 0}
	return newDriver(testConfig(), credentials, interaction, options)
}

// TestCompleteRejectionAndCloseFailure checks received diagnostics when body cleanup fails during cancellation.
func (s *bodyFailureStreamSuite) TestCompleteRejectionAndCloseFailure() {
	t := s.T()
	const rejectionBody = `{"error":{"message":"received rejection before cleanup failure"},` +
		`"diagnostic":"full source details"}`
	// Arrange a complete rejection body whose Close method returns an error.
	closeCause := errors.New("provider body cleanup source")
	body := NewMockIOReadCloser(gomock.NewController(t))
	body.EXPECT().Read(gomock.Any()).DoAndReturn(func(buffer []byte) (int, error) {
		return copy(buffer, rejectionBody), io.EOF
	})
	body.EXPECT().Close().Return(closeCause)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		cancel()
		return &http.Response{
			Status: diagnosticTooManyRequestsStatus, StatusCode: http.StatusTooManyRequests,
			Proto: diagnosticHTTPProtocol, ProtoMajor: 1, ProtoMinor: 1,
			Header: http.Header{diagnosticRetryAfterHeader: []string{"7"}}, Body: body,
			ContentLength: int64(len(rejectionBody)), TransferEncoding: nil,
			Close: false, Uncompressed: false, Trailer: nil, Request: request, TLS: nil,
		}, nil
	})
	service := newTransportFailureDriver(t, transport)

	// Act through the provider and SDK after receipt of the rejected response.
	events, err := collectStreamEvents(service, ctx, modelexecution.ProviderRequest{
		ReasoningChoice: model.ReasoningChoiceOn, Instructions: "instructions",
		Model: testModelDescriptor("gpt-test"), History: nil, Tools: nil, Wire: mo.None[modelexecution.
			WireCallbacks](),
	}, nil)
	response := terminalResponse(events)

	// Assert cancellation, the complete rejection, and the acquired close cause at both boundaries.
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, closeCause)
	require.ErrorContains(t, err, rejectionBody)
	assert.Contains(t, response.ErrorMessage.OrEmpty(), rejectionBody)
	assert.Contains(t, response.ErrorMessage.OrEmpty(), closeCause.Error())
	assert.Contains(t, response.ErrorMessage.OrEmpty(), "429")
	assert.Equal(t, 1, strings.Count(err.Error(), rejectionBody))
	assert.Equal(t, model.OutcomeAborted, response.Outcome.OrEmpty())
	var failure *modelexecution.ProviderFailureError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, modelexecution.ProviderFailureTransient, failure.Classification)
	assert.Equal(t, 7*time.Second, failure.RetryDelay.OrEmpty())
}
