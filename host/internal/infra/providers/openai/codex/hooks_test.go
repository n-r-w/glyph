//nolint:exhaustruct // Tests set only fields needed by each HTTP boundary.
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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/hooks"
	hookrunner "github.com/n-r-w/glyph/host/internal/hooks/runner"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestServiceStreamAppliesRequestHooksBeforeOneDispatch verifies sequential payload and header replacement.
func TestServiceStreamAppliesRequestHooksBeforeOneDispatch(t *testing.T) {
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
	service := hookTestService(t, runner, testProviderOptions(server), 1)

	events, err := collectStreamEvents(service, t.Context(), hookModelRequest("original instructions"), nil)
	response := terminalResponse(events)

	require.NoError(t, err)
	assert.Equal(t, model.OutcomeStop, response.Outcome)
	assert.True(t, firstSeen)
	assert.True(t, responseSeen)
	assert.Equal(t, int32(1), requests.Load())
}

// TestServiceStreamStopsBeforeDispatchOnRequestHookFailure verifies safe request-stage short circuiting.
func TestServiceStreamStopsBeforeDispatchOnRequestHookFailure(t *testing.T) {
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
				return hooks.Request{}, errors.New("secret raw request hook error")
			}
			return value, nil
		},
		func(_ context.Context, value hooks.Request) (hooks.Request, error) {
			laterCalls++
			return value, nil
		},
	}, nil)
	service := hookTestService(t, runner, testProviderOptions(server), 2)

	events, err := collectStreamEvents(service, t.Context(), hookModelRequest("instructions"), nil)
	response := terminalResponse(events)

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret")
	assert.Zero(t, laterCalls)
	assert.Zero(t, requests.Load())
	assertHookFailure(t, response, hooks.StageRequest)

	events, err = collectStreamEvents(service, t.Context(), hookModelRequest("second instructions"), nil)
	response = terminalResponse(events)
	require.NoError(t, err)
	assert.Equal(t, model.OutcomeStop, response.Outcome)
	assert.Equal(t, 1, laterCalls)
	assert.Equal(t, int32(1), requests.Load())
}

// TestServiceStreamClosesBodyOnResponseHookFailure verifies pre-decode response short circuiting.
func TestServiceStreamClosesBodyOnResponseHookFailure(t *testing.T) {
	t.Parallel()

	body := &trackingReadCloser{Reader: bytes.NewBufferString("data: " + completedEvent(`[]`) + "\n\n")}
	transport := &staticResponseTransport{body: body}
	options := defaultServiceOptions()
	options.modelBaseURL = "https://hooks.invalid"
	options.httpClient = &http.Client{Transport: transport}
	laterCalls := 0
	runner := hookrunner.New(nil, nil, []hooks.ResponseHandler{
		func(_ context.Context, value hooks.Response) error {
			assert.Equal(t, model.ProviderID(ProviderID), value.Provider)
			assert.Equal(t, model.ID("gpt-test"), value.Model)
			assert.Equal(t, http.StatusOK, value.Status)
			assert.Equal(t, "response-value", value.Headers["X-Response"][0])
			return errors.New("secret raw response hook error")
		},
		func(context.Context, hooks.Response) error {
			laterCalls++
			return nil
		},
	})
	service := hookTestService(t, runner, options, 1)
	events := make([]run.StreamEvent, 0)

	events, err := collectStreamEvents(service, t.Context(), hookModelRequest("instructions"), func(event run.StreamEvent) error {
		events = append(events, event)
		return nil
	})
	response := terminalResponse(events)

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret")
	assert.Equal(t, int32(1), transport.requests.Load())
	assert.True(t, body.closed.Load())
	assert.Zero(t, body.reads.Load())
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
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": {"text/event-stream"},
			"X-Response":   {"response-value"},
		},
		Body: transport.body,
	}, nil
}

type trackingReadCloser struct {
	io.Reader
	reads  atomic.Int32
	closed atomic.Bool
}

func (body *trackingReadCloser) Read(data []byte) (int, error) {
	body.reads.Add(1)
	return body.Reader.Read(data)
}

func (body *trackingReadCloser) Close() error {
	body.closed.Store(true)
	return nil
}

func hookTestService(t *testing.T, runner *hookrunner.Runner, options serviceOptions, calls int) *Service {
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
	return newService(Config{Hooks: runner}, credentials, interaction, options)
}

func hookModelRequest(instructions string) run.ModelRequest {
	return run.ModelRequest{
		Instructions: instructions,
		Model:        model.Descriptor{Provider: ProviderID, Model: "gpt-test"},
		History:      []agent.HistoryEntry{{Kind: agent.HistoryEntryUser, User: model.TextMessage("hello")}},
		Tools:        nil,
	}
}

func assertHookFailure(t *testing.T, response model.Response, stage hooks.Stage) {
	t.Helper()
	assert.Equal(t, model.OutcomeFailed, response.Outcome)
	assert.Equal(t, "Model request failed.", response.ErrorMessage)
	assert.Equal(t, []model.Diagnostic{{Code: "internal_hook_failed", Message: string(stage)}}, response.Diagnostics)
}
