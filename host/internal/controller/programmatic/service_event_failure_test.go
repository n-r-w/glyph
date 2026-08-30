package programmatic

import (
	"context"
	"errors"

	"testing"
	"testing/synctest"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	domainsession "github.com/n-r-w/glyph/host/internal/domain/session"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestEventFailureEndsBlockedReceive verifies terminal event work does not wait for another client frame.
func TestEventFailureEndsBlockedReceive(t *testing.T) {
	t.Parallel()

	// Arrange mapping and transport failures that occur while request receive is blocked.
	tests := map[string]struct {
		event        AgentEvent
		sendErr      error
		wantCode     codes.Code
		wantCause    SessionCompletionCause
		wantSendCall bool
	}{
		"mapping": {
			event:        invalidModelOutcomeEvent(),
			wantCode:     codes.Internal,
			wantCause:    SessionCompletionProtocolFailure,
			sendErr:      nil,
			wantSendCall: false,
		},
		"status send": {
			event: AgentEvent{
				CorrelationID:   "user",
				Type:            AgentEventAgentStart,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
			sendErr:      status.Error(codes.ResourceExhausted, "send failed"),
			wantCode:     codes.ResourceExhausted,
			wantCause:    SessionCompletionTransportFailure,
			wantSendCall: true,
		},
		"plain send": {
			event: AgentEvent{
				CorrelationID:   "user",
				Type:            AgentEventAgentStart,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
			sendErr:      errors.New("send failed"),
			wantCode:     codes.Internal,
			wantCause:    SessionCompletionTransportFailure,
			wantSendCall: true,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				ctrl := gomock.NewController(t)
				session := NewMockHostSession(ctrl)
				operation := NewMockOperation(ctrl)
				stream := NewMockOpenStream(ctrl)
				streamContext, cancelStream := context.WithCancel(t.Context())
				stream.EXPECT().Context().Return(streamContext).AnyTimes()
				//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active UserRequest field.
				request := programmaticv1.OpenRequest_builder{
					GetSessionEntries: nil,
					CorrelationId:     new("user"),
					UserRequest: programmaticv1.UserRequest_builder{
						Text: new("request"),
					}.Build(),
					CreateSession:  nil,
					ListSessions:   nil,
					ResumeSession:  nil,
					SetSessionName: nil,
					GetSessionInfo: nil,
				}.Build()
				events := make(chan AgentEvent)
				receiveBlocked := make(chan struct{})
				receiveReleased := make(chan struct{})
				operation.EXPECT().Events().Return(events)
				operation.EXPECT().Start()
				gomock.InOrder(
					stream.EXPECT().Recv().Return(request, nil),
					session.EXPECT().Handle(gomock.Any(), gomock.Any()).Return(Response{
						SessionEntries:    nil,
						SessionStatistics: mo.None[domainsession.Statistics](),
						CorrelationID:     "user",
						Kind:              ResponseUserRequestAccepted,
						State:             mo.None[RunStateResult](),
						Messages:          nil,
						Models:            mo.None[ModelsResult](),
						Selection:         mo.None[model.Selection](),
						Rejection:         mo.None[Rejection](),
						SessionInfo:       mo.None[domainsession.Info](),
						Sessions:          nil,
					}, operation, nil),
					stream.EXPECT().Send(gomock.Any()).Return(nil),
					stream.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
						close(receiveBlocked)
						<-streamContext.Done()
						close(receiveReleased)
						return nil, context.Canceled
					}),
					session.EXPECT().CancelAndWait(gomock.Any()).Return(nil),
				)
				if test.wantSendCall {
					stream.EXPECT().Send(gomock.Any()).Return(test.sendErr)
				}
				service := New(t.Context(), session)
				// Act by delivering the terminal event after receive blocks.
				openDone := make(chan error, 1)
				go func() { openDone <- service.open(stream) }()
				<-receiveBlocked
				events <- test.event
				synctest.Wait()

				// Assert event failure terminates the controller before receive is released.
				select {
				case err := <-openDone:
					assert.Equal(t, test.wantCode, status.Code(err))
				default:
					cancelStream()
					synctest.Wait()
					require.Fail(t, "controller remained blocked in Recv after an event terminal")
				}
				completion := <-service.Completions()
				assert.Equal(t, test.wantCause, completion.Cause)
				select {
				case <-receiveReleased:
					require.Fail(t, "receive was released before completion")
				default:
				}

				cancelStream()
				synctest.Wait()
				select {
				case <-receiveReleased:
				default:
					require.Fail(t, "receive worker did not exit after stream cancellation")
				}
			})
		})
	}
}
