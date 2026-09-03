//go:build !integration

package programmatic

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/internal/testsupport/operationmock"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestSendFailureCancelsAndJoinsOwnedWork verifies transport failure cleanup.
func TestSendFailureCancelsAndJoinsOwnedWork(t *testing.T) {
	t.Parallel()

	// Arrange accepted work and a writer that fails its first delivery.
	controller := gomock.NewController(t)
	host := NewMockHostSession(controller)
	prepared := operationmock.NewMockOperationPrepared[AgentEvent, Response](controller)
	prepared.EXPECT().Release()
	host.EXPECT().Prepare(gomock.Any(), gomock.Any()).Return(prepared, nil)
	stream := NewMockOpenStream(controller)
	stream.EXPECT().Context().Return(t.Context()).AnyTimes()
	received := false
	stream.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
		if received {
			<-t.Context().Done()
			return nil, t.Context().Err()
		}
		received = true
		return testRequest("send-failure", func(request *programmaticv1.ControllerRequest) {
			request.SetGetModels(new(programmaticv1.GetModels))
		}), nil
	}).AnyTimes()
	stream.EXPECT().Send(gomock.Any()).Return(errors.New("send failed"))
	service := New(t.Context(), host)

	// Act by opening the stream.
	err := service.open(stream)

	// Assert unavailable status and release before Run starts.
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

// TestDeliveryOverflowFailsConnection verifies bounded queue overflow mapping.
func TestDeliveryOverflowFailsConnection(t *testing.T) {
	t.Parallel()

	// Arrange malformed requests and a writer blocked on its first Send.
	controller := gomock.NewController(t)
	host := NewMockHostSession(controller)
	stream := NewMockOpenStream(controller)
	stream.EXPECT().Context().Return(t.Context()).AnyTimes()
	var received int
	sendStarted := make(chan struct{})
	overflowRequestReceived := make(chan struct{})
	stream.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
		if received >= 66 {
			<-t.Context().Done()
			return nil, t.Context().Err()
		}
		received++
		if received == 2 {
			<-sendStarted
		}
		if received == 66 {
			close(overflowRequestReceived)
		}
		request := new(programmaticv1.OpenRequest)
		request.SetOperationId(fmt.Sprintf("malformed-%d", received))
		request.SetRequest(new(programmaticv1.ControllerRequest))
		return request, nil
	}).AnyTimes()
	releaseSend := make(chan struct{})
	stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(*programmaticv1.OpenResponse) error {
		close(sendStarted)
		<-releaseSend
		return nil
	})
	service := New(t.Context(), host)

	// Act by overflowing the bounded writer queue before releasing its active send.
	result := make(chan error, 1)
	go func() { result <- service.open(stream) }()
	<-overflowRequestReceived
	time.Sleep(50 * time.Millisecond)
	close(releaseSend)
	err := <-result

	// Assert ResourceExhausted terminates the connection after Writer.Run joins.
	require.Error(t, err)
	assert.Equal(t, codes.ResourceExhausted, status.Code(err))
}
