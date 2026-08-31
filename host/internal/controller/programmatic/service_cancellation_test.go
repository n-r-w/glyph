//go:build !integration

package programmatic

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestApplicationCancellationPreservesSelectedTerminals verifies precedence joins independent selected causes.
func TestApplicationCancellationPreservesSelectedTerminals(t *testing.T) {
	t.Parallel()

	cleanupErr := errors.New("unique selected cleanup failure")
	tests := []struct {
		name     string
		cause    SessionCompletionCause
		terminal error
	}{
		{
			name: "protocol terminal", cause: SessionCompletionProtocolFailure,
			terminal: status.Error(codes.InvalidArgument, "unique selected protocol failure"),
		},
		{
			name: "transport terminal", cause: SessionCompletionTransportFailure,
			terminal: errors.New("unique selected transport failure"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange a valid canceled application context and an already-selected independent terminal.
			applicationContext, cancelApplication := context.WithCancel(t.Context())
			cancelApplication()
			service := New(applicationContext, nil)
			selected := terminalResult{
				cause: test.cause, err: test.terminal, clean: false, passthrough: true,
			}
			eventTerminals := make(chan terminalResult, 1)
			receiveTerminals := make(chan terminalResult, 1)

			// Act through precedence and completion publication.
			terminal := service.applyTerminalPrecedence(
				t.Context(), selected, eventTerminals, receiveTerminals,
			)
			completion, rpcErr := service.complete(t.Context(), terminal, cleanupErr)

			// Assert RPC and completion ownership remain canceled while every independent cause survives once.
			assert.Equal(t, codes.Canceled, status.Code(rpcErr))
			assert.Equal(t, SessionCompletionApplicationCanceled, completion.Cause)
			require.ErrorIs(t, completion.Err, context.Canceled)
			require.ErrorIs(t, completion.Err, test.terminal)
			assert.Equal(t, 1, strings.Count(completion.Err.Error(), context.Canceled.Error()))
			assert.Equal(t, 1, strings.Count(completion.Err.Error(), test.terminal.Error()))
			require.ErrorIs(t, completion.CleanupErr, cleanupErr)
		})
	}
}

// TestOwnerClosurePreservesSelectedAndReadyTerminals verifies clean RPC ownership keeps local errors.
func TestOwnerClosurePreservesSelectedAndReadyTerminals(t *testing.T) {
	t.Parallel()

	protocolErr := status.Error(codes.InvalidArgument, "unique owner protocol failure")
	eventErr := errors.New("unique owner event failure")
	transportErr := status.Error(codes.Unavailable, "unique owner transport failure")
	cleanupErr := errors.New("unique owner cleanup failure")
	tests := []struct {
		name          string
		cancelStream  bool
		selected      terminalResult
		eventReady    terminalResult
		receiveReady  terminalResult
		cleanupErr    error
		expectedErr   []error
		expectedCause SessionCompletionCause
		expectedRPC   codes.Code
	}{
		{
			name: "selected protocol with stream cancellation", cancelStream: true,
			selected: terminalResult{
				cause: SessionCompletionProtocolFailure, err: protocolErr, clean: false, passthrough: true,
			},
			eventReady: terminalResult{}, receiveReady: terminalResult{}, cleanupErr: cleanupErr,
			expectedErr: []error{protocolErr, cleanupErr}, expectedCause: SessionCompletionProtocolFailure,
			expectedRPC: codes.Internal,
		},
		{
			name: "selected event with stream cancellation", cancelStream: true,
			selected: terminalResult{
				cause: SessionCompletionProtocolFailure, err: eventErr, clean: false, passthrough: false,
			},
			eventReady: terminalResult{}, receiveReady: terminalResult{}, cleanupErr: nil,
			expectedErr: []error{eventErr}, expectedCause: SessionCompletionProtocolFailure,
			expectedRPC: codes.OK,
		},
		{
			name: "selected transport with stream cancellation", cancelStream: true,
			selected: terminalResult{
				cause: SessionCompletionTransportFailure, err: transportErr, clean: false, passthrough: true,
			},
			eventReady: terminalResult{}, receiveReady: terminalResult{}, cleanupErr: nil,
			expectedErr: []error{transportErr}, expectedCause: SessionCompletionTransportFailure,
			expectedRPC: codes.OK,
		},
		{
			name: "buffered event with EOF", cancelStream: false,
			selected: terminalResult{
				cause: SessionCompletionCleanClientClosure, err: io.EOF, clean: true, passthrough: false,
			},
			eventReady: terminalResult{
				cause: SessionCompletionProtocolFailure, err: eventErr, clean: false, passthrough: false,
			},
			receiveReady: terminalResult{}, cleanupErr: nil,
			expectedErr: []error{eventErr}, expectedCause: SessionCompletionProtocolFailure,
			expectedRPC: codes.OK,
		},
		{
			name: "buffered receive with stream cancellation", cancelStream: true,
			selected: terminalResult{
				cause: SessionCompletionCleanClientClosure, err: nil, clean: true, passthrough: false,
			},
			eventReady: terminalResult{},
			receiveReady: terminalResult{
				cause: SessionCompletionTransportFailure, err: transportErr, clean: false, passthrough: true,
			},
			cleanupErr: nil, expectedErr: []error{transportErr},
			expectedCause: SessionCompletionTransportFailure, expectedRPC: codes.OK,
		},
		{
			name: "pure EOF closure", cancelStream: false,
			selected: terminalResult{
				cause: SessionCompletionCleanClientClosure, err: io.EOF, clean: true, passthrough: false,
			},
			eventReady: terminalResult{}, receiveReady: terminalResult{}, cleanupErr: nil,
			expectedErr: nil, expectedCause: SessionCompletionCleanClientClosure, expectedRPC: codes.OK,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange selected and buffered terminals with a valid owner stream context.
			applicationContext := t.Context()
			streamContext, cancelStream := context.WithCancel(t.Context())
			if test.cancelStream {
				cancelStream()
			} else {
				t.Cleanup(cancelStream)
			}
			service := New(applicationContext, nil)
			eventTerminals := make(chan terminalResult, 1)
			receiveTerminals := make(chan terminalResult, 1)
			if test.eventReady.err != nil {
				eventTerminals <- test.eventReady
			}
			if test.receiveReady.err != nil {
				receiveTerminals <- test.receiveReady
			}

			// Act through owner-closure precedence and completion publication.
			terminal := service.applyTerminalPrecedence(
				streamContext, test.selected, eventTerminals, receiveTerminals,
			)
			completion, rpcErr := service.complete(streamContext, terminal, test.cleanupErr)

			// Assert RPC closure behavior and every independent local completion cause.
			assert.Equal(t, test.expectedRPC, status.Code(rpcErr))
			assert.Equal(t, test.expectedCause, completion.Cause)
			if len(test.expectedErr) == 0 {
				require.NoError(t, completion.Err)
			} else {
				for _, expectedErr := range test.expectedErr {
					require.ErrorIs(t, completion.Err, expectedErr)
					assert.Equal(t, 1, strings.Count(completion.Err.Error(), expectedErr.Error()))
				}
			}
			require.NotErrorIs(t, completion.Err, io.EOF)
			require.NotErrorIs(t, completion.Err, context.Canceled)
		})
	}
}

// TestCleanReceivePreservesSelectedEventTerminal verifies half-close cannot replace selected event failure.
func TestCleanReceivePreservesSelectedEventTerminal(t *testing.T) {
	t.Parallel()

	eventErr := errors.New("unique selected event failure before clean receive")
	tests := []struct {
		name          string
		selected      terminalResult
		expectedErr   error
		expectedCause SessionCompletionCause
	}{
		{
			name: "selected event and clean receive",
			selected: terminalResult{
				cause: SessionCompletionProtocolFailure, err: eventErr, clean: false, passthrough: false,
			},
			expectedErr: eventErr, expectedCause: SessionCompletionProtocolFailure,
		},
		{
			name: "pure clean receive",
			selected: terminalResult{
				cause: SessionCompletionUnspecified, err: nil, clean: false, passthrough: false,
			},
			expectedErr: nil, expectedCause: SessionCompletionCleanClientClosure,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange an active stream and one already-buffered clean receive terminal.
			service := New(t.Context(), nil)
			eventTerminals := make(chan terminalResult, 1)
			receiveTerminals := make(chan terminalResult, 1)
			receiveTerminals <- terminalResult{
				cause: SessionCompletionCleanClientClosure, err: nil, clean: true, passthrough: false,
			}

			// Act after an event terminal was selected before the clean receive became observable.
			terminal := service.applyTerminalPrecedence(
				t.Context(), test.selected, eventTerminals, receiveTerminals,
			)
			completion, rpcErr := service.complete(t.Context(), terminal, nil)

			// Assert pure receive closure is clean while an independent selected event remains once.
			require.NoError(t, rpcErr)
			assert.Equal(t, test.expectedCause, completion.Cause)
			if test.expectedErr == nil {
				require.NoError(t, completion.Err)
			} else {
				require.ErrorIs(t, completion.Err, test.expectedErr)
				assert.Equal(t, 1, strings.Count(completion.Err.Error(), test.expectedErr.Error()))
			}
		})
	}
}

// TestApplicationCancellationCollectsReadyReceiveTerminalAfterCleanup verifies late receive publication.
func TestApplicationCancellationCollectsReadyReceiveTerminalAfterCleanup(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange a blocked receive and cleanup barrier around valid application cancellation.
		controller := gomock.NewController(t)
		session := NewMockHostSession(controller)
		stream := NewMockOpenStream(controller)
		applicationContext, cancelApplication := context.WithCancel(t.Context())
		streamContext, cancelStream := context.WithCancel(t.Context())
		t.Cleanup(cancelStream)
		receiveStarted := make(chan struct{})
		releaseReceive := make(chan struct{})
		cleanupStarted := make(chan struct{})
		releaseCleanup := make(chan struct{})
		terminalErr := status.Error(codes.Unavailable, "unique ready receive failure")
		cleanupErr := errors.New("unique ready cleanup failure")
		stream.EXPECT().Context().Return(streamContext).AnyTimes()
		stream.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
			close(receiveStarted)
			<-releaseReceive
			return nil, terminalErr
		})
		session.EXPECT().CancelAndWait(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
			require.NoError(t, ctx.Err())
			close(cleanupStarted)
			<-releaseCleanup
			return cleanupErr
		})
		service := New(applicationContext, session)
		openDone := make(chan error, 1)
		go func() { openDone <- service.open(stream) }()
		<-receiveStarted

		// Act by canceling the application, then publishing the receive terminal before cleanup completes.
		cancelApplication()
		<-cleanupStarted
		close(releaseReceive)
		synctest.Wait()
		close(releaseCleanup)
		synctest.Wait()
		rpcErr := <-openDone
		completion := <-service.Completions()

		// Assert cancellation precedence and RPC status retain the ready transport and cleanup causes once.
		assert.Equal(t, codes.Canceled, status.Code(rpcErr))
		assert.Equal(t, SessionCompletionApplicationCanceled, completion.Cause)
		require.ErrorIs(t, completion.Err, context.Canceled)
		require.ErrorIs(t, completion.Err, terminalErr)
		assert.Equal(t, 1, strings.Count(completion.Err.Error(), context.Canceled.Error()))
		assert.Equal(t, 1, strings.Count(completion.Err.Error(), terminalErr.Error()))
		require.ErrorIs(t, completion.CleanupErr, cleanupErr)
	})
}

// TestApplicationCancellationEndsBlockedReceive verifies application shutdown does not wait for another client frame.
func TestApplicationCancellationEndsBlockedReceive(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// Arrange a receive that remains blocked until the client stream context is canceled.
		ctrl := gomock.NewController(t)
		session := NewMockHostSession(ctrl)
		stream := NewMockOpenStream(ctrl)
		applicationContext, cancelApplication := context.WithCancel(t.Context())
		streamContext, cancelStream := context.WithCancel(t.Context())
		stream.EXPECT().Context().Return(streamContext).AnyTimes()
		receiveBlocked := make(chan struct{})
		receiveReleased := make(chan struct{})
		stream.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
			close(receiveBlocked)
			<-streamContext.Done()
			close(receiveReleased)
			return nil, context.Canceled
		})
		session.EXPECT().CancelAndWait(gomock.Any()).Return(nil)
		service := New(applicationContext, session)
		openDone := make(chan error, 1)
		go func() { openDone <- service.open(stream) }()
		<-receiveBlocked

		// Act by canceling only the application context.
		cancelApplication()
		synctest.Wait()

		// Assert controller completion does not depend on releasing the client receive.
		select {
		case err := <-openDone:
			assert.Equal(t, codes.Canceled, status.Code(err))
		default:
			cancelStream()
			synctest.Wait()
			require.Fail(t, "controller remained blocked in Recv after application cancellation")
		}
		completion := <-service.Completions()
		assert.Equal(t, SessionCompletionApplicationCanceled, completion.Cause)
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
}
