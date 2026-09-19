//go:build integration

package codex

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"

	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestDriverStreamJoinsProviderAndFinalErrorHandlerFailures verifies final callback failure retains provider cause
// once.
func TestDriverStreamJoinsProviderAndFinalErrorHandlerFailures(t *testing.T) {
	t.Parallel()

	// Arrange authenticated credentials and one unique transport failure.
	accountID := "final-error-account"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(
		testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil,
	)
	interaction := NewMockInteraction(gomock.NewController(t))
	providerErr := errors.New("unique Codex final provider failure")
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).Return(nil, providerErr)
	options := defaultDriverOptions()
	options.modelBaseURL = "https://final-error.invalid"
	options.httpClient = &http.Client{
		Transport: transport, CheckRedirect: nil, Jar: nil, Timeout: 0,
	}
	service := newDriver(testConfig(), credentials, interaction, options)
	handlerErr := errors.New("unique Codex final error handler failure")
	callbacks := 0

	// Act by rejecting the one final provider error event.
	err := service.Stream(t.Context(), modelexecution.ProviderRequest{
		ReasoningChoice: model.ReasoningChoiceOn,
		Instructions:    "instructions",
		Model:           testModelDescriptor("gpt-test"),
		History:         nil,
		Tools:           nil,
	}, func(event modelexecution.StreamEvent) error {
		callbacks++
		assert.Equal(t, modelexecution.StreamEventError, event.Kind)
		return handlerErr
	})

	// Assert both exact causes occur once and no second terminal callback is attempted.
	require.ErrorIs(t, err, providerErr)
	require.ErrorIs(t, err, handlerErr)
	var providerFailure *modelexecution.ProviderFailureError
	assert.NotErrorAs(t, err, &providerFailure)
	assert.Equal(t, 1, strings.Count(err.Error(), providerErr.Error()))
	assert.Equal(t, 1, strings.Count(err.Error(), handlerErr.Error()))
	assert.Equal(t, 1, callbacks)
}

// TestDriverStreamJoinsSDKAndContentEndFailures verifies partial finalization retains both causes once.
func TestDriverStreamJoinsSDKAndContentEndFailures(t *testing.T) {
	t.Parallel()

	// Arrange an authenticated partial stream followed by malformed SDK input.
	accountID := "combined-stream-account"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(
		testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil,
	)
	interaction := NewMockInteraction(gomock.NewController(t))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		payload := []byte(
			"data: " + `{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"partial"}` + "\n\n",
		)
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.Header().Set("Content-Length", fmt.Sprint(len(payload)+100))
		_, err := writer.Write(payload)
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))
	handlerErr := errors.New("unique Codex ContentEnd delivery failure")
	events := make([]modelexecution.StreamEventKind, 0)

	// Act by streaming until assembler finalization reaches the failed handler.
	err := service.Stream(t.Context(), modelexecution.ProviderRequest{
		ReasoningChoice: model.ReasoningChoiceOn,
		Instructions:    "instructions",
		Model:           testModelDescriptor("gpt-test"),
		History:         nil,
		Tools:           nil,
	}, func(event modelexecution.StreamEvent) error {
		events = append(events, event.Kind)
		if event.Kind == modelexecution.StreamEventContentEnd {
			return handlerErr
		}
		return nil
	})

	// Assert both exact causes occur once and no terminal callback follows handler failure.
	require.Error(t, err)
	require.ErrorIs(t, err, handlerErr)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	assert.Equal(t, 1, strings.Count(err.Error(), handlerErr.Error()))
	assert.Equal(t, 1, strings.Count(err.Error(), io.ErrUnexpectedEOF.Error()))
	require.NotEmpty(t, events)
	assert.Equal(t, modelexecution.StreamEventContentEnd, events[len(events)-1])
	assert.NotContains(t, events, modelexecution.StreamEventDone)
	assert.NotContains(t, events, modelexecution.StreamEventError)
}

// TestDriverStreamRequestPreparationFailureRemainsPredispatch verifies local request mapping is not a provider failure.
func TestDriverStreamRequestPreparationFailureRemainsPredispatch(t *testing.T) {
	t.Parallel()

	// Arrange valid credentials and a request rejected before model dispatch.
	accountID := "request-preparation-account"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(
		testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil,
	)
	service := newDriver(
		testConfig(), credentials, NewMockInteraction(gomock.NewController(t)), defaultDriverOptions(),
	)

	// Act with missing instructions so request preparation fails before HTTP dispatch.
	_, err := collectStreamEvents(service, t.Context(), modelexecution.ProviderRequest{
		ReasoningChoice: model.ReasoningChoiceOn,
		Instructions:    "",
		Model:           testModelDescriptor("gpt-test"),
		History:         nil,
		Tools:           nil,
	}, nil)

	// Assert local request failure remains outside provider classification.
	require.ErrorContains(t, err, "request instructions are required")
	var providerFailure *modelexecution.ProviderFailureError
	assert.NotErrorAs(t, err, &providerFailure)
}

// TestDriverStreamPreservesTransportFailure verifies a transport cause reaches the returned error and terminal
// response.
func TestDriverStreamPreservesTransportFailure(t *testing.T) {
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
	transport.EXPECT().RoundTrip(gomock.Any()).Return(nil, transportErr)
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
	events, err := collectStreamEvents(service, t.Context(), modelexecution.ProviderRequest{
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
	assert.Equal(t, modelexecution.ProviderFailureTransient, providerFailure.Classification)
}

// TestDriverStreamClassifiesDeadlineTransportFailure verifies timeout identity remains transient and inspectable.
func TestDriverStreamClassifiesDeadlineTransportFailure(t *testing.T) {
	t.Parallel()

	// Arrange authenticated credentials and a transport returning a wrapped deadline sentinel.
	accountID := "deadline-account"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(
		testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil,
	)
	interaction := NewMockInteraction(gomock.NewController(t))
	transportErr := fmt.Errorf("configured timeout: %w", context.DeadlineExceeded)
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).Return(nil, transportErr)
	options := defaultDriverOptions()
	options.modelBaseURL = "https://timeout.invalid"
	options.httpClient = &http.Client{Transport: transport, CheckRedirect: nil, Jar: nil, Timeout: 0}
	service := newDriver(testConfig(), credentials, interaction, options)

	// Act through one raw provider attempt.
	_, err := collectStreamEvents(service, t.Context(), modelexecution.ProviderRequest{
		ReasoningChoice: model.ReasoningChoiceOn, Instructions: "instructions",
		Model: testModelDescriptor("gpt-test"), History: nil, Tools: nil,
	}, nil)

	// Assert the typed provider boundary retains both transient classification and timeout identity.
	var providerFailure *modelexecution.ProviderFailureError
	require.ErrorAs(t, err, &providerFailure)
	assert.Equal(t, modelexecution.ProviderFailureTransient, providerFailure.Classification)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "configured timeout")
}

// TestDriverStreamClassifiesPrematureClosure verifies a cleanly closed stream without terminal output is transient.
func TestDriverStreamClassifiesPrematureClosure(t *testing.T) {
	t.Parallel()

	// Arrange authenticated credentials and one real SSE endpoint with no terminal event.
	accountID := "premature-close-account"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(
		testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil,
	)
	interaction := NewMockInteraction(gomock.NewController(t))
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "text/event-stream")
	}))
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))

	// Act through one raw adapter invocation.
	events, err := collectStreamEvents(service, t.Context(), modelexecution.ProviderRequest{
		ReasoningChoice: model.ReasoningChoiceOn,
		Instructions:    "instructions",
		Model:           testModelDescriptor("gpt-test"),
		History:         nil,
		Tools:           nil,
	}, nil)

	// Assert the explicit source, transient classification, and no hidden SDK retry.
	require.ErrorContains(t, err, "OpenAI Codex stream ended without a terminal response")
	assert.Contains(
		t,
		terminalResponse(events).ErrorMessage.OrEmpty(),
		"OpenAI Codex stream ended without a terminal response",
	)
	var providerFailure *modelexecution.ProviderFailureError
	require.ErrorAs(t, err, &providerFailure)
	assert.Equal(t, modelexecution.ProviderFailureTransient, providerFailure.Classification)
	assert.Equal(t, int32(1), requests.Load())
}

// TestDriverStreamFailureEventsPreserveSourceWhenContentEndFails verifies each provider failure source survives
// failed active-content finalization without a later terminal callback.
func TestDriverStreamFailureEventsPreserveSourceWhenContentEndFails(t *testing.T) {
	t.Parallel()

	providerDetail := "unique Codex failure event source"
	testCases := map[string]string{
		"failed": fmt.Sprintf(`{"type":"response.failed","response":{"id":"resp","status":"failed",`+
			`"error":{"code":"server_error","message":%q},"output":[]}}`, providerDetail),
		"incomplete": fmt.Sprintf(`{"type":"response.incomplete","response":{"id":"resp",`+
			`"status":"incomplete","incomplete_details":{"reason":"content_filter"},`+
			`"error":{"code":"server_error","message":%q},"output":[]}}`, providerDetail),
		"error": fmt.Sprintf(`{"type":"error","code":"server_error","message":%q}`, providerDetail),
	}
	for name, terminalEvent := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange active streamed content followed by one provider failure event.
			accountID := "failure-event-account"
			accessToken := testJWT(t, map[string]any{
				"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
			})
			credentials := NewMockCredentials(gomock.NewController(t))
			credentials.EXPECT().Load().Return(
				testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil,
			)
			interaction := NewMockInteraction(gomock.NewController(t))
			var attempts atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				writeSSE(
					writer,
					`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"partial"}`,
					terminalEvent,
				)
			}))
			t.Cleanup(server.Close)
			service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))
			handlerErr := errors.New("unique Codex failure-event ContentEnd callback source")
			callbacks := make([]modelexecution.StreamEventKind, 0)

			// Act until content finalization rejects its callback.
			err := service.Stream(t.Context(), modelexecution.ProviderRequest{
				ReasoningChoice: model.ReasoningChoiceOn,
				Instructions:    "instructions",
				Model:           testModelDescriptor("gpt-test"),
				History:         nil,
				Tools:           nil,
			}, func(event modelexecution.StreamEvent) error {
				callbacks = append(callbacks, event.Kind)
				if event.Kind == modelexecution.StreamEventContentEnd {
					return handlerErr
				}
				return nil
			})

			// Assert both independent sources survive one attempt and callback use stops at ContentEnd.
			require.ErrorIs(t, err, handlerErr)
			assert.Equal(t, 1, strings.Count(err.Error(), providerDetail), err.Error())
			assert.Equal(t, 1, strings.Count(err.Error(), handlerErr.Error()), err.Error())
			assert.Equal(t, int64(1), attempts.Load())
			require.NotEmpty(t, callbacks)
			assert.Equal(t, modelexecution.StreamEventContentEnd, callbacks[len(callbacks)-1])
			assert.NotContains(t, callbacks, modelexecution.StreamEventError)
			assert.NotContains(t, callbacks, modelexecution.StreamEventDone)
		})
	}
}

// TestDriverStreamFailureEventPreservesSourceAcrossTerminalMergeFailure verifies a provider source survives a later
// terminal output merge failure.
func TestDriverStreamFailureEventPreservesSourceAcrossTerminalMergeFailure(t *testing.T) {
	t.Parallel()

	// Arrange completed output only at position one so terminal merge cannot fill position zero.
	accountID := "failure-merge-account"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(
		testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil,
	)
	interaction := NewMockInteraction(gomock.NewController(t))
	providerDetail := "unique Codex merge provider source"
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		writeSSE(
			writer,
			`{"type":"response.output_item.done","output_index":1,`+
				`"item":{"id":"fc-1","type":"function_call","call_id":"call-1",`+
				`"name":"read","arguments":"{}","status":"completed"}}`,
			fmt.Sprintf(`{"type":"response.failed","response":{"id":"resp","status":"failed",`+
				`"error":{"code":"server_error","message":%q},"output":[]}}`, providerDetail),
		)
	}))
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))
	callbacks := 0
	var terminal model.Response

	// Act through finalization and terminal merge.
	err := service.Stream(t.Context(), modelexecution.ProviderRequest{
		ReasoningChoice: model.ReasoningChoiceOn,
		Instructions:    "instructions",
		Model:           testModelDescriptor("gpt-test"),
		History:         nil,
		Tools:           nil,
	}, func(event modelexecution.StreamEvent) error {
		callbacks++
		if event.Kind == modelexecution.StreamEventError {
			terminal = event.Response.OrEmpty()
		}
		return nil
	})

	// Assert the provider and merge sources both reach the returned error and terminal projection.
	require.Error(t, err)
	assert.Equal(t, 1, strings.Count(err.Error(), providerDetail), err.Error())
	assert.Equal(t, 1, strings.Count(err.Error(), "noncontiguous completed output"), err.Error())
	assert.Contains(t, terminal.ErrorMessage.OrEmpty(), providerDetail)
	assert.Equal(t, int64(1), attempts.Load())
	assert.Equal(t, 3, callbacks)
}

// TestDriverStreamMixedCancellationPreservesAcquiredProviderFailure verifies cancellation does not replace an
// independently acquired typed transport failure at the driver's stream-error boundary.
func TestDriverStreamMixedCancellationPreservesAcquiredProviderFailure(t *testing.T) {
	t.Parallel()

	// Arrange an acquired typed transport failure before cancellation becomes visible to the driver.
	providerCause := fmt.Errorf("unique acquired Codex transport source: %w", syscall.ECONNRESET)
	typedProviderErr := &url.Error{Op: "POST", URL: "https://mixed-cancel.invalid", Err: providerCause}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	service := &Driver{}

	// Act once through the stream error mapper used after the provider attempt ends.
	response, sourceErr := service.streamError(ctx, typedProviderErr, newErrorCaptureTransport(http.DefaultTransport))
	err := classifyCodexFailure(ctx, sourceErr)

	// Assert aborted presentation, transient classification, and both internal causes survive the mapping.
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, providerCause)
	var providerFailure *modelexecution.ProviderFailureError
	require.ErrorAs(t, err, &providerFailure)
	assert.Equal(t, modelexecution.ProviderFailureTransient, providerFailure.Classification)
	var acquired *url.Error
	require.ErrorAs(t, err, &acquired)
	assert.Same(t, typedProviderErr, acquired)
	assert.Equal(t, model.OutcomeAborted, response.Outcome.OrEmpty())
	assert.Contains(t, response.ErrorMessage.OrEmpty(), providerCause.Error())
}

// TestModelResponsePreservesToolArgumentDecodeCause verifies SDK conversion exposes malformed tool JSON.
func TestModelResponsePreservesToolArgumentDecodeCause(t *testing.T) {
	t.Parallel()

	// Arrange one completed SDK function call with malformed arguments.
	var sdkResponse responses.Response
	require.NoError(
		t,
		json.Unmarshal(
			[]byte(`{"output":[{"type":"function_call","call_id":"call","name":"read","arguments":"{\"path\":"}]}`),
			&sdkResponse,
		),
	)

	// Act by converting the terminal SDK response.
	response, err := modelResponse(sdkResponse, model.OutcomeStop, nil)

	// Assert both conversion outputs contain the parser cause.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected EOF")
	assert.Contains(t, response.ErrorMessage.OrEmpty(), "unexpected EOF")
}

// TestModelResponseRejectsNonObjectToolArguments verifies completed SDK calls retain the prior map argument shape.
func TestModelResponseRejectsNonObjectToolArguments(t *testing.T) {
	t.Parallel()

	for name, arguments := range map[string]string{"array": `[]`, "scalar": `"value"`} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange one completed SDK function call with valid JSON outside the established map shape.
			encodedArguments, err := json.Marshal(arguments)
			require.NoError(t, err)
			payload := []byte(`{"output":[{"type":"function_call","call_id":"call","name":"read","arguments":` +
				string(encodedArguments) + `}]}`)
			var sdkResponse responses.Response
			require.NoError(t, json.Unmarshal(payload, &sdkResponse))

			// Act by converting the terminal SDK response.
			response, err := modelResponse(sdkResponse, model.OutcomeStop, nil)

			// Assert both conversion outputs contain the complete JSON shape cause.
			require.Error(t, err)
			require.ErrorContains(t, err, "unmarshal JSON")
			require.ErrorContains(t, err, "into Go map[string]interface {}")
			assert.Contains(t, response.ErrorMessage.OrEmpty(), err.Error())
		})
	}
}

// TestDriverStreamPreservesMalformedReasoningCause verifies reasoning context parser detail reaches both boundaries.
func TestDriverStreamPreservesMalformedReasoningCause(t *testing.T) {
	t.Parallel()

	// Arrange authenticated credentials and malformed stored reasoning context.
	accountID := "reasoning-account"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(
		testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil,
	)
	interaction := NewMockInteraction(gomock.NewController(t))
	service := newDriver(testConfig(), credentials, interaction, defaultDriverOptions())
	history := []agent.HistoryEntry{{
		Kind: agent.HistoryEntryModel,
		User: mo.None[model.Message](), ToolResult: mo.None[agent.ToolResult](),
		Model: mo.Some(model.Response{
			Content: []model.Content{{
				Kind: model.ContentReasoning, Text: mo.None[string](), Final: false,
				ToolCall: mo.None[model.ToolCall](),
				ProviderContext: mo.Some(model.ProviderContext{
					Source: model.ProviderContextSource{
						ProviderID:       ProviderID,
						API:              "responses",
						Model:            "gpt-test",
						CompatibilityKey: mo.None[string](),
					},
					Payload: []byte(`{"id":`),
				}),
			}},
			Outcome: mo.None[model.Outcome](), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](),
			Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
			Usage: mo.None[model.Usage](), Diagnostics: nil,
		}),
	}}

	// Act before any HTTP dispatch can occur.
	events, err := collectStreamEvents(service, t.Context(), modelexecution.ProviderRequest{
		ReasoningChoice: model.ReasoningChoiceOn,
		Instructions:    "instructions",
		Model:           testModelDescriptor("gpt-test"),
		History:         history,
		Tools:           nil,
	}, nil)
	response := terminalResponse(events)

	// Assert parser detail is visible in the returned error and terminal response.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected EOF")
	assert.Contains(t, response.ErrorMessage.OrEmpty(), "unexpected EOF")
}

// TestDriverStreamHTTPFailuresDoNotRetry verifies safe 401 and one-attempt provider errors.
func TestDriverStreamHTTPFailuresDoNotRetry(t *testing.T) {
	t.Parallel()

	// Arrange valid HTTP error JSON whose source exceeds 64 KiB before its diagnostic suffix.
	longSource := "  " + strings.Repeat("界", 22000) + " complete HTTP diagnostic suffix...  "
	testCases := map[string]struct {
		// status is the provider HTTP failure status.
		status int
		// body contains the provider's diagnostic response.
		body string
		// expectedText identifies the user-facing failure classification.
		expectedText string
		// expectedSourceText must survive in the response and returned error.
		expectedSourceText string
		// signInRequired specifies whether authentication recovery is required.
		signInRequired bool
		// retryAfter is the optional provider delay header.
		retryAfter string
		// classification is the expected source failure class.
		classification modelexecution.ProviderFailureClassification
		// delay is the expected source delay.
		delay mo.Option[time.Duration]
	}{
		"unauthorized": {
			status:             http.StatusUnauthorized,
			body:               `{"detail":"expired token"}`,
			expectedText:       signInRequiredMessage,
			expectedSourceText: "expired token",
			signInRequired:     true,
			retryAfter:         "",
			classification:     modelexecution.ProviderFailureNonRetryable,
			delay:              mo.None[time.Duration](),
		},
		"server error": {
			status:             http.StatusInternalServerError,
			body:               `{"error":{"message":"backend unavailable"}}`,
			expectedText:       "backend unavailable",
			expectedSourceText: "backend unavailable",
			signInRequired:     false,
			retryAfter:         "9",
			classification:     modelexecution.ProviderFailureTransient,
			delay:              mo.Some(9 * time.Second),
		},
		"request timeout": {
			status: http.StatusRequestTimeout, body: `{"error":{"message":"timed out"}}`,
			expectedText: "timed out", expectedSourceText: "timed out", signInRequired: false,
			retryAfter: "", classification: modelexecution.ProviderFailureTransient, delay: mo.None[time.Duration](),
		},
		"rate limited": {
			status: http.StatusTooManyRequests, body: `{"error":{"message":"slow down"}}`,
			expectedText: "slow down", expectedSourceText: "slow down", signInRequired: false,
			retryAfter: "", classification: modelexecution.ProviderFailureTransient, delay: mo.None[time.Duration](),
		},
		"bad gateway": {
			status: http.StatusBadGateway, body: `{"error":{"message":"gateway"}}`,
			expectedText: "gateway", expectedSourceText: "gateway", signInRequired: false,
			retryAfter: "", classification: modelexecution.ProviderFailureTransient, delay: mo.None[time.Duration](),
		},
		"service unavailable": {
			status: http.StatusServiceUnavailable, body: `{"error":{"message":"unavailable"}}`,
			expectedText: "unavailable", expectedSourceText: "unavailable", signInRequired: false,
			retryAfter: "", classification: modelexecution.ProviderFailureTransient, delay: mo.None[time.Duration](),
		},
		"gateway timeout": {
			status: http.StatusGatewayTimeout, body: `{"error":{"message":"gateway timeout"}}`,
			expectedText: "gateway timeout", expectedSourceText: "gateway timeout", signInRequired: false,
			retryAfter: "", classification: modelexecution.ProviderFailureTransient, delay: mo.None[time.Duration](),
		},
		"context overflow": {
			status: http.StatusBadRequest,
			body: `{"error":{"code":"context_length_exceeded",` +
				`"message":"too many tokens","type":"invalid_request_error"}}`,
			expectedText:       "too many tokens",
			expectedSourceText: "too many tokens",
			signInRequired:     false,
			retryAfter:         "",
			classification:     modelexecution.ProviderFailureContextOverflow,
			delay:              mo.None[time.Duration](),
		},
		"large unauthorized": {
			status:             http.StatusUnauthorized,
			body:               fmt.Sprintf(`{"detail":%q}`, longSource),
			expectedText:       signInRequiredMessage,
			expectedSourceText: longSource,
			signInRequired:     true,
			retryAfter:         "",
			classification:     modelexecution.ProviderFailureNonRetryable,
			delay:              mo.None[time.Duration](),
		},
		"large server error": {
			status:             http.StatusInternalServerError,
			body:               fmt.Sprintf(`{"error":{"message":%q}}`, longSource),
			expectedText:       "complete HTTP diagnostic suffix",
			expectedSourceText: longSource,
			signInRequired:     false,
			retryAfter:         "",
			classification:     modelexecution.ProviderFailureTransient,
			delay:              mo.None[time.Duration](),
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			accountID := "account"
			accessToken := testJWT(
				t,
				map[string]any{
					"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
				},
			)
			credentials := NewMockCredentials(gomock.NewController(t))
			credentials.EXPECT().
				Load().
				Return(testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil)
			interaction := NewMockInteraction(gomock.NewController(t))
			var requests atomic.Int32
			server := httptest.NewServer(
				http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
					requests.Add(1)
					writer.Header().Set("Content-Type", "application/json")
					if testCase.retryAfter != "" {
						writer.Header().Set("Retry-After", testCase.retryAfter)
					}
					writer.WriteHeader(testCase.status)
					_, _ = writer.Write([]byte(testCase.body))
				}),
			)
			t.Cleanup(server.Close)
			service := newDriver(
				testConfig(),
				credentials,
				interaction,
				testProviderOptions(server),
			)

			events, err := collectStreamEvents(
				service,
				t.Context(),
				modelexecution.ProviderRequest{
					ReasoningChoice: model.ReasoningChoiceOn,
					Instructions:    "instructions",
					Model:           testModelDescriptor("gpt-test"),
					History: []agent.HistoryEntry{
						{
							Model:      mo.None[model.Response](),
							ToolResult: mo.None[agent.ToolResult](),
							Kind:       agent.HistoryEntryUser,
							User:       mo.Some(model.TextMessage("hello")),
						},
					},
					Tools: nil,
				},
				func(modelexecution.StreamEvent) error { return nil },
			)
			response := terminalResponse(events)

			require.Error(t, err)
			assert.Equal(t, model.OutcomeFailed, response.Outcome.OrEmpty())
			assert.Contains(t, response.ErrorMessage.OrEmpty(), testCase.expectedText)
			assert.Contains(t, response.ErrorMessage.OrEmpty(), testCase.expectedSourceText)
			require.ErrorContains(t, err, testCase.expectedSourceText)
			var apiError *openai.Error
			require.ErrorAs(t, err, &apiError)
			assert.Equal(t, testCase.status, apiError.StatusCode)
			assert.Equal(t, testCase.signInRequired, errors.Is(err, ErrSignInRequired))
			var providerFailure *modelexecution.ProviderFailureError
			require.ErrorAs(t, err, &providerFailure)
			assert.Equal(t, testCase.classification, providerFailure.Classification)
			assert.Equal(t, testCase.delay, providerFailure.RetryDelay)
			assert.Equal(t, int32(1), requests.Load())
		})
	}
}

// TestDriverStreamMapsIncompleteAndFailedOutcomes verifies terminal SSE status mapping.
func TestDriverStreamMapsIncompleteAndFailedOutcomes(t *testing.T) {
	t.Parallel()

	// Arrange a Unicode source with significant whitespace and punctuation beyond the former detail limit.
	source := "  " + strings.Repeat("界", 4001) + " complete streaming failure suffix...  "
	testCases := map[string]struct {
		event           string
		expectedOutcome model.Outcome
		expectsError    bool
		classification  mo.Option[modelexecution.ProviderFailureClassification]
	}{
		"length": {
			event: `{"type":"response.incomplete","response":{"id":"resp",` +
				`"status":"incomplete",` +
				`"incomplete_details":{"reason":"max_output_tokens"},"output":[]}}`,
			expectedOutcome: model.OutcomeLength,
			expectsError:    false,
			classification:  mo.None[modelexecution.ProviderFailureClassification](),
		},
		"failure": {
			event: fmt.Sprintf(`{"type":"response.failed","response":{"id":"resp",`+
				`"status":"failed","error":{"code":"server_error","message":%q},"output":[]}}`, source),
			expectedOutcome: model.OutcomeFailed,
			expectsError:    true,
			classification:  mo.Some(modelexecution.ProviderFailureTransient),
		},
		"context overflow": {
			event: fmt.Sprintf(`{"type":"response.failed","response":{"id":"resp",`+
				`"status":"failed","error":{"code":"context_length_exceeded","message":%q},"output":[]}}`, source),
			expectedOutcome: model.OutcomeFailed,
			expectsError:    true,
			classification:  mo.Some(modelexecution.ProviderFailureContextOverflow),
		},
		"incomplete failure": {
			event: fmt.Sprintf(`{"type":"response.incomplete","response":{"id":"resp",`+
				`"status":"incomplete","incomplete_details":{"reason":"content_filter"},`+
				`"error":{"code":"server_error","message":%q},"output":[]}}`, source),
			expectedOutcome: model.OutcomeFailed,
			expectsError:    true,
			classification:  mo.Some(modelexecution.ProviderFailureTransient),
		},
		"error": {
			event:           fmt.Sprintf(`{"type":"error","code":"server_error","message":%q}`, source),
			expectedOutcome: model.OutcomeFailed,
			expectsError:    true,
			classification:  mo.Some(modelexecution.ProviderFailureTransient),
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			accountID := "account"
			accessToken := testJWT(
				t,
				map[string]any{
					"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
				},
			)
			credentials := NewMockCredentials(gomock.NewController(t))
			credentials.EXPECT().
				Load().
				Return(testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil)
			interaction := NewMockInteraction(gomock.NewController(t))
			server := httptest.NewServer(
				http.HandlerFunc(
					func(writer http.ResponseWriter, _ *http.Request) { writeSSE(writer, testCase.event) },
				),
			)
			t.Cleanup(server.Close)
			service := newDriver(
				testConfig(),
				credentials,
				interaction,
				testProviderOptions(server),
			)

			events, err := collectStreamEvents(
				service,
				t.Context(),
				modelexecution.ProviderRequest{
					ReasoningChoice: model.ReasoningChoiceOn,
					Instructions:    "instructions",
					Model:           testModelDescriptor("gpt-test"),
					History: []agent.HistoryEntry{
						{
							Model:      mo.None[model.Response](),
							ToolResult: mo.None[agent.ToolResult](),
							Kind:       agent.HistoryEntryUser,
							User:       mo.Some(model.TextMessage("hello")),
						},
					},
					Tools: nil,
				},
				func(modelexecution.StreamEvent) error { return nil },
			)
			response := terminalResponse(events)

			// Assert every failed SSE branch retains the exact source at both adapter boundaries.
			if testCase.expectsError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), source)
				assert.Contains(t, response.ErrorMessage.OrEmpty(), source)
				var providerFailure *modelexecution.ProviderFailureError
				require.ErrorAs(t, err, &providerFailure)
				assert.Equal(t, testCase.classification.MustGet(), providerFailure.Classification)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, testCase.expectedOutcome, response.Outcome.OrEmpty())
		})
	}
}

// TestDriverStreamCancellationMapsAborted verifies request cancellation terminates the SSE stream.
func TestDriverStreamCancellationMapsAborted(t *testing.T) {
	t.Parallel()

	accountID := "account"
	accessToken := testJWT(
		t,
		map[string]any{
			"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
		},
	)
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().
		Load().
		Return(testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil)
	interaction := NewMockInteraction(gomock.NewController(t))
	started := make(chan struct{})
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "text/event-stream")
			writer.WriteHeader(http.StatusOK)
			writer.(http.Flusher).Flush()
			close(started)
			<-request.Context().Done()
		}),
	)
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan struct {
		response model.Response
		err      error
	}, 1)
	go func() {
		events, err := collectStreamEvents(
			service,
			ctx,
			modelexecution.ProviderRequest{
				ReasoningChoice: model.ReasoningChoiceOn,
				Instructions:    "instructions",
				Model:           testModelDescriptor("gpt-test"),
				History: []agent.HistoryEntry{
					{
						Model:      mo.None[model.Response](),
						ToolResult: mo.None[agent.ToolResult](),
						Kind:       agent.HistoryEntryUser,
						User:       mo.Some(model.TextMessage("hello")),
					},
				},
				Tools: nil,
			},
			func(modelexecution.StreamEvent) error { return nil },
		)
		response := terminalResponse(events)
		result <- struct {
			response model.Response
			err      error
		}{
			response: response,
			err:      err,
		}
	}()
	select {
	case <-started:
		cancel()
		terminal := <-result
		require.ErrorIs(t, terminal.err, context.Canceled)
		assert.Equal(t, model.OutcomeAborted, terminal.response.Outcome.OrEmpty())
	case terminal := <-result:
		cancel()
		require.ErrorIs(t, terminal.err, context.Canceled)
		assert.Equal(t, model.OutcomeAborted, terminal.response.Outcome.OrEmpty())
	}
}
