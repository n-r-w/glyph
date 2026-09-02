//go:build integration

package uiv1

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// invalidHostPayloadCase defines one correlated malformed Host event.
type invalidHostPayloadCase struct {
	// name identifies the nested contract invariant.
	name string
	// request is the UI operation correlated with the Host event.
	request *uiv1.UIRequest
	// event is the malformed progress or completion event.
	event *uiv1.HostEvent
}

// TestMalformedNestedPayloadsFailBeforeDelivery verifies SDK validation through real streams.
func TestMalformedNestedPayloadsFailBeforeDelivery(t *testing.T) {
	t.Parallel()

	for _, test := range malformedHostPayloadCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one tracked operation with callback and completion observations.
			controller := gomock.NewController(t)
			service := NewMockService(controller)
			initializationOperation := NewMockInitializeOperation(controller)
			callbackCalled := make(chan struct{}, 1)
			completionDelivered := make(chan struct{}, 1)
			cleanupCompleted := make(chan struct{})
			service.EXPECT().PrepareInitialize(gomock.Any(), gomock.Any()).Return(initializationOperation, nil)
			initializationOperation.EXPECT().Run(gomock.Any()).Return(new(uiv1.Initialized), nil)
			initializationOperation.EXPECT().Release()
			service.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(func(
				ctx context.Context,
				host *Host,
			) error {
				tracked, err := host.Start(ctx, "operation", test.request)
				if err != nil {
					return err
				}
				_, err = tracked.Wait(ctx, func(*uiv1.HostProgress) { callbackCalled <- struct{}{} })
				if err == nil {
					completionDelivered <- struct{}{}
					return host.Close(context.WithoutCancel(ctx))
				}
				return err
			})
			service.EXPECT().Close().DoAndReturn(func() error {
				close(cleanupCompleted)
				return nil
			})
			client := TestClient(t, service)
			stream, err := client.Open(t.Context())
			require.NoError(t, err)
			sendIntegrationInitialization(t, stream)
			for range 3 {
				_, err = stream.Recv()
				require.NoError(t, err)
			}
			request, err := stream.Recv()
			require.NoError(t, err)
			require.Equal(t, "operation", request.GetOperationId())
			for _, lifecycle := range []*uiv1.HostEvent{acceptedHostEvent(), runningHostEvent()} {
				require.NoError(t, stream.Send(hostOperationEvent(lifecycle)))
			}

			// Act by sending one malformed nested payload and a terminal fallback for progress cases.
			require.NoError(t, stream.Send(hostOperationEvent(test.event)))
			if test.event.GetProgress() != nil {
				terminalSendErr := stream.Send(hostOperationEvent(validCompletedHostEvent(test.request)))
				if terminalSendErr != nil {
					assert.Contains(
						t,
						[]codes.Code{codes.Canceled, codes.Unknown, codes.Unavailable},
						status.Code(terminalSendErr),
					)
				}
			}
			require.NoError(t, stream.CloseSend())
			for {
				_, err = stream.Recv()
				if err != nil {
					break
				}
			}

			// Assert protocol failure precedes callback or successful completion delivery.
			require.Error(t, err)
			require.NotErrorIs(t, err, io.EOF)
			assert.Equal(t, codes.FailedPrecondition, status.Code(err))
			require.Eventually(t, func() bool {
				select {
				case <-cleanupCompleted:
					return true
				default:
					return false
				}
			}, time.Second, time.Millisecond)
			select {
			case <-callbackCalled:
				t.Fatal("malformed progress reached the callback")
			default:
			}
			select {
			case <-completionDelivered:
				t.Fatal("malformed completion reached Operation.Wait")
			default:
			}
		})
	}
}

// malformedHostPayloadCases returns progress and completion invariants for every contract variant.
func malformedHostPayloadCases() []invalidHostPayloadCase {
	submit := submitUIRequest()
	return []invalidHostPayloadCase{
		{name: "authorization URL", request: authenticationUIRequest(), event: func() *uiv1.HostEvent {
			progress := new(uiv1.HostProgress)
			progress.SetAuthorization(new(uiv1.AuthorizationRequest))
			event := new(uiv1.HostEvent)
			event.SetProgress(progress)
			return event
		}()},
		{name: "agent start run ID", request: submit, event: malformedAgentEvent(
			uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START, func(event *uiv1.AgentEvent) { event.ClearRunId() },
		)},
		{name: "agent start inactive text", request: submit, event: malformedAgentEvent(
			uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START, func(event *uiv1.AgentEvent) { event.SetText("unexpected") },
		)},
		{name: "turn start run ID", request: submit, event: malformedAgentEvent(
			uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_START, func(event *uiv1.AgentEvent) { event.ClearRunId() },
		)},
		{name: "message start run ID", request: submit, event: malformedAgentEvent(
			uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START, func(event *uiv1.AgentEvent) { event.ClearRunId() },
		)},
		{name: "model content start type", request: submit, event: malformedAgentEvent(
			uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START, func(event *uiv1.AgentEvent) {
				content := validModelContent(uiv1.ModelContentType_MODEL_CONTENT_TYPE_UNSPECIFIED)
				event.SetModelContent(content)
			},
		)},
		{name: "model text delta text", request: submit, event: malformedAgentEvent(
			uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA, func(event *uiv1.AgentEvent) {
				content := validModelContent(uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA)
				content.ClearText()
				event.SetModelContent(content)
			},
		)},
		{name: "model content end kind", request: submit, event: malformedAgentEvent(
			uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END, func(event *uiv1.AgentEvent) {
				content := validModelContent(uiv1.ModelContentType_MODEL_CONTENT_TYPE_END)
				content.SetKind(uiv1.ModelContentKind_MODEL_CONTENT_KIND_UNSPECIFIED)
				event.SetModelContent(content)
			},
		)},
		{name: "message end nil content", request: submit, event: malformedAgentEvent(
			uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END, func(event *uiv1.AgentEvent) {
				response := new(uiv1.ModelResponse)
				response.SetContent([]*uiv1.ModelResponseContent{nil})
				event.SetModelResponse(response)
			},
		)},
		{name: "tool call start preview", request: submit, event: malformedAgentEvent(
			uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_START, func(event *uiv1.AgentEvent) {
				event.SetToolCallPreview(new(uiv1.ToolCallPreview))
			},
		)},
		{name: "tool call delta field content", request: submit, event: malformedAgentEvent(
			uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_DELTA, func(event *uiv1.AgentEvent) {
				preview := validToolCallPreview()
				field := new(uiv1.ToolCallPreviewField)
				field.SetName("argument")
				preview.SetFields([]*uiv1.ToolCallPreviewField{field})
				event.SetToolCallPreview(preview)
			},
		)},
		{name: "tool call end arguments", request: submit, event: malformedAgentEvent(
			uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END, func(event *uiv1.AgentEvent) {
				call := new(uiv1.FinalToolCall)
				call.SetCallId("call")
				call.SetName("tool")
				call.SetPosition(0)
				event.SetFinalToolCall(call)
			},
		)},
		{name: "tool execution start identity", request: submit, event: malformedAgentEvent(
			uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START, func(event *uiv1.AgentEvent) {
				event.SetToolName("tool")
			},
		)},
		{name: "tool execution update channel", request: submit, event: malformedAgentEvent(
			uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE, func(event *uiv1.AgentEvent) {
				event.SetText("progress")
				event.SetProgressChannel(uiv1.ProgressChannel_PROGRESS_CHANNEL_UNSPECIFIED)
			},
		)},
		{name: "tool execution end status", request: submit, event: malformedAgentEvent(
			uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END, func(event *uiv1.AgentEvent) {
				event.SetToolCallId("call")
				event.SetToolName("tool")
			},
		)},
		{name: "tool result nil content", request: submit, event: malformedAgentEvent(
			uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT, func(event *uiv1.AgentEvent) {
				event.SetToolCallId("call")
				event.SetToolName("tool")
				event.SetIsError(false)
				event.SetToolResultContents([]*uiv1.ToolResultContent{nil})
			},
		)},
		{name: "turn end summary", request: submit, event: malformedAgentEvent(
			uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_END, func(*uiv1.AgentEvent) {},
		)},
		{name: "agent end outcome", request: submit, event: malformedAgentEvent(
			uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END, func(*uiv1.AgentEvent) {},
		)},
		{name: "submit acknowledgement", request: submit, event: malformedCompletion(func(value *uiv1.HostCompleted) {
			value.SetSubmit(nil)
		})},
		{
			name:    "cancellation target state",
			request: cancelUIRequest(),
			event: malformedCompletion(func(value *uiv1.HostCompleted) {
				value.SetCancel(new(operationv1.CancelCompleted))
			}),
		},
		{
			name:    "authentication acknowledgement",
			request: authenticationUIRequest(),
			event: malformedCompletion(func(value *uiv1.HostCompleted) {
				value.SetAuthentication(nil)
			}),
		},
		{
			name:    "model selection",
			request: modelSelectionUIRequest(),
			event: malformedCompletion(func(value *uiv1.HostCompleted) {
				value.SetModelSelection(new(uiv1.ModelSelectionChanged))
			}),
		},
		{
			name:    "session changed entry",
			request: createSessionUIRequest(),
			event: malformedCompletion(func(value *uiv1.HostCompleted) {
				changed := new(uiv1.SessionChanged)
				changed.SetInfo(validSessionInfo())
				changed.SetEntries([]*uiv1.SessionEntry{nil})
				value.SetSessionChanged(changed)
			}),
		},
		{
			name:    "session list item",
			request: listSessionsUIRequest(),
			event: malformedCompletion(func(value *uiv1.HostCompleted) {
				list := new(uiv1.SessionList)
				list.SetSessions([]*uiv1.SessionSummary{nil})
				value.SetSessionList(list)
			}),
		},
		{
			name:    "session information statistics",
			request: sessionInfoUIRequest(),
			event: malformedCompletion(func(value *uiv1.HostCompleted) {
				information := new(uiv1.SessionInformation)
				information.SetInfo(validSessionInfo())
				statistics := new(uiv1.SessionStatistics)
				statistics.SetEstimatedCost(new(uiv1.EstimatedCost))
				information.SetStatistics(statistics)
				value.SetSessionInformation(information)
			}),
		},
		{
			name:    "session tree entry",
			request: sessionTreeUIRequest(),
			event: malformedCompletion(func(value *uiv1.HostCompleted) {
				result := new(uiv1.SessionTreeResult)
				tree := new(uiv1.SessionTree)
				tree.SetEntries([]*uiv1.SessionTreeEntry{nil})
				result.SetTree(tree)
				value.SetSessionTree(result)
			}),
		},
		{
			name:    "navigation active branch",
			request: navigationUIRequest(),
			event: malformedCompletion(func(value *uiv1.HostCompleted) {
				result := new(uiv1.SessionTreeNavigationResult)
				result.SetStatus(uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_COMMITTED)
				result.SetTree(new(uiv1.SessionTree))
				result.SetActiveBranch([]*uiv1.SessionEntry{nil})
				value.SetSessionTreeNavigation(result)
			}),
		},
		{
			name:    "forked session entry",
			request: forkUIRequest(),
			event: malformedCompletion(func(value *uiv1.HostCompleted) {
				forked := new(uiv1.SessionForked)
				changed := new(uiv1.SessionChanged)
				changed.SetInfo(validSessionInfo())
				changed.SetEntries([]*uiv1.SessionEntry{nil})
				forked.SetSession(changed)
				forked.SetNextInput("next")
				value.SetSessionForked(forked)
			}),
		},
		{name: "cloned session", request: cloneUIRequest(), event: malformedCompletion(func(value *uiv1.HostCompleted) {
			value.SetSessionCloned(new(uiv1.SessionCloned))
		})},
		{
			name:    "entry label tree",
			request: entryLabelUIRequest(),
			event: malformedCompletion(func(value *uiv1.HostCompleted) {
				result := new(uiv1.EntryLabelSet)
				tree := new(uiv1.SessionTree)
				tree.SetEntries([]*uiv1.SessionTreeEntry{nil})
				result.SetTree(tree)
				value.SetEntryLabelSet(result)
			}),
		},
	}
}
