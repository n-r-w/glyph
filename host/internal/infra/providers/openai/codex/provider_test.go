//nolint:exhaustruct // Tests set only active tagged-union variants and relevant request fields.
package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestServiceGenerateSendsOrderedStrictRequestAndPreservesOutput verifies the complete Responses translation.
func TestServiceGenerateSendsOrderedStrictRequestAndPreservesOutput(t *testing.T) {
	t.Parallel()

	accountID := "account-ordered"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil)
	interaction := NewMockInteraction(gomock.NewController(t))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "/responses", request.URL.Path)
		assert.Equal(t, "Bearer "+accessToken, request.Header.Get("Authorization"))
		assert.Equal(t, accountID, request.Header.Get("chatgpt-account-id"))
		assert.Equal(t, "responses=experimental", request.Header.Get("OpenAI-Beta"))
		assert.Equal(t, "glyph", request.Header.Get("originator"))
		assert.Equal(t, "glyph", request.Header.Get("User-Agent"))
		var body map[string]any
		assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		assert.Equal(t, "gpt-test", body["model"])
		assert.Equal(t, false, body["store"])
		assert.Equal(t, "request instructions", body["instructions"])
		assert.Equal(t, true, body["stream"])
		assert.Contains(t, body["include"], "reasoning.encrypted_content")
		reasoning := body["reasoning"].(map[string]any)
		assert.Equal(t, "high", reasoning["effort"])
		assert.Equal(t, "auto", reasoning["summary"])
		tools := body["tools"].([]any)
		if !assert.Len(t, tools, 1) {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		functionTool := tools[0].(map[string]any)
		assert.Equal(t, "function", functionTool["type"])
		assert.Equal(t, true, functionTool["strict"])
		assert.Equal(t, "read", functionTool["name"])
		input := body["input"].([]any)
		if !assert.Len(t, input, 6) {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.Equal(t, []string{
			"message", "reasoning", "message", "function_call", "function_call_output", "message",
		}, inputTypes(input))
		assert.Equal(t, "user", input[0].(map[string]any)["role"])
		assert.Equal(t, "r-old", input[1].(map[string]any)["id"])
		assert.Equal(t, "assistant", input[2].(map[string]any)["role"])
		assert.Equal(t, "call-old", input[3].(map[string]any)["call_id"])
		assert.Equal(t, "call-old", input[4].(map[string]any)["call_id"])
		assert.Equal(t, "user", input[5].(map[string]any)["role"])
		writeSSE(writer,
			`{"type":"response.output_text.delta","output_index":1,"content_index":0,"delta":"ans"}`,
			`{"type":"response.output_text.delta","output_index":1,"content_index":0,"delta":"wer"}`,
			completedEvent(`[
				{"id":"r-new","type":"reasoning","encrypted_content":"enc-new","summary":[{"type":"summary_text","text":"summary"}]},
				{"id":"m-new","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"answer","annotations":[],"logprobs":[]}]},
				{"id":"fc-new","type":"function_call","call_id":"call-new","name":"read","arguments":"{\"path\":\"file.txt\"}","status":"completed"}
			]`),
		)
	}))
	t.Cleanup(server.Close)
	options := testProviderOptions(server)
	service := newService(Config{Model: "gpt-test", ThinkingLevel: "high"}, credentials, interaction, options)
	updates := make([]run.ModelUpdate, 0)
	history := []agent.HistoryEntry{
		{Kind: agent.HistoryEntryUser, User: agent.UserMessage{Text: "first"}},
		{Kind: agent.HistoryEntryModel, Model: agent.ModelResponse{
			Items: []agent.ModelItem{
				{Kind: agent.ModelItemProviderContext, ProviderContext: agent.ProviderContext{ProviderID: ProviderID, Payload: []byte(`{"id":"r-old","encrypted_content":"enc-old","summary":["old"]}`)}},
				{Kind: agent.ModelItemText, Text: "prior"},
				{Kind: agent.ModelItemToolCall, ToolCall: agent.ToolCall{ID: "call-old", Name: "read", Arguments: map[string]any{"path": "old.txt"}}},
			},
			Outcome: agent.ModelOutcomeToolUse,
		}},
		{Kind: agent.HistoryEntryToolResult, ToolResult: agent.ToolResult{CallID: "call-old", ToolName: "read", Content: "old data", IsError: false}},
		{Kind: agent.HistoryEntryUser, User: agent.UserMessage{Text: "next"}},
	}

	response, err := service.Generate(t.Context(), run.ModelRequest{
		Instructions: "request instructions",
		History:      history,
		Tools: []tool.Descriptor{{
			Name: "read", Description: "Read a file.",
			InputSchemaJSON: []byte(`{"type":"object","properties":{"path":{"type":"string","description":"File path."}},"required":["path"],"additionalProperties":false}`),
		}},
	}, func(update run.ModelUpdate) error {
		updates = append(updates, update)
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, []run.ModelUpdate{{Position: 1, Delta: "ans"}, {Position: 1, Delta: "wer"}}, updates)
	assert.Equal(t, agent.ModelOutcomeToolUse, response.Outcome)
	require.Len(t, response.Items, 3)
	assert.Equal(t, agent.ModelItemProviderContext, response.Items[0].Kind)
	assert.Equal(t, ProviderID, response.Items[0].ProviderContext.ProviderID)
	assert.Equal(t, agent.ModelItemText, response.Items[1].Kind)
	assert.Equal(t, "answer", response.Items[1].Text)
	assert.Equal(t, agent.ModelItemToolCall, response.Items[2].Kind)
	assert.Equal(t, map[string]any{"path": "file.txt"}, response.Items[2].ToolCall.Arguments)
}

// TestServiceGenerateStreamsRefusalDeltas verifies refusals use the provider-neutral text update path.
func TestServiceGenerateStreamsRefusalDeltas(t *testing.T) {
	t.Parallel()

	accountID := "account-refusal"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(
		testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil,
	)
	interaction := NewMockInteraction(gomock.NewController(t))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeSSE(writer,
			`{"type":"response.refusal.delta","output_index":1,"content_index":0,"delta":"I can"}`,
			`{"type":"response.refusal.delta","output_index":1,"content_index":0,"delta":"not help"}`,
			completedEvent(`[
				{"id":"r-refusal","type":"reasoning","encrypted_content":"enc-refusal","summary":[]},
				{"id":"m-refusal","type":"message","role":"assistant","status":"completed","content":[{"type":"refusal","refusal":"I cannot help"}]}
			]`),
		)
	}))
	t.Cleanup(server.Close)
	service := newService(testConfig("gpt-test", "high"), credentials, interaction, testProviderOptions(server))
	updates := make([]run.ModelUpdate, 0, 2)

	response, err := service.Generate(t.Context(), run.ModelRequest{
		Instructions: "instructions",
		History:      []agent.HistoryEntry{{Kind: agent.HistoryEntryUser, User: agent.UserMessage{Text: "request"}}},
		Tools:        nil,
	}, func(update run.ModelUpdate) error {
		updates = append(updates, update)
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, []run.ModelUpdate{
		{Position: 1, Delta: "I can"},
		{Position: 1, Delta: "not help"},
	}, updates)
	require.Len(t, response.Items, 2)
	assert.Equal(t, agent.ModelItemProviderContext, response.Items[0].Kind)
	assert.Equal(t, agent.ModelItemText, response.Items[1].Kind)
	assert.Equal(t, "I cannot help", response.Items[1].Text)
}

// TestServiceGenerateRejectsMissingEncryptedReasoning verifies stateless replay fails before HTTP.
func TestServiceGenerateRejectsMissingEncryptedReasoning(t *testing.T) {
	t.Parallel()

	accountID := "account"
	accessToken := testJWT(t, map[string]any{"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID}})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil)
	interaction := NewMockInteraction(gomock.NewController(t))
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	t.Cleanup(server.Close)
	service := newService(Config{Model: "model", ThinkingLevel: ""}, credentials, interaction, testProviderOptions(server))
	history := []agent.HistoryEntry{{
		Kind: agent.HistoryEntryModel,
		Model: agent.ModelResponse{Items: []agent.ModelItem{{
			Kind:            agent.ModelItemProviderContext,
			ProviderContext: agent.ProviderContext{ProviderID: ProviderID, Payload: []byte(`{"id":"r","encrypted_content":"","summary":[]}`)},
		}}},
	}}

	response, err := service.Generate(t.Context(), run.ModelRequest{
		Instructions: "instructions", History: history, Tools: nil,
	}, func(run.ModelUpdate) error { return nil })

	require.Error(t, err)
	assert.Equal(t, agent.ModelOutcomeFailed, response.Outcome)
	assert.Contains(t, response.ErrorMessage, "encrypted reasoning")
	assert.Zero(t, requests.Load())
}

// TestServiceGenerateOmitsAbsentReasoning verifies user-only history does not synthesize context.
func TestServiceGenerateOmitsAbsentReasoning(t *testing.T) {
	t.Parallel()

	accountID := "account"
	accessToken := testJWT(t, map[string]any{"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID}})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil)
	interaction := NewMockInteraction(gomock.NewController(t))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		input := body["input"].([]any)
		assert.Equal(t, []string{"message"}, inputTypes(input))
		writeSSE(writer, completedEvent(`[{"id":"m","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"done","annotations":[],"logprobs":[]}]}]`))
	}))
	t.Cleanup(server.Close)
	service := newService(Config{Model: "model", ThinkingLevel: ""}, credentials, interaction, testProviderOptions(server))

	response, err := service.Generate(t.Context(), run.ModelRequest{
		Instructions: "instructions",
		History:      []agent.HistoryEntry{{Kind: agent.HistoryEntryUser, User: agent.UserMessage{Text: "hello"}}},
		Tools:        nil,
	}, func(run.ModelUpdate) error { return nil })

	require.NoError(t, err)
	assert.Equal(t, agent.ModelOutcomeStop, response.Outcome)
}

// TestServiceGenerateRefreshesAtThresholdAndPersistsRotation verifies fresh request authorization.
func TestServiceGenerateRefreshesAtThresholdAndPersistsRotation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	accountID := "account-refresh"
	oldAccess := testJWT(t, map[string]any{"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID}})
	newAccess := testJWT(t, map[string]any{"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID}, "rotated": true})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(testCredentialPayload(t, oldAccess, "refresh-old", accountID, now.Add(5*time.Minute)), true, nil)
	credentials.EXPECT().Save(gomock.Any()).DoAndReturn(func(payload []byte) error {
		var rotated oauthCredentials
		require.NoError(t, json.Unmarshal(payload, &rotated))
		assert.Equal(t, newAccess, rotated.AccessToken)
		assert.Equal(t, "refresh-new", rotated.RefreshToken)
		assert.Equal(t, accountID, rotated.AccountID)
		return nil
	})
	interaction := NewMockInteraction(gomock.NewController(t))
	var tokenRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/token":
			tokenRequests.Add(1)
			var body map[string]string
			assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			assert.Equal(t, "refresh_token", body["grant_type"])
			assert.Equal(t, "refresh-old", body["refresh_token"])
			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(writer, `{"access_token":%q,"refresh_token":"refresh-new","expires_in":3600}`, newAccess)
		case "/responses":
			assert.Equal(t, "Bearer "+newAccess, request.Header.Get("Authorization"))
			assert.Equal(t, accountID, request.Header.Get("chatgpt-account-id"))
			writeSSE(writer, completedEvent(`[{"id":"m","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"done","annotations":[],"logprobs":[]}]}]`))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	options := testProviderOptions(server)
	options.tokenURL = server.URL + "/token"
	options.now = func() time.Time { return now }
	service := newService(Config{Model: "model", ThinkingLevel: ""}, credentials, interaction, options)

	response, err := service.Generate(t.Context(), run.ModelRequest{
		Instructions: "instructions",
		History:      []agent.HistoryEntry{{Kind: agent.HistoryEntryUser, User: agent.UserMessage{Text: "hello"}}},
		Tools:        nil,
	}, func(run.ModelUpdate) error { return nil })

	require.NoError(t, err)
	assert.Equal(t, agent.ModelOutcomeStop, response.Outcome)
	assert.Equal(t, int32(1), tokenRequests.Load())
}

// TestServiceGenerateSkipsRefreshOutsideThreshold verifies six remaining minutes use loaded credentials.
func TestServiceGenerateSkipsRefreshOutsideThreshold(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	accountID := "account-fresh"
	accessToken := testJWT(t, map[string]any{"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID}})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(testCredentialPayload(t, accessToken, "refresh", accountID, now.Add(6*time.Minute)), true, nil)
	interaction := NewMockInteraction(gomock.NewController(t))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "Bearer "+accessToken, request.Header.Get("Authorization"))
		writeSSE(writer, completedEvent(`[{"id":"m","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"done","annotations":[],"logprobs":[]}]}]`))
	}))
	t.Cleanup(server.Close)
	options := testProviderOptions(server)
	options.now = func() time.Time { return now }
	service := newService(Config{Model: "model", ThinkingLevel: ""}, credentials, interaction, options)

	_, err := service.Generate(t.Context(), run.ModelRequest{
		Instructions: "instructions",
		History:      []agent.HistoryEntry{{Kind: agent.HistoryEntryUser, User: agent.UserMessage{Text: "hello"}}},
		Tools:        nil,
	}, func(run.ModelUpdate) error { return nil })

	require.NoError(t, err)
}

// TestServiceGenerateMissingCredentialsDoesNotStartOAuth verifies headless failure requires no interaction.
func TestServiceGenerateMissingCredentialsDoesNotStartOAuth(t *testing.T) {
	t.Parallel()

	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(nil, false, nil)
	interaction := NewMockInteraction(gomock.NewController(t))
	service := New(testConfig("model", ""), credentials, interaction)

	response, err := service.Generate(
		t.Context(),
		run.ModelRequest{
			Instructions: "instructions",
			History: []agent.HistoryEntry{{
				Kind: agent.HistoryEntryUser, User: agent.UserMessage{Text: "hello"},
			}},
			Tools: nil,
		},
		func(run.ModelUpdate) error { return nil },
	)

	require.ErrorIs(t, err, ErrSignInRequired)
	assert.Equal(t, agent.ModelOutcomeFailed, response.Outcome)
	assert.Equal(t, signInRequiredMessage, response.ErrorMessage)
}

// TestServiceGenerateLoadsCredentialsForEveryRequest verifies access data is never cached across model calls.
func TestServiceGenerateLoadsCredentialsForEveryRequest(t *testing.T) {
	t.Parallel()

	accountID := "account-fresh-load"
	firstAccess := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
		"request":                     1,
	})
	secondAccess := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
		"request":                     2,
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	gomock.InOrder(
		credentials.EXPECT().Load().Return(
			testCredentialPayload(t, firstAccess, "refresh", accountID, time.Now().Add(time.Hour)), true, nil,
		),
		credentials.EXPECT().Load().Return(
			testCredentialPayload(t, secondAccess, "refresh", accountID, time.Now().Add(time.Hour)), true, nil,
		),
	)
	interaction := NewMockInteraction(gomock.NewController(t))
	authorizations := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authorizations <- request.Header.Get("Authorization")
		writeSSE(writer, completedEvent(
			`[{"id":"m","type":"message","role":"assistant","status":"completed","content":[]}]`,
		))
	}))
	t.Cleanup(server.Close)
	service := newService(
		testConfig("model", ""), credentials, interaction, testProviderOptions(server),
	)
	request := run.ModelRequest{
		Instructions: "instructions",
		History:      []agent.HistoryEntry{{Kind: agent.HistoryEntryUser, User: agent.UserMessage{Text: "hello"}}},
		Tools:        nil,
	}

	_, firstErr := service.Generate(t.Context(), request, func(run.ModelUpdate) error { return nil })
	_, secondErr := service.Generate(t.Context(), request, func(run.ModelUpdate) error { return nil })

	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	assert.Equal(t, "Bearer "+firstAccess, <-authorizations)
	assert.Equal(t, "Bearer "+secondAccess, <-authorizations)
}

// TestServiceGenerateHTTPFailuresDoNotRetry verifies safe 401 and one-attempt provider errors.
func TestServiceGenerateHTTPFailuresDoNotRetry(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		status       int
		body         string
		expectedText string
	}{
		"unauthorized": {status: http.StatusUnauthorized, body: `{"detail":"expired token"}`, expectedText: signInRequiredMessage},
		"server error": {status: http.StatusInternalServerError, body: `{"error":{"message":"backend unavailable"}}`, expectedText: "backend unavailable"},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			accountID := "account"
			accessToken := testJWT(t, map[string]any{"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID}})
			credentials := NewMockCredentials(gomock.NewController(t))
			credentials.EXPECT().Load().Return(testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil)
			interaction := NewMockInteraction(gomock.NewController(t))
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(testCase.status)
				_, _ = writer.Write([]byte(testCase.body))
			}))
			t.Cleanup(server.Close)
			service := newService(Config{Model: "model", ThinkingLevel: ""}, credentials, interaction, testProviderOptions(server))

			response, err := service.Generate(t.Context(), run.ModelRequest{
				Instructions: "instructions",
				History:      []agent.HistoryEntry{{Kind: agent.HistoryEntryUser, User: agent.UserMessage{Text: "hello"}}},
				Tools:        nil,
			}, func(run.ModelUpdate) error { return nil })

			require.Error(t, err)
			assert.Equal(t, agent.ModelOutcomeFailed, response.Outcome)
			assert.Contains(t, response.ErrorMessage, testCase.expectedText)
			assert.Equal(t, int32(1), requests.Load())
		})
	}
}

// TestServiceGenerateMapsIncompleteAndFailedOutcomes verifies terminal SSE status mapping.
func TestServiceGenerateMapsIncompleteAndFailedOutcomes(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		event           string
		expectedOutcome agent.ModelOutcome
		expectsError    bool
	}{
		"length": {
			event:           `{"type":"response.incomplete","response":{"id":"resp","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[]}}`,
			expectedOutcome: agent.ModelOutcomeLength,
			expectsError:    false,
		},
		"failure": {
			event:           `{"type":"response.failed","response":{"id":"resp","status":"failed","error":{"code":"server_error","message":"safe failure"},"output":[]}}`,
			expectedOutcome: agent.ModelOutcomeFailed,
			expectsError:    true,
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			accountID := "account"
			accessToken := testJWT(t, map[string]any{"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID}})
			credentials := NewMockCredentials(gomock.NewController(t))
			credentials.EXPECT().Load().Return(testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil)
			interaction := NewMockInteraction(gomock.NewController(t))
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writeSSE(writer, testCase.event) }))
			t.Cleanup(server.Close)
			service := newService(Config{Model: "model", ThinkingLevel: ""}, credentials, interaction, testProviderOptions(server))

			response, err := service.Generate(t.Context(), run.ModelRequest{
				Instructions: "instructions",
				History:      []agent.HistoryEntry{{Kind: agent.HistoryEntryUser, User: agent.UserMessage{Text: "hello"}}},
				Tools:        nil,
			}, func(run.ModelUpdate) error { return nil })

			if testCase.expectsError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, testCase.expectedOutcome, response.Outcome)
		})
	}
}

// TestServiceGenerateCancellationMapsAborted verifies request cancellation terminates the SSE stream.
func TestServiceGenerateCancellationMapsAborted(t *testing.T) {
	t.Parallel()

	accountID := "account"
	accessToken := testJWT(t, map[string]any{"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID}})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil)
	interaction := NewMockInteraction(gomock.NewController(t))
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		writer.(http.Flusher).Flush()
		close(started)
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)
	service := newService(Config{Model: "model", ThinkingLevel: ""}, credentials, interaction, testProviderOptions(server))
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan struct {
		response agent.ModelResponse
		err      error
	}, 1)
	go func() {
		response, err := service.Generate(ctx, run.ModelRequest{
			Instructions: "instructions",
			History:      []agent.HistoryEntry{{Kind: agent.HistoryEntryUser, User: agent.UserMessage{Text: "hello"}}},
			Tools:        nil,
		}, func(run.ModelUpdate) error { return nil })
		result <- struct {
			response agent.ModelResponse
			err      error
		}{response: response, err: err}
	}()
	select {
	case <-started:
		cancel()
		terminal := <-result
		require.ErrorIs(t, terminal.err, context.Canceled)
		assert.Equal(t, agent.ModelOutcomeAborted, terminal.response.Outcome)
	case terminal := <-result:
		cancel()
		require.ErrorIs(t, terminal.err, context.Canceled)
		assert.Equal(t, agent.ModelOutcomeAborted, terminal.response.Outcome)
	}
}

// TestServiceCheckAuthenticationUsesProviderOwnedClassification verifies Host preflight ownership.
func TestServiceCheckAuthenticationUsesProviderOwnedClassification(t *testing.T) {
	t.Parallel()

	t.Run("usable credentials", func(t *testing.T) {
		t.Parallel()
		accountID := "account"
		accessToken := testJWT(t, map[string]any{
			"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
		})
		credentials := NewMockCredentials(gomock.NewController(t))
		credentials.EXPECT().Load().Return(
			testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil,
		)
		interaction := NewMockInteraction(gomock.NewController(t))
		service := newService(testConfig("model", ""), credentials, interaction, defaultServiceOptions())

		err := service.CheckAuthentication(t.Context())

		require.NoError(t, err)
	})

	t.Run("missing credentials", func(t *testing.T) {
		t.Parallel()
		credentials := NewMockCredentials(gomock.NewController(t))
		credentials.EXPECT().Load().Return(nil, false, nil)
		interaction := NewMockInteraction(gomock.NewController(t))
		service := newService(testConfig("model", ""), credentials, interaction, defaultServiceOptions())

		err := service.CheckAuthentication(t.Context())

		require.ErrorIs(t, err, ErrSignInRequired)
		assert.True(t, service.IsSignInRequired(err))
	})

	t.Run("malformed credentials", func(t *testing.T) {
		t.Parallel()
		credentials := NewMockCredentials(gomock.NewController(t))
		credentials.EXPECT().Load().Return([]byte("not-json"), true, nil)
		interaction := NewMockInteraction(gomock.NewController(t))
		service := newService(testConfig("model", ""), credentials, interaction, defaultServiceOptions())

		err := service.CheckAuthentication(t.Context())

		require.ErrorIs(t, err, ErrSignInRequired)
		assert.True(t, service.IsSignInRequired(err))
	})
}

// testConfig creates one complete provider configuration fixture.
func testConfig(model, thinkingLevel string) Config {
	return Config{Model: model, ThinkingLevel: thinkingLevel}
}

// testProviderOptions points both SDK and token HTTP calls at one test server.
func testProviderOptions(server *httptest.Server) serviceOptions {
	options := defaultServiceOptions()
	options.modelBaseURL = server.URL
	options.httpClient = server.Client()
	return options
}

// testCredentialPayload encodes one provider-owned credential fixture.
func testCredentialPayload(t *testing.T, accessToken, refreshToken, accountID string, expiresAt time.Time) []byte {
	t.Helper()
	payload, err := json.Marshal(oauthCredentials{
		AccessToken: accessToken, RefreshToken: refreshToken, AccountID: accountID, ExpiresAt: expiresAt,
	})
	require.NoError(t, err)
	return payload
}

// inputTypes returns the ordered Responses item discriminators from a captured request.
func inputTypes(input []any) []string {
	result := make([]string, len(input))
	for index, item := range input {
		result[index], _ = item.(map[string]any)["type"].(string)
	}
	return result
}

// completedEvent constructs one successful terminal Responses SSE event.
func completedEvent(output string) string {
	return `{"type":"response.completed","response":{"id":"resp","status":"completed","output":` + output + `}}`
}

// writeSSE writes typed SDK events without a custom parser in production.
func writeSSE(writer http.ResponseWriter, events ...string) {
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.WriteHeader(http.StatusOK)
	for _, event := range events {
		var compact bytes.Buffer
		if err := json.Compact(&compact, []byte(event)); err != nil {
			panic("invalid SSE test fixture: " + event)
		}
		_, _ = fmt.Fprintf(writer, "data: %s\n\n", compact.String())
	}
	_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
}
