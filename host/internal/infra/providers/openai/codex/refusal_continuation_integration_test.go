//go:build integration

package codex

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestRefusalContinuation checks acquired causes and semantic isolation after a refusal.
func TestRefusalContinuation(t *testing.T) {
	t.Parallel()
	const refusal = "data: " + `{"type":"response.failed","response":{"id":"refused","status":"failed",` +
		`"error":{"code":"context_length_exceeded","message":"refusal source"},"output":[]}}` + "\n\n"
	const conversion = "data: " + `{"type":"response.failed","response":{"status":"failed",` +
		`"error":{"code":"context_length_exceeded","message":"refusal source"},"output":[{"type":"function_call",` +
		`"id":"item","call_id":"call","name":"tool","arguments":"{"}]}}` + "\n\n"
	const merge = "data: " + `{"type":"response.output_item.done","output_index":1,"item":{"id":"fc",` +
		`"type":"function_call","call_id":"call","name":"read","arguments":"{}","status":"completed"}}` + "\n\n" + refusal
	const later = "data: " + `{"type":"response.output_text.delta","output_index":0,"content_index":0,` +
		`"delta":"must not be delivered"}` + "\n\n"
	for _, scenario := range []struct {
		name, payload, extra string
		clean, cancel        bool
	}{
		{name: "read_failure", payload: refusal, extra: "", clean: false, cancel: false},
		{name: "conversion_failure", payload: conversion, extra: "arguments", clean: false, cancel: false},
		{name: "merge_failure", payload: merge, extra: "noncontiguous completed output", clean: false, cancel: false},
		{name: "clean_end", payload: refusal, extra: "", clean: true, cancel: false},
		{name: "owning_cancellation", payload: refusal, extra: "", clean: false, cancel: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			// Arrange separate terminal, later event, read, and close observations.
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			controller := gomock.NewController(t)
			body := NewMockIOReadCloser(controller)
			var readCause error = io.ErrUnexpectedEOF
			if scenario.clean {
				readCause = io.EOF
			}
			if scenario.cancel {
				readCause = context.Canceled
			}
			var closeCause error = errors.New("refusal cleanup source")
			if scenario.clean {
				closeCause = nil
			}
			reading := make(chan struct{})
			if scenario.cancel {
				go func() {
					select {
					case <-reading:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			gomock.InOrder(
				body.EXPECT().
					Read(gomock.Any()).
					DoAndReturn(func(buffer []byte) (int, error) { return copy(buffer, scenario.payload), nil }),
				body.EXPECT().
					Read(gomock.Any()).
					DoAndReturn(func(buffer []byte) (int, error) { return copy(buffer, later), nil }),
				body.EXPECT().Read(gomock.Any()).DoAndReturn(func([]byte) (int, error) {
					if scenario.cancel {
						close(reading)
						<-ctx.Done()
					}
					return 0, readCause
				}),
			)
			body.EXPECT().Close().Return(closeCause)
			transport := NewMockHTTPRoundTripper(controller)
			transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
				return newReceivedDiagnosticResponse(request, http.StatusOK, body), nil
			})
			var terminal model.Response
			var deltas []string
			terminals := 0
			// Act through the concrete driver and existing typed failure mapping.
			err := newTransportFailureDriver(t, transport).Stream(ctx, modelexecution.ProviderRequest{
				ReasoningChoice: model.ReasoningChoiceOn,
				Instructions:    "instructions",
				Model:           testModelDescriptor("gpt-test"),
				History:         nil,
				Tools:           nil,
			}, func(event modelexecution.StreamEvent) error {
				if event.Kind == modelexecution.StreamEventTextDelta {
					deltas = append(deltas, event.Delta.OrEmpty())
				}
				if event.Kind == modelexecution.StreamEventError {
					terminals++
					terminal = event.Response.OrEmpty()
				}
				return nil
			})
			// Assert all acquired causes retain refusal classification and later events stay private.
			require.Error(t, err)
			assert.Contains(t, err.Error(), "refusal source")
			if !scenario.clean {
				require.ErrorIs(t, err, readCause)
			}
			if closeCause != nil {
				require.ErrorIs(t, err, closeCause)
			}
			var failure *modelexecution.ProviderFailureError
			require.ErrorAs(t, err, &failure)
			assert.Equal(t, modelexecution.ProviderFailureContextOverflow, failure.Classification)
			assert.Equal(t, 1, terminals)
			assert.Empty(t, deltas)
			assert.Contains(t, terminal.ErrorMessage.OrEmpty(), "refusal source")
			if scenario.extra != "" {
				assert.Contains(t, err.Error(), scenario.extra)
			}
			if closeCause != nil {
				assert.Equal(t, 1, strings.Count(err.Error(), closeCause.Error()))
			}
			if scenario.clean {
				assert.NotContains(t, err.Error(), missingTerminalResponseMessage)
				assert.NotContains(t, err.Error(), io.ErrUnexpectedEOF.Error())
			}
			if scenario.cancel {
				assert.Equal(t, model.OutcomeAborted, terminal.Outcome.OrEmpty())
			}
		})
	}
}

// TestTerminalImmediateExits forbids additional reads after noncontinuable terminals.
func TestTerminalImmediateExits(t *testing.T) {
	t.Parallel()
	const completed = "data: " + `{"type":"response.completed","response":{"status":"completed","output":[]}}` + "\n\n"
	const length = "data: " + `{"type":"response.incomplete","response":{"status":"incomplete",` +
		`"incomplete_details":{"reason":"max_output_tokens"},"output":[]}}` + "\n\n"
	const invalid = "data: " + `{"type":"response.completed","response":{"status":"completed",` +
		`"output":[{"type":"function_call","id":"item","call_id":"call","name":"tool","arguments":"{"}]}}` + "\n\n"
	const delta = "data: " + `{"type":"response.output_text.delta","output_index":0,"content_index":0,` +
		`"delta":"partial"}` + "\n\n"
	const refused = "data: " + `{"type":"response.failed","response":{"status":"failed",` +
		`"error":{"code":"server_error","message":"delivery refusal source"},"output":[]}}` + "\n\n"
	for _, scenario := range []struct {
		name, payload string
		delivery      modelexecution.StreamEventKind
	}{
		{name: "completed", payload: completed, delivery: 0},
		{name: "length", payload: length, delivery: 0},
		{name: "assembler_only", payload: invalid, delivery: 0},
		{name: "delta_delivery", payload: delta, delivery: modelexecution.StreamEventTextDelta},
		{name: "refusal_content_end_delivery", payload: delta + refused, delivery: modelexecution.StreamEventContentEnd},
		{name: "final_delivery", payload: completed, delivery: modelexecution.StreamEventDone},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			// Arrange one body read with no permission for a subsequent read.
			controller := gomock.NewController(t)
			body := NewMockIOReadCloser(controller)
			body.EXPECT().
				Read(gomock.Any()).
				DoAndReturn(func(buffer []byte) (int, error) { return copy(buffer, scenario.payload), nil })
			closed := false
			closeCause := errors.New("immediate cleanup source")
			handlerCause := errors.New("immediate delivery source")
			body.EXPECT().Close().DoAndReturn(func() error { closed = true; return closeCause })
			transport := NewMockHTTPRoundTripper(controller)
			transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
				return newReceivedDiagnosticResponse(request, http.StatusOK, body), nil
			})
			terminals := 0
			// Act through the terminal or semantic delivery failure.
			err := newTransportFailureDriver(t, transport).Stream(t.Context(), modelexecution.ProviderRequest{
				ReasoningChoice: model.ReasoningChoiceOn,
				Instructions:    "instructions",
				Model:           testModelDescriptor("gpt-test"),
				History:         nil,
				Tools:           nil,
			}, func(event modelexecution.StreamEvent) error {
				if event.Kind == modelexecution.StreamEventDone || event.Kind == modelexecution.StreamEventError {
					terminals++
					assert.True(t, closed)
				}
				if event.Kind == scenario.delivery ||
					(scenario.name == "final_delivery" && event.Kind == modelexecution.StreamEventError) {
					return handlerCause
				}
				return nil
			})
			// Assert cleanup and delivery ownership without any extra SDK read.
			require.ErrorIs(t, err, closeCause)
			if scenario.delivery != 0 {
				require.ErrorIs(t, err, handlerCause)
				var failure *modelexecution.ProviderFailureError
				assert.NotErrorAs(t, err, &failure)
				if scenario.name == "final_delivery" {
					assert.Equal(t, 1, terminals)
				} else {
					assert.Zero(t, terminals)
				}
			} else {
				assert.Equal(t, 1, terminals)
			}
			if scenario.name == "refusal_content_end_delivery" {
				assert.Contains(t, err.Error(), "delivery refusal source")
			}
			if scenario.name == "assembler_only" {
				assert.Contains(t, err.Error(), "arguments")
			}
		})
	}
}

// TestRefusalContinuationHTTPCancellation checks owning cancellation after refusal delivery over HTTP.
func TestRefusalContinuationHTTPCancellation(t *testing.T) {
	t.Parallel()
	// Arrange a live HTTP response that stays open after delivering a refusal.
	const accountID = "refusal-cancellation-account"
	const authClaimKey = "https://api.openai.com/auth"
	const accountIDClaimKey = "chatgpt_account_id"
	const delta = `{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"partial"}`
	const refusal = `{"type":"response.failed","response":{"status":"failed",` +
		`"error":{"code":"context_length_exceeded","message":"HTTP cancellation refusal source"},"output":[]}}`
	credentials := NewMockCredentials(gomock.NewController(t))
	token := testJWT(t, map[string]any{authClaimKey: map[string]any{accountIDClaimKey: accountID}})
	credentials.EXPECT().
		Load().
		Return(testCredentialPayload(t, token, "refresh", accountID, time.Now().Add(time.Hour)), true, nil)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeSSE(writer, delta, refusal)
		writer.(http.Flusher).Flush()
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)
	driver := newDriver(
		testConfig(),
		credentials,
		NewMockInteraction(gomock.NewController(t)),
		testProviderOptions(server),
	)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	acquired := make(chan struct{})
	result := make(chan error, 1)
	var terminal model.Response
	// Act through the real SDK connection, canceling after refusal finalizes streamed content.
	go func() {
		result <- driver.Stream(ctx, modelexecution.ProviderRequest{
			ReasoningChoice: model.ReasoningChoiceOn, Instructions: "instructions", Model: testModelDescriptor("gpt-test"),
			History: nil, Tools: nil,
		}, func(event modelexecution.StreamEvent) error {
			if event.Kind == modelexecution.StreamEventContentEnd {
				close(acquired)
			}
			if event.Kind == modelexecution.StreamEventError {
				terminal = event.Response.OrEmpty()
			}
			return nil
		})
	}()
	select {
	case <-acquired:
		cancel()
	case <-ctx.Done():
		t.Fatal("request ended before refusal delivery")
	}
	err := <-result
	// Assert cancellation and the acquired refusal retain their causes, classification, and aborted outcome.
	require.ErrorIs(t, err, context.Canceled)
	assert.Contains(t, err.Error(), "HTTP cancellation refusal source")
	var failure *modelexecution.ProviderFailureError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, modelexecution.ProviderFailureContextOverflow, failure.Classification)
	assert.Equal(t, model.OutcomeAborted, terminal.Outcome.OrEmpty())
}
