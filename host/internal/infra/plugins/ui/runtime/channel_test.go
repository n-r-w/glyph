//go:build !integration

package runtime

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// TestChannelCloseUnblocksPendingReceive verifies cancellation through the owned stream context.
func TestChannelCloseUnblocksPendingReceive(t *testing.T) {
	t.Parallel()

	service := &closeContractService{
		UnimplementedUIServiceServer: uipb.UnimplementedUIServiceServer{},
		opened:                       make(chan struct{}),
	}
	client := uisdk.TestClient(t, service)
	streamContext, cancel := context.WithCancel(t.Context())
	stream, err := client.Open(streamContext)
	require.NoError(t, err)
	transport := &channel{
		stream: stream,
		cancel: cancel,
		closed: atomic.Bool{},
		mutex:  sync.Mutex{},
	}
	receiveStarted := make(chan struct{})
	receiveDone := make(chan error, 1)
	go func() {
		close(receiveStarted)
		_, receiveErr := transport.Receive()
		receiveDone <- receiveErr
	}()

	<-service.opened
	<-receiveStarted
	transport.Close()

	require.Equal(t, codes.Canceled, status.Code(<-receiveDone))
}

// TestChannelSendRetainsFailureWithCanceledStreamContext verifies cancellation does not hide a Send cause.
func TestChannelSendRetainsFailureWithCanceledStreamContext(t *testing.T) {
	t.Parallel()

	// Arrange an independent Send failure with a stream context canceled by its parent or remote peer.
	controller := gomock.NewController(t)
	stream := NewMockUIService_OpenClient[uipb.OpenRequest, uipb.OpenResponse](controller)
	streamContext, cancel := context.WithCancel(t.Context())
	cancel()
	sendErr := errors.New("unique canceled-stream Send failure")
	stream.EXPECT().Send(gomock.Any()).Return(sendErr)
	stream.EXPECT().Context().Return(streamContext).AnyTimes()
	transport := &channel{
		stream: stream, cancel: cancel, closed: atomic.Bool{}, mutex: sync.Mutex{},
	}

	// Act while both the independent Send failure and stream cancellation are observable.
	err := transport.Send(testSimpleFrame(domainui.FrameInformation, "remote closed"))

	// Assert both independent causes remain reachable once.
	require.ErrorIs(t, err, sendErr)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, strings.Count(err.Error(), sendErr.Error()))
	assert.Equal(t, 1, strings.Count(err.Error(), context.Canceled.Error()))
}

// TestChannelSendCanonicalizesCancellationEquivalentFailures verifies shutdown filtering sees one cancellation.
func TestChannelSendCanonicalizesCancellationEquivalentFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sendErr error
	}{
		{name: "context cancellation", sendErr: context.Canceled},
		{name: "gRPC cancellation", sendErr: status.Error(codes.Canceled, "stream canceled")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange a cancellation-equivalent Send failure with an active stream context.
			controller := gomock.NewController(t)
			stream := NewMockUIService_OpenClient[uipb.OpenRequest, uipb.OpenResponse](controller)
			streamContext, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			stream.EXPECT().Send(gomock.Any()).Return(test.sendErr)
			stream.EXPECT().Context().Return(streamContext).AnyTimes()
			transport := &channel{
				stream: stream, cancel: cancel, closed: atomic.Bool{}, mutex: sync.Mutex{},
			}

			// Act through the cancellation-equivalent Send result.
			err := transport.Send(testSimpleFrame(domainui.FrameInformation, "canceled"))

			// Assert the result is canonical cancellation for recursive shutdown filtering.
			require.ErrorIs(t, err, context.Canceled)
			assert.Equal(t, 1, strings.Count(err.Error(), context.Canceled.Error()))
		})
	}
}

// TestChannelCloseUnblocksSendWithCanonicalCancellation verifies owned close releases terminal delivery.
func TestChannelCloseUnblocksSendWithCanonicalCancellation(t *testing.T) {
	t.Parallel()

	// Arrange a Send that remains blocked until the owned stream context is canceled.
	controller := gomock.NewController(t)
	stream := NewMockUIService_OpenClient[uipb.OpenRequest, uipb.OpenResponse](controller)
	streamContext, cancel := context.WithCancel(t.Context())
	sendStarted := make(chan struct{})
	stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(*uipb.OpenRequest) error {
		close(sendStarted)
		<-streamContext.Done()
		return io.EOF
	})
	stream.EXPECT().Context().Return(streamContext).AnyTimes()
	transport := &channel{
		stream: stream, cancel: cancel, closed: atomic.Bool{}, mutex: sync.Mutex{},
	}
	sendDone := make(chan error, 1)
	go func() {
		sendDone <- transport.Send(testSimpleFrame(domainui.FrameInformation, "blocked"))
	}()
	<-sendStarted

	// Act by closing the channel that owns the blocked stream.
	transport.Close()
	err := <-sendDone

	// Assert owned-close EOF becomes one canonical cancellation for shutdown filtering.
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, io.EOF)
	assert.Equal(t, 1, strings.Count(err.Error(), context.Canceled.Error()))
}

// TestChannelSendNormalizesPeerCanceledEOF verifies peer stream closure becomes canonical cancellation.
func TestChannelSendNormalizesPeerCanceledEOF(t *testing.T) {
	t.Parallel()

	// Arrange EOF with a peer-canceled stream context before any Host Close call.
	controller := gomock.NewController(t)
	stream := NewMockUIService_OpenClient[uipb.OpenRequest, uipb.OpenResponse](controller)
	streamContext, cancel := context.WithCancel(t.Context())
	cancel()
	stream.EXPECT().Send(gomock.Any()).Return(io.EOF)
	stream.EXPECT().Context().Return(streamContext).AnyTimes()
	transport := &channel{
		stream: stream, cancel: cancel, closed: atomic.Bool{}, mutex: sync.Mutex{},
	}

	// Act while the peer-canceled context and Send EOF are both observable.
	err := transport.Send(testSimpleFrame(domainui.FrameInformation, "peer EOF"))

	// Assert UI transport closure is one canonical cancellation rather than raw EOF.
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, io.EOF)
	assert.Equal(t, 1, strings.Count(err.Error(), context.Canceled.Error()))
}

// TestChannelSendPreservesRemoteEOF verifies EOF without owned close remains visible.
func TestChannelSendPreservesRemoteEOF(t *testing.T) {
	t.Parallel()

	// Arrange a remote EOF while the stream context remains active and the channel is not closed.
	controller := gomock.NewController(t)
	stream := NewMockUIService_OpenClient[uipb.OpenRequest, uipb.OpenResponse](controller)
	streamContext, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	stream.EXPECT().Send(gomock.Any()).Return(io.EOF)
	stream.EXPECT().Context().Return(streamContext).AnyTimes()
	transport := &channel{
		stream: stream, cancel: cancel, closed: atomic.Bool{}, mutex: sync.Mutex{},
	}

	// Act before any owned Close call.
	err := transport.Send(testSimpleFrame(domainui.FrameInformation, "remote EOF"))

	// Assert remote EOF stays visible and is not reclassified as cancellation.
	require.ErrorIs(t, err, io.EOF)
	require.NotErrorIs(t, err, context.Canceled)
}

// TestChannelSendPreservesFailureWithActiveStreamContext verifies unrelated send errors remain intact.
func TestChannelSendPreservesFailureWithActiveStreamContext(t *testing.T) {
	t.Parallel()

	// Arrange a generated stream mock with an active context and a unique Send failure.
	controller := gomock.NewController(t)
	stream := NewMockUIService_OpenClient[uipb.OpenRequest, uipb.OpenResponse](controller)
	streamContext, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	sendErr := errors.New("unique uncanceled UI send failure")
	stream.EXPECT().Send(gomock.Any()).Return(sendErr)
	stream.EXPECT().Context().Return(streamContext).AnyTimes()
	transport := &channel{
		stream: stream, cancel: cancel, closed: atomic.Bool{}, mutex: sync.Mutex{},
	}

	// Act while the stream context remains active.
	err := transport.Send(testSimpleFrame(domainui.FrameInformation, "active"))

	// Assert the original Send failure remains reachable and cancellation is not introduced.
	require.ErrorIs(t, err, sendErr)
	require.NotErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "send UI frame")
}
