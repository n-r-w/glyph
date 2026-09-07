//go:build integration

package programmatic

import (
	"context"
	"errors"
	"io"
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

// TestRejectedOutputRetainsSourcesAtCompletion covers send failure, queued disposal and late admission cleanup.
func TestRejectedOutputRetainsSourcesAtCompletion(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"direct send", "queued rejection", "late admission", "late failure"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				// Arrange a failed rejection Send and optional admission still producing a second rejection.
				controller := gomock.NewController(t)
				firstSource := errors.New("first admission source")
				source := errors.Join(errors.New("second admission source"), context.Canceled, io.EOF)
				deliveryErr := errors.New("rejection delivery failed")
				admissionRelease := make(chan struct{})
				host := NewMockHostSession(controller)
				host.EXPECT().Prepare(gomock.Any(), gomock.Any()).Return(nil, Reject(RejectionCodeBusy, firstSource))
				if mode != "direct send" {
					host.EXPECT().Prepare(gomock.Any(), gomock.Any()).DoAndReturn(
						func(context.Context, Command) (operation.Prepared[OperationProgress, Response], error) {
							if mode == "late admission" || mode == "late failure" {
								<-admissionRelease
							}
							if mode == "late failure" {
								return nil, source
							}
							return nil, Reject(RejectionCodeBusy, source)
						},
					)
				}
				output := NewMockConnectionOutput(controller)
				output.EXPECT().BindWriter(gomock.Any()).Return(func() {})
				stream := NewMockOpenStream(controller)
				stream.EXPECT().Context().Return(t.Context())
				request := new(programmaticv1.OpenRequest)
				request.SetOperationId("rejected")
				payload := new(programmaticv1.ControllerRequest)
				payload.SetGetModels(new(programmaticv1.GetModels))
				request.SetRequest(payload)
				stopReceive := make(chan struct{})
				firstReceived := stream.EXPECT().Recv().Return(request, nil)
				if mode != "direct send" {
					firstReceived = stream.EXPECT().Recv().Return(request, nil).After(firstReceived)
				}
				stream.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
					<-stopReceive
					return nil, context.Canceled
				}).After(firstReceived).MaxTimes(1)
				releaseSend := make(chan struct{})
				stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(response *programmaticv1.OpenResponse) error {
					assert.Equal(t, firstSource.Error(), response.GetEvent().GetRejected().GetMessage())
					<-releaseSend
					return deliveryErr
				})
				service := New(t.Context(), host, output)
				result := make(chan error, 1)

				// Act after request processing reaches its queued or in-flight rejection step.
				go func() { result <- service.open(stream) }()
				synctest.Wait()
				close(releaseSend)
				synctest.Wait()
				close(admissionRelease)
				err := <-result
				completion := <-service.Completions()
				close(stopReceive)
				synctest.Wait()

				// Assert both local completions retain the original admission causes and delivery category.
				require.Equal(t, codes.Unavailable, status.Code(err))
				for _, failure := range []error{err, completion.Err} {
					require.ErrorIs(t, failure, firstSource)
					require.ErrorIs(t, failure, deliveryErr)
					if mode != "direct send" {
						require.ErrorIs(t, failure, source)
					}
				}
			})
		})
	}
}

// TestPreparationFailureRetainsIndependentWriterCleanup checks the read-first completion path.
func TestPreparationFailureRetainsIndependentWriterCleanup(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange an accepted Send that fails only after a second preparation stops the connection.
		controller := gomock.NewController(t)
		host := NewMockHostSession(controller)
		prepared := operationmock.NewMockOperationPrepared[OperationProgress, Response](controller)
		sending, released := make(chan struct{}), make(chan struct{})
		prepared.EXPECT().Release().Do(func() { close(released) })
		readErr := errors.New("independent fatal preparation source")
		writeErr := errors.New("independent accepted-frame Send failure")
		gomock.InOrder(
			host.EXPECT().Prepare(gomock.Any(), gomock.Any()).Return(prepared, nil),
			host.EXPECT().Prepare(gomock.Any(), gomock.Any()).DoAndReturn(
				func(context.Context, Command) (operation.Prepared[OperationProgress, Response], error) {
					<-sending
					return nil, readErr
				},
			),
		)
		stream := NewMockOpenStream(controller)
		stream.EXPECT().Context().Return(t.Context())
		stopReceive := make(chan struct{})
		request := func(id string) *programmaticv1.OpenRequest {
			payload := new(programmaticv1.ControllerRequest)
			payload.SetGetModels(new(programmaticv1.GetModels))
			message := new(programmaticv1.OpenRequest)
			message.SetOperationId(id)
			message.SetRequest(payload)
			return message
		}
		gomock.InOrder(
			stream.EXPECT().Recv().Return(request("first"), nil),
			stream.EXPECT().Recv().Return(request("second"), nil),
			stream.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
				<-stopReceive
				return nil, io.EOF
			}).MaxTimes(1),
		)
		stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(*programmaticv1.OpenResponse) error {
			close(sending)
			<-released
			return writeErr
		})
		output := NewMockConnectionOutput(controller)
		output.EXPECT().BindWriter(gomock.Any()).Return(func() {})
		service := New(t.Context(), host, output)

		// Act through read-first connection cleanup and its pending writer result.
		err := service.open(stream)
		completion := <-service.Completions()
		close(stopReceive)
		synctest.Wait()

		// Assert independent Send failure cannot replace the selected preparation category.
		require.Equal(t, codes.Internal, status.Code(err))
		require.Equal(t, SessionCompletionProtocolFailure, completion.Cause)
		for _, result := range []error{err, completion.Err} {
			require.ErrorIs(t, result, readErr)
			require.ErrorIs(t, result, writeErr)
		}
	})
}
