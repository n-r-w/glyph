//go:build integration

package codex

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// streamCleanupSuite checks cleanup before terminal delivery on every stream exit.
type streamCleanupSuite struct {
	// Suite supplies scenario assertions.
	suite.Suite
}

// TestDriverStreamCleanup checks completion, delivery failure and cancellation cleanup.
func TestDriverStreamCleanup(t *testing.T) {
	// Arrange independent stream-cleanup suite dependencies.
	t.Parallel()
	// Act through provider execution in the suite scenarios.
	// Assert cleanup errors, cancellation and response data in the suite scenarios.
	suite.Run(t, new(streamCleanupSuite))
}

// TestTerminalExits checks acquired cleanup causes and response data on terminal exits.
func (s *streamCleanupSuite) TestTerminalExits() {
	const completed = "data: " + `{"type":"response.completed","response":{"id":"response-id",` +
		`"model":"gpt-test","status":"completed","output":[]}}` + "\n\n"
	const delta = "data: " + `{"type":"response.output_text.delta","output_index":0,` +
		`"content_index":0,"delta":"partial"}` + "\n\n"
	const invalid = "data: " + `{"type":"response.completed","response":{"status":"completed",` +
		`"output":[{"type":"function_call","id":"item","call_id":"call","name":"tool","arguments":"{"}]}}` + "\n\n"
	for _, scenario := range []struct {
		name               string
		payload            string
		cancel             bool
		handlerFailure     bool
		expectedResponseID string
	}{
		{name: "completed", payload: completed, cancel: false, handlerFailure: false, expectedResponseID: "response-id"},
		{name: "assembler_failure", payload: invalid, cancel: false, handlerFailure: false, expectedResponseID: ""},
		{name: "handler_failure", payload: delta, cancel: false, handlerFailure: true, expectedResponseID: ""},
		{name: "canceled", payload: "", cancel: true, handlerFailure: false, expectedResponseID: ""},
		{name: "pure_canceled_with_cleanup", payload: "", cancel: true, handlerFailure: false, expectedResponseID: ""},
	} {
		s.Run(scenario.name, func() {
			t := s.T()
			// Arrange a success-status stream with a separately acquired cleanup cause.
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			closeCause := errors.New("stream cleanup source")
			handlerCause := errors.New("stream handler source")
			readCause := errors.New("stream read source")
			if scenario.name == "pure_canceled_with_cleanup" {
				readCause = context.Canceled
			}
			reader := strings.NewReader(scenario.payload)
			closed := false
			controller := gomock.NewController(t)
			body := NewMockIOReadCloser(controller)
			body.EXPECT().Read(gomock.Any()).DoAndReturn(func(buffer []byte) (int, error) {
				if scenario.cancel {
					cancel()
					return 0, readCause
				}
				return reader.Read(buffer)
			}).AnyTimes()
			body.EXPECT().Close().DoAndReturn(func() error { closed = true; return closeCause })
			transport := NewMockHTTPRoundTripper(controller)
			transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
				return newReceivedDiagnosticResponse(request, http.StatusOK, body), nil
			})
			terminalCount := 0
			var response model.Response
			// Act through the provider with a handler that checks whether the response body is closed.
			err := newTransportFailureDriver(t, transport).Stream(ctx, modelexecution.ProviderRequest{
				ReasoningChoice: model.ReasoningChoiceOn,
				Instructions:    "instructions",
				Model:           testModelDescriptor("gpt-test"),
				History:         nil,
				Tools:           nil, Wire: mo.None[modelexecution.
							WireCallbacks](),
			}, func(event modelexecution.StreamEvent) error {
				if event.Kind == modelexecution.StreamEventDone || event.Kind == modelexecution.StreamEventError {
					terminalCount++
					assert.True(t, closed)
					response = event.Response.OrEmpty()
				}
				if scenario.handlerFailure {
					return handlerCause
				}
				return nil
			})
			// Assert the provider joins the cleanup error before terminal delivery and preserves cancellation and response data.
			require.ErrorIs(t, err, closeCause)
			assert.Equal(t, 1, strings.Count(err.Error(), closeCause.Error()))
			if scenario.handlerFailure {
				require.ErrorIs(t, err, handlerCause)
				assert.Zero(t, terminalCount)
			} else {
				assert.Equal(t, 1, terminalCount)
				assert.Contains(t, response.ErrorMessage.OrEmpty(), closeCause.Error())
				assert.Equal(t, scenario.expectedResponseID, response.ResponseID.OrEmpty())
			}
			if scenario.cancel {
				require.ErrorIs(t, err, context.Canceled)
				require.ErrorIs(t, err, readCause)
				assert.Equal(t, model.OutcomeAborted, response.Outcome.OrEmpty())
				assert.Contains(t, response.ErrorMessage.OrEmpty(), readCause.Error())
			} else {
				require.NotErrorIs(t, err, context.Canceled)
			}
			if scenario.name == "assembler_failure" {
				assert.Contains(t, err.Error(), "arguments")
			}
		})
	}
}

// TestFinalizationDeliveryFailure checks read, finalization-handler and cleanup causes together.
func (s *streamCleanupSuite) TestFinalizationDeliveryFailure() {
	t := s.T()
	const delta = "data: " + `{"type":"response.output_text.delta","output_index":0,` +
		`"content_index":0,"delta":"partial"}` + "\n\n"
	// Arrange partial content followed by a read failure and failing cleanup.
	readCause := io.ErrUnexpectedEOF
	handlerCause := errors.New("content end source")
	closeCause := errors.New("finalization cleanup source")
	controller := gomock.NewController(t)
	body := NewMockIOReadCloser(controller)
	gomock.InOrder(
		body.EXPECT().
			Read(gomock.Any()).
			DoAndReturn(func(buffer []byte) (int, error) { return copy(buffer, delta), nil }),
		body.EXPECT().Read(gomock.Any()).Return(0, readCause),
	)
	body.EXPECT().Close().Return(closeCause)
	transport := NewMockHTTPRoundTripper(controller)
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		return newReceivedDiagnosticResponse(request, http.StatusOK, body), nil
	})
	terminalCount := 0
	// Act until finalization reaches the failed content-end handler.
	err := newTransportFailureDriver(t, transport).Stream(t.Context(), modelexecution.ProviderRequest{
		ReasoningChoice: model.ReasoningChoiceOn,
		Instructions:    "instructions",
		Model:           testModelDescriptor("gpt-test"),
		History:         nil,
		Tools:           nil, Wire: mo.None[modelexecution.
					WireCallbacks](),
	}, func(event modelexecution.StreamEvent) error {
		if event.Kind == modelexecution.StreamEventDone || event.Kind == modelexecution.StreamEventError {
			terminalCount++
		}
		if event.Kind == modelexecution.StreamEventContentEnd {
			return handlerCause
		}
		return nil
	})
	// Assert all acquired causes survive without a terminal callback.
	require.ErrorIs(t, err, readCause)
	require.ErrorIs(t, err, handlerCause)
	require.ErrorIs(t, err, closeCause)
	assert.Zero(t, terminalCount)
}
