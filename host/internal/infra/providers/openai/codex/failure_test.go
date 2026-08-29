package codex

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openai/openai-go/v3/responses"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"

	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestDriverStreamJoinsProviderAndFinalErrorHandlerFailures verifies final callback failure retains provider cause once.
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
	err := service.Stream(t.Context(), run.ModelRequest{
		ReasoningChoice: model.ReasoningChoiceOn,
		Instructions:    "instructions",
		Model:           testModelDescriptor("gpt-test"),
		History:         nil,
		Tools:           nil,
	}, func(event run.StreamEvent) error {
		callbacks++
		assert.Equal(t, run.StreamEventError, event.Kind)
		return handlerErr
	})

	// Assert both exact causes occur once and no second terminal callback is attempted.
	require.ErrorIs(t, err, providerErr)
	require.ErrorIs(t, err, handlerErr)
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
		payload := []byte("data: " + `{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"partial"}` + "\n\n")
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.Header().Set("Content-Length", fmt.Sprint(len(payload)+100))
		_, err := writer.Write(payload)
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))
	handlerErr := errors.New("unique Codex ContentEnd delivery failure")
	events := make([]run.StreamEventKind, 0)

	// Act by streaming until assembler finalization reaches the failed handler.
	err := service.Stream(t.Context(), run.ModelRequest{
		ReasoningChoice: model.ReasoningChoiceOn,
		Instructions:    "instructions",
		Model:           testModelDescriptor("gpt-test"),
		History:         nil,
		Tools:           nil,
	}, func(event run.StreamEvent) error {
		events = append(events, event.Kind)
		if event.Kind == run.StreamEventContentEnd {
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
	assert.Equal(t, run.StreamEventContentEnd, events[len(events)-1])
	assert.NotContains(t, events, run.StreamEventDone)
	assert.NotContains(t, events, run.StreamEventError)
}

// TestDriverStreamPreservesTransportFailure verifies a transport cause reaches the returned error and terminal response.
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
	transportErr := errors.New("unique Codex transport failure")
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
	events, err := collectStreamEvents(service, t.Context(), run.ModelRequest{
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
	assert.NotErrorIs(t, err, ErrSignInRequired)
}

// TestModelResponsePreservesToolArgumentDecodeCause verifies SDK conversion exposes malformed tool JSON.
func TestModelResponsePreservesToolArgumentDecodeCause(t *testing.T) {
	t.Parallel()

	// Arrange one completed SDK function call with malformed arguments.
	var sdkResponse responses.Response
	require.NoError(t, json.Unmarshal([]byte(`{"output":[{"type":"function_call","call_id":"call","name":"read","arguments":"{\"path\":"}]}`), &sdkResponse))

	// Act by converting the terminal SDK response.
	response, err := modelResponse(sdkResponse, model.OutcomeStop, nil)

	// Assert both conversion outputs contain the parser cause.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected EOF")
	assert.Contains(t, response.ErrorMessage.OrEmpty(), "unexpected EOF")
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
						ProviderID: ProviderID, API: "responses", Model: "gpt-test", CompatibilityKey: mo.None[string](),
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
	events, err := collectStreamEvents(service, t.Context(), run.ModelRequest{
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

	testCases := map[string]struct {
		status       int
		body         string
		expectedText string
	}{
		"unauthorized": {
			status:       http.StatusUnauthorized,
			body:         `{"detail":"expired token"}`,
			expectedText: signInRequiredMessage,
		},
		"server error": {
			status:       http.StatusInternalServerError,
			body:         `{"error":{"message":"backend unavailable"}}`,
			expectedText: "backend unavailable",
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
			credentials.EXPECT().Load().Return(testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil)
			interaction := NewMockInteraction(gomock.NewController(t))
			var requests atomic.Int32
			server := httptest.NewServer(
				http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
					requests.Add(1)
					writer.Header().Set("Content-Type", "application/json")
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
				run.ModelRequest{
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
				func(run.StreamEvent) error { return nil },
			)
			response := terminalResponse(events)

			require.Error(t, err)
			assert.Equal(t, model.OutcomeFailed, response.Outcome.OrEmpty())
			assert.Contains(t, response.ErrorMessage.OrEmpty(), testCase.expectedText)
			assert.Equal(t, int32(1), requests.Load())
		})
	}
}

// TestDriverStreamMapsIncompleteAndFailedOutcomes verifies terminal SSE status mapping.
func TestDriverStreamMapsIncompleteAndFailedOutcomes(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		event           string
		expectedOutcome model.Outcome
		expectsError    bool
	}{
		"length": {
			event:           `{"type":"response.incomplete","response":{"id":"resp","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[]}}`,
			expectedOutcome: model.OutcomeLength,
			expectsError:    false,
		},
		"failure": {
			event:           `{"type":"response.failed","response":{"id":"resp","status":"failed","error":{"code":"server_error","message":"safe failure"},"output":[]}}`,
			expectedOutcome: model.OutcomeFailed,
			expectsError:    true,
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
			credentials.EXPECT().Load().Return(testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil)
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
				run.ModelRequest{
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
				func(run.StreamEvent) error { return nil },
			)
			response := terminalResponse(events)

			if testCase.expectsError {
				require.Error(t, err)
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
	credentials.EXPECT().Load().Return(testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil)
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
			run.ModelRequest{
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
			func(run.StreamEvent) error { return nil },
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
