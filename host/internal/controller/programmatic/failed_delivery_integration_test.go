//go:build integration

package programmatic

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/internal/operation"
	"github.com/n-r-w/glyph/internal/testsupport/operationmock"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestFailedTerminalSendPreservesCompletionCauses exercises the real writer, owner, and RPC completion.
func TestFailedTerminalSendPreservesCompletionCauses(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange accepted work that fails and a transport that rejects only its Failed event.
		controller := gomock.NewController(t)
		source := errors.Join(
			operation.ErrQueueFull,
			errors.New(strings.Repeat("界", 4001)+" complete operation source suffix..."),
		)
		deliveryErr := errors.New("complete terminal send failure suffix")
		prepared := operationmock.NewMockOperationPrepared[OperationProgress, Response](controller)
		prepared.EXPECT().
			Run(gomock.Any(), gomock.Any()).
			Return(operation.Failed[Response](FailureCodeInternal, source))
		prepared.EXPECT().Release()
		host := NewMockHostSession(controller)
		host.EXPECT().Prepare(gomock.Any(), gomock.Any()).Return(prepared, nil)
		output := NewMockConnectionOutput(controller)
		output.EXPECT().BindWriter(gomock.Any()).Return(func() {})
		stream := NewMockOpenStream(controller)
		stream.EXPECT().Context().Return(t.Context())
		request := new(programmaticv1.OpenRequest)
		request.SetOperationId("failed-operation")
		payload := new(programmaticv1.ControllerRequest)
		payload.SetGetModels(new(programmaticv1.GetModels))
		request.SetRequest(payload)
		receiveStop := make(chan struct{})
		gomock.InOrder(
			stream.EXPECT().Recv().Return(request, nil),
			stream.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
				<-receiveStop
				return nil, context.Canceled
			}),
		)
		var events []string
		stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(response *programmaticv1.OpenResponse) error {
			event := response.GetEvent()
			switch {
			case event.HasAccepted():
				events = append(events, "accepted")
			case event.HasRunning():
				events = append(events, "running")
			case event.HasFailed():
				events = append(events, "failed")
				assert.Equal(t, FailureCodeInternal, event.GetFailed().GetCode())
				assert.Equal(t, source.Error(), event.GetFailed().GetMessage())
				return deliveryErr
			default:
				t.Error("unexpected operation event")
			}
			return nil
		}).Times(3)
		service := New(t.Context(), host, output)

		// Act through the actual accepted-operation completion path.
		err := service.open(stream)
		completion := <-service.Completions()
		close(receiveStop)
		synctest.Wait()

		// Assert RPC and application completion retain both identities and transport classification.
		assert.Equal(t, []string{"accepted", "running", "failed"}, events)
		assert.Equal(t, codes.Unavailable, status.Code(err))
		assert.Equal(t, SessionCompletionTransportFailure, completion.Cause)
		for _, result := range []error{err, completion.Err} {
			require.ErrorIs(t, result, source)
			require.ErrorIs(t, result, deliveryErr)
			require.ErrorContains(t, result, source.Error())
			require.ErrorContains(t, result, deliveryErr.Error())
		}
	})
}

// TestPendingFailedTerminalPreservesCompletionCauses verifies completion when an earlier send blocks delivery.
func TestPendingFailedTerminalPreservesCompletionCauses(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{
		"earlier send failure", "stream cancellation", "terminal enqueue overflow", "receive failure",
	} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				// Arrange a blocked Running send and an operation that has already failed.
				controller := gomock.NewController(t)
				source := errors.New("pending failed terminal source")
				deliveryErr := errors.New("earlier delivery stopped")
				streamContext, cancel := context.WithCancelCause(t.Context())
				defer cancel(context.Canceled)
				running := make(chan struct{})
				release := make(chan struct{})
				receiveStop := make(chan struct{})
				var writer *operation.Writer[*programmaticv1.OpenResponse]
				output := NewMockConnectionOutput(controller)
				output.EXPECT().BindWriter(gomock.Any()).DoAndReturn(
					func(bound *operation.Writer[*programmaticv1.OpenResponse]) func() {
						writer = bound
						return func() {}
					},
				)
				prepared := operationmock.NewMockOperationPrepared[OperationProgress, Response](controller)
				prepared.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
					func(context.Context, operation.Reporter[OperationProgress]) operation.Outcome[Response] {
						<-running
						if mode == "terminal enqueue overflow" {
							for writer.Enqueue(new(programmaticv1.OpenResponse)) == nil {
							}
						}
						return operation.Failed[Response](FailureCodeInternal, source)
					},
				)
				prepared.EXPECT().Release()
				host := NewMockHostSession(controller)
				host.EXPECT().Prepare(gomock.Any(), gomock.Any()).Return(prepared, nil)
				stream := NewMockOpenStream(controller)
				stream.EXPECT().Context().Return(streamContext)
				request := new(programmaticv1.OpenRequest)
				request.SetOperationId("pending-failure")
				payload := new(programmaticv1.ControllerRequest)
				payload.SetGetModels(new(programmaticv1.GetModels))
				request.SetRequest(payload)
				gomock.InOrder(
					stream.EXPECT().Recv().Return(request, nil),
					stream.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
						<-receiveStop
						if mode == "receive failure" {
							return nil, deliveryErr
						}
						return nil, context.Canceled
					}),
				)
				gomock.InOrder(
					stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(response *programmaticv1.OpenResponse) error {
						assert.True(t, response.GetEvent().HasAccepted())
						return nil
					}),
					stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(response *programmaticv1.OpenResponse) error {
						assert.True(t, response.GetEvent().HasRunning())
						close(running)
						<-release
						if mode == "earlier send failure" {
							return deliveryErr
						}
						return nil
					}),
				)
				service := New(t.Context(), host, output)
				result := make(chan error, 1)

				// Act after Terminal has enqueued or rejected its failed event.
				go func() { result <- service.open(stream) }()
				synctest.Wait()
				if mode == "stream cancellation" {
					cancel(deliveryErr)
				}
				if mode == "receive failure" {
					close(receiveStop)
					synctest.Wait()
				}
				close(release)
				err := <-result
				completion := <-service.Completions()
				if mode != "receive failure" {
					close(receiveStop)
				}
				synctest.Wait()

				// Assert the selected completion branch keeps the pending source and its delivery cause.
				expectedCode := codes.Unavailable
				expectedCause := SessionCompletionTransportFailure
				if mode == "receive failure" {
					expectedCode = codes.InvalidArgument
					expectedCause = SessionCompletionProtocolFailure
				}
				if mode == "terminal enqueue overflow" {
					deliveryErr = operation.ErrQueueFull
					expectedCode = codes.ResourceExhausted
				}
				assert.Equal(t, expectedCode, status.Code(err))
				assert.Equal(t, expectedCause, completion.Cause)
				for _, failure := range []error{err, completion.Err} {
					require.ErrorIs(t, failure, source)
					require.ErrorIs(t, failure, deliveryErr)
					require.ErrorContains(t, failure, source.Error())
				}
			})
		})
	}
}

// TestFailedTerminalQueueAndAcknowledgementPreserveCauses exercises immediate enqueue and waiting failures.
func TestFailedTerminalQueueAndAcknowledgementPreserveCauses(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"queue full", "closed", "acknowledgement canceled"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange a real writer whose delivery cannot complete for the selected reason.
			source := errors.Join(operation.ErrQueueFull, errors.New("failed outcome source suffix"))
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(context.Canceled)
			writer := operation.NewWriter(func(*programmaticv1.OpenResponse) error { return nil })
			var deliveryErr error
			switch name {
			case "queue full":
				for {
					deliveryErr = writer.Enqueue(new(programmaticv1.OpenResponse))
					if deliveryErr != nil {
						break
					}
				}
				require.ErrorIs(t, deliveryErr, operation.ErrQueueFull)
			case "closed":
				writer.Close()
				deliveryErr = operation.ErrClosed
			case "acknowledgement canceled":
				deliveryErr = errors.New("acknowledgement context stopped")
				cancel(deliveryErr)
			}
			var reported error
			delivery := &streamDelivery{
				context: ctx, writer: writer, registry: newTargetRegistry(),
				fail:  func(err error) { reported = err },
				mutex: sync.Mutex{}, failureSources: make(map[string]error),
			}

			// Act by reporting the operation failure through Terminal.
			ack, err := delivery.Terminal("failed-operation", operation.Failed[Response](FailureCodeInternal, source))

			// Assert both returned and connection-fatal errors retain source and delivery causes.
			assert.Nil(t, ack)
			expectedCode := codes.Unavailable
			if name == "queue full" {
				expectedCode = codes.ResourceExhausted
			}
			assert.Equal(t, expectedCode, status.Code(mapDeliveryError(reported)))
			// A later acknowledgement can receive the already mapped connection cause.
			firstReport := reported
			require.EqualError(t, delivery.failed(firstReport), firstReport.Error())
			for _, result := range []error{err, reported} {
				require.ErrorIs(t, result, source)
				require.ErrorIs(t, result, deliveryErr)
			}
			writer.Close()
		})
	}
}
