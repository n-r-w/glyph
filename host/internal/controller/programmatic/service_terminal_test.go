//go:build !integration

package programmatic

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/internal/operation"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestControllerHalfCloseCancelsAndJoinsOwnedWork verifies clean drain and joining.
func TestControllerHalfCloseCancelsAndJoinsOwnedWork(t *testing.T) {
	t.Parallel()

	// Arrange work that exits only after connection closure cancels it.
	controller := gomock.NewController(t)
	host := NewMockHostSession(controller)
	prepared := operation.NewMockPrepared[AgentEvent, Response](controller)
	joined := make(chan struct{})
	prepared.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ operation.Reporter[AgentEvent]) operation.Outcome[Response] {
			<-ctx.Done()
			close(joined)
			return operation.Canceled[Response]()
		},
	)
	prepared.EXPECT().Release()
	host.EXPECT().Prepare(gomock.Any(), gomock.Any()).Return(prepared, nil)
	stream := newStreamHarness(t, t.Context())
	service := New(t.Context(), host)
	result := make(chan error, 1)
	go func() { result <- service.open(stream.stream) }()
	stream.requests <- testRequest("owned", func(request *programmaticv1.ControllerRequest) {
		request.SetGetMessages(new(programmaticv1.GetMessages))
	})
	for {
		response := <-stream.responses
		if response.GetOperationId() == "owned" && response.GetEvent().HasRunning() {
			break
		}
	}

	// Act by half-closing while work is running.
	stream.closeSend()

	// Assert work joined and its terminal event drained before return.
	require.NoError(t, <-result)
	<-joined
}

// TestHostClosureWaitsForControllerHalfClose verifies orderly Host-requested shutdown.
func TestHostClosureWaitsForControllerHalfClose(t *testing.T) {
	t.Parallel()

	// Arrange an idle stream and an independently cancelable Host context.
	controller := gomock.NewController(t)
	host := NewMockHostSession(controller)
	applicationContext, cancelApplication := context.WithCancel(t.Context())
	stream := newStreamHarness(t, t.Context())
	service := New(applicationContext, host)
	result := make(chan error, 1)
	go func() { result <- service.open(stream.stream) }()

	// Act by closing the Host and half-closing only after its close request arrives.
	cancelApplication()
	response := <-stream.responses
	require.True(t, response.HasClose())
	stream.closeSend()

	// Assert the controller half-close completes clean Host-requested closure.
	require.NoError(t, <-result)
}

// TestHostClosureRejectsLateRequestAndJoinsOwnedWork verifies post-close request failure.
func TestHostClosureRejectsLateRequestAndJoinsOwnedWork(t *testing.T) {
	t.Parallel()

	// Arrange work that records when Host-requested closure has joined it.
	controller := gomock.NewController(t)
	host := NewMockHostSession(controller)
	prepared := operation.NewMockPrepared[AgentEvent, Response](controller)
	joined := make(chan struct{})
	prepared.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ operation.Reporter[AgentEvent]) operation.Outcome[Response] {
			<-ctx.Done()
			close(joined)
			return operation.Canceled[Response]()
		},
	)
	prepared.EXPECT().Release()
	host.EXPECT().Prepare(gomock.Any(), gomock.Any()).Return(prepared, nil)
	applicationContext, cancelApplication := context.WithCancel(t.Context())
	stream := newStreamHarness(t, t.Context())
	service := New(applicationContext, host)
	result := make(chan error, 1)
	go func() { result <- service.open(stream.stream) }()
	stream.requests <- testRequest("owned", func(request *programmaticv1.ControllerRequest) {
		request.SetGetMessages(new(programmaticv1.GetMessages))
	})
	for {
		response := <-stream.responses
		if response.GetOperationId() == "owned" && response.GetEvent().HasRunning() {
			break
		}
	}

	// Act by starting Host closure, then queueing one late request before request EOF.
	cancelApplication()
	response := <-stream.responses
	require.True(t, response.HasClose())
	stream.requests <- testRequest("late", func(request *programmaticv1.ControllerRequest) {
		request.SetGetMessages(new(programmaticv1.GetMessages))
	})
	stream.closeSend()

	// Assert the late request fails the stream and all prior work is joined.
	err := <-result
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	<-joined
	for {
		select {
		case response = <-stream.responses:
			require.False(t,
				response.GetOperationId() == "late" && response.GetEvent().HasAccepted(),
				"late request must not be accepted",
			)
		default:
			return
		}
	}
}

// TestHostClosurePreservesReceiveFailure verifies failure precedence while waiting for request EOF.
func TestHostClosurePreservesReceiveFailure(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		receiveErr error
		wantCode   codes.Code
	}{
		"protocol": {
			receiveErr: status.Error(codes.FailedPrecondition, "late request"),
			wantCode:   codes.FailedPrecondition,
		},
		"transport": {
			receiveErr: status.Error(codes.Unavailable, "receive failed"),
			wantCode:   codes.Unavailable,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange receive failure after the Host close message is delivered.
			controller := gomock.NewController(t)
			host := NewMockHostSession(controller)
			applicationContext, cancelApplication := context.WithCancel(t.Context())
			cancelApplication()
			closeSent := make(chan struct{})
			stream := NewMockOpenStream(controller)
			stream.EXPECT().Context().Return(t.Context()).AnyTimes()
			stream.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
				<-closeSent
				return nil, test.receiveErr
			})
			stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(response *programmaticv1.OpenResponse) error {
				require.True(t, response.HasClose())
				close(closeSent)
				return nil
			})
			service := New(applicationContext, host)

			// Act by opening with Host closure already requested.
			err := service.open(stream)

			// Assert the receive failure wins over clean application closure.
			require.Equal(t, test.wantCode, status.Code(err))
		})
	}
}

// TestHostClosurePreservesWriterFailure verifies send failure precedence during closure.
func TestHostClosurePreservesWriterFailure(t *testing.T) {
	t.Parallel()

	// Arrange a Host closure whose CloseConnection send fails.
	controller := gomock.NewController(t)
	host := NewMockHostSession(controller)
	applicationContext, cancelApplication := context.WithCancel(t.Context())
	cancelApplication()
	stopReceive := make(chan struct{})
	stream := NewMockOpenStream(controller)
	stream.EXPECT().Context().Return(t.Context()).AnyTimes()
	stream.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
		<-stopReceive
		return nil, io.EOF
	})
	writerErr := errors.New("send failed")
	stream.EXPECT().Send(gomock.Any()).Return(writerErr)
	service := New(applicationContext, host)
	result := make(chan error, 1)
	go func() { result <- service.open(stream) }()

	// Act by waiting for the writer failure, then release the receive goroutine.
	err := <-result
	close(stopReceive)

	// Assert Host closure preserves the writer failure.
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.ErrorContains(t, err, writerErr.Error())
}
