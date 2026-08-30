//go:build integration

package codex

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/hooks"
	hookrunner "github.com/n-r-w/glyph/host/internal/hooks/runner"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestDriverStreamAppliesRequestHooksBeforeOneDispatch verifies sequential payload and header replacement.
func TestDriverStreamAppliesRequestHooksBeforeOneDispatch(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		payload, err := io.ReadAll(request.Body)
		if !assert.NoError(t, err) {
			return
		}
		assert.Contains(t, string(payload), `"instructions":"final instructions"`)
		assert.Equal(t, "final-header", request.Header.Get("X-Hook"))
		writer.Header().Set("X-Response-Hook", "observed")
		writeSSE(writer, completedEvent(`[]`))
	}))
	t.Cleanup(server.Close)
	firstSeen := false
	responseSeen := false
	runner := hookrunner.New(nil, []hooks.RequestHandler{
		func(_ context.Context, value hooks.Request) (hooks.Request, error) {
			assert.Equal(t, model.ProviderID(ProviderID), value.Provider)
			assert.Equal(t, model.ID("gpt-test"), value.Model)
			assert.Contains(t, string(value.Payload), `"instructions":"original instructions"`)
			assert.NotEmpty(t, value.Headers["Authorization"])
			value.Payload = bytes.ReplaceAll(value.Payload, []byte("original instructions"), []byte("first instructions"))
			value.Headers["X-Hook"] = []string{"first-header"}
			return value, nil
		},
		func(_ context.Context, value hooks.Request) (hooks.Request, error) {
			firstSeen = true
			assert.Contains(t, string(value.Payload), `"instructions":"first instructions"`)
			assert.Equal(t, "first-header", value.Headers["X-Hook"][0])
			value.Payload = bytes.ReplaceAll(value.Payload, []byte("first instructions"), []byte("final instructions"))
			value.Headers["X-Hook"] = []string{"final-header"}
			return value, nil
		},
	}, []hooks.ResponseHandler{
		func(_ context.Context, value hooks.Response) error {
			responseSeen = true
			assert.Equal(t, http.StatusOK, value.Status)
			assert.Equal(t, "observed", value.Headers["X-Response-Hook"][0])
			return nil
		},
	})
	service := hookTestDriver(t, runner, testProviderOptions(server), 1)

	events, err := collectStreamEvents(service, t.Context(), hookModelRequest("original instructions"), nil)
	response := terminalResponse(events)

	require.NoError(t, err)
	assert.Equal(t, model.OutcomeStop, response.Outcome.OrEmpty())
	assert.True(t, firstSeen)
	assert.True(t, responseSeen)
	assert.Equal(t, int32(1), requests.Load())
}

// TestDriverStreamStopsBeforeDispatchOnRequestHookFailure verifies request-stage failures retain their cause.
func TestDriverStreamStopsBeforeDispatchOnRequestHookFailure(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		payload, err := io.ReadAll(request.Body)
		if !assert.NoError(t, err) {
			return
		}
		assert.Contains(t, string(payload), `"instructions":"second instructions"`)
		assert.NotEqual(t, "secret transformed header", request.Header.Get("Authorization"))
		writeSSE(writer, completedEvent(`[]`))
	}))
	t.Cleanup(server.Close)
	laterCalls := 0
	var invocations atomic.Int32
	hookErr := errors.New("unique request hook error")
	runner := hookrunner.New(nil, []hooks.RequestHandler{
		func(_ context.Context, value hooks.Request) (hooks.Request, error) {
			if invocations.Add(1) == 1 {
				value.Payload = []byte("secret transformed payload")
				value.Headers["Authorization"] = []string{"secret transformed header"}
			} else {
				assert.Contains(t, string(value.Payload), `"instructions":"second instructions"`)
				assert.NotEqual(t, "secret transformed header", value.Headers["Authorization"][0])
			}
			return value, nil
		},
		func(_ context.Context, value hooks.Request) (hooks.Request, error) {
			if invocations.Load() == 1 {
				return hooks.Request{}, hookErr
			}
			return value, nil
		},
		func(_ context.Context, value hooks.Request) (hooks.Request, error) {
			laterCalls++
			return value, nil
		},
	}, nil)
	service := hookTestDriver(t, runner, testProviderOptions(server), 2)

	events, err := collectStreamEvents(service, t.Context(), hookModelRequest("instructions"), nil)
	response := terminalResponse(events)

	require.Error(t, err)
	require.ErrorIs(t, err, hookErr)
	assert.Contains(t, err.Error(), hookErr.Error())
	assert.Contains(t, response.ErrorMessage.OrEmpty(), hookErr.Error())
	assert.Zero(t, laterCalls)
	assert.Zero(t, requests.Load())
	assertHookFailure(t, response, hooks.StageRequest)

	events, err = collectStreamEvents(service, t.Context(), hookModelRequest("second instructions"), nil)
	response = terminalResponse(events)
	require.NoError(t, err)
	assert.Equal(t, model.OutcomeStop, response.Outcome.OrEmpty())
	assert.Equal(t, 1, laterCalls)
	assert.Equal(t, int32(1), requests.Load())
}

// TestDriverStreamClosesBodyOnResponseHookFailure verifies response hook and body-close failures are retained.
func TestDriverStreamClosesBodyOnResponseHookFailure(t *testing.T) {
	t.Parallel()

	closeErr := errors.New("unique response body close error")
	body := NewMockIOReadCloser(gomock.NewController(t))
	body.EXPECT().Read(gomock.Any()).Times(0)
	body.EXPECT().Close().Return(closeErr)
	transport := &staticResponseTransport{body: body, requests: atomic.Int32{}}
	options := defaultDriverOptions()
	options.modelBaseURL = "https://hooks.invalid"
	options.httpClient = &http.Client{
		Transport:     transport,
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       0,
	}
	laterCalls := 0
	hookErr := errors.New("unique response hook error")
	runner := hookrunner.New(nil, nil, []hooks.ResponseHandler{
		func(_ context.Context, value hooks.Response) error {
			assert.Equal(t, model.ProviderID(ProviderID), value.Provider)
			assert.Equal(t, model.ID("gpt-test"), value.Model)
			assert.Equal(t, http.StatusOK, value.Status)
			assert.Equal(t, "response-value", value.Headers["X-Response"][0])
			return hookErr
		},
		func(context.Context, hooks.Response) error {
			laterCalls++
			return nil
		},
	})
	service := hookTestDriver(t, runner, options, 1)
	events := make([]run.StreamEvent, 0)

	events, err := collectStreamEvents(service, t.Context(), hookModelRequest("instructions"), func(event run.StreamEvent) error {
		events = append(events, event)
		return nil
	})
	response := terminalResponse(events)

	require.Error(t, err)
	require.ErrorIs(t, err, hookErr)
	require.ErrorIs(t, err, closeErr)
	assert.Contains(t, response.ErrorMessage.OrEmpty(), hookErr.Error())
	assert.Contains(t, response.ErrorMessage.OrEmpty(), closeErr.Error())
	assert.Equal(t, int32(1), transport.requests.Load())
	assert.Zero(t, laterCalls)
	require.Len(t, events, 1)
	assert.Equal(t, run.StreamEventError, events[0].Kind)
	assertHookFailure(t, response, hooks.StageResponse)
}

type staticResponseTransport struct {
	requests atomic.Int32
	body     io.ReadCloser
}

func (transport *staticResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.requests.Add(1)
	return &http.Response{
		Status:     "",
		StatusCode: http.StatusOK,
		Proto:      "",
		ProtoMajor: 0,
		ProtoMinor: 0,
		Header: http.Header{
			"Content-Type": {"text/event-stream"},
			"X-Response":   {"response-value"},
		},
		Body:             transport.body,
		ContentLength:    0,
		TransferEncoding: nil,
		Close:            false,
		Uncompressed:     false,
		Trailer:          nil,
		Request:          nil,
		TLS:              nil,
	}, nil
}

func hookTestDriver(t *testing.T, runner *hookrunner.Runner, options driverOptions, calls int) *Driver {
	t.Helper()
	accountID := "hook-account"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(
		testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil,
	).Times(calls)
	interaction := NewMockInteraction(gomock.NewController(t))
	return newDriver(Config{Hooks: runner, Models: testConfig().Models, ReasoningCompatibilityKeys: nil}, credentials, interaction, options)
}

func hookModelRequest(instructions string) run.ModelRequest {
	return run.ModelRequest{ReasoningChoice: model.ReasoningChoiceOn,
		Instructions: instructions,
		Model:        model.Descriptor{Provider: ProviderID, Model: "gpt-test", Input: nil, ContextWindow: 0, MaxTokens: 0, ReasoningCapabilities: model.ReasoningCapabilities{}, ToolCapabilities: model.ToolCapabilities{}, Pricing: mo.None[model.Pricing]()},
		History: []agent.HistoryEntry{{Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("hello")), Model: mo.None[model.Response](),
			ToolResult: mo.None[agent.ToolResult](),
		}},
		Tools: nil,
	}
}

func assertHookFailure(t *testing.T, response model.Response, stage hooks.Stage) {
	t.Helper()
	assert.Equal(t, model.OutcomeFailed, response.Outcome.OrEmpty())
	assert.Contains(t, response.ErrorMessage.OrEmpty(), "internal hook failed at "+string(stage)+" stage")
	assert.Equal(t, []model.Diagnostic{{Code: "internal_hook_failed", Message: string(stage)}}, response.Diagnostics)
}
