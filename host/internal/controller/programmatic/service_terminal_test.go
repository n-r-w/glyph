//go:build !integration

package programmatic

import (
	"context"
	"errors"
	"io"

	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestTerminalCausePrecedence verifies application, client, transport, and cleanup precedence.
func (s *ServiceSuite) TestTerminalCausePrecedence() {
	transportErr := status.Error(codes.Unavailable, "transport failed")
	plainErr := errors.New("receive failed")
	cleanupErr := errors.New("cleanup failed")
	passthroughErr := status.Error(codes.Unavailable, "unique passthrough terminal failure")
	passthroughCleanupErr := errors.New("unique passthrough cleanup failure")
	controllerErr := errors.New("unique controller terminal failure")
	controllerCleanupErr := errors.New("unique controller cleanup failure")
	tests := map[string]struct {
		appCanceled    bool
		streamCanceled bool
		recvErr        error
		cleanupErr     error
		wantCode       codes.Code
		wantMessages   []string
		wantCause      SessionCompletionCause
		wantErr        error
	}{
		"application cancellation": {
			appCanceled:    true,
			recvErr:        transportErr,
			wantCode:       codes.Canceled,
			wantMessages:   nil,
			wantCause:      SessionCompletionApplicationCanceled,
			wantErr:        context.Canceled,
			streamCanceled: false,
			cleanupErr:     nil,
		},
		"stream cancellation": {
			streamCanceled: true,
			recvErr:        transportErr,
			wantCode:       codes.OK,
			wantMessages:   nil,
			wantCause:      SessionCompletionCleanClientClosure,
			appCanceled:    false,
			cleanupErr:     nil,
			wantErr:        nil,
		},
		"eof": {
			recvErr:        io.EOF,
			wantCode:       codes.OK,
			wantMessages:   nil,
			wantCause:      SessionCompletionCleanClientClosure,
			appCanceled:    false,
			streamCanceled: false,
			cleanupErr:     nil,
			wantErr:        nil,
		},
		"status": {
			recvErr:        transportErr,
			wantCode:       codes.Unavailable,
			wantMessages:   nil,
			wantCause:      SessionCompletionTransportFailure,
			wantErr:        transportErr,
			appCanceled:    false,
			streamCanceled: false,
			cleanupErr:     nil,
		},
		"plain receive error": {
			recvErr:        plainErr,
			wantCode:       codes.Internal,
			wantMessages:   []string{"Programmatic Control controller failed", "receive failed"},
			wantCause:      SessionCompletionTransportFailure,
			wantErr:        plainErr,
			appCanceled:    false,
			streamCanceled: false,
			cleanupErr:     nil,
		},
		"status and cleanup": {
			recvErr:        passthroughErr,
			cleanupErr:     passthroughCleanupErr,
			wantCode:       codes.Unavailable,
			wantMessages:   []string{"unique passthrough terminal failure", "unique passthrough cleanup failure"},
			wantCause:      SessionCompletionTransportFailure,
			wantErr:        passthroughErr,
			appCanceled:    false,
			streamCanceled: false,
		},
		"controller and cleanup": {
			recvErr:        controllerErr,
			cleanupErr:     controllerCleanupErr,
			wantCode:       codes.Internal,
			wantMessages:   []string{"unique controller terminal failure", "unique controller cleanup failure"},
			wantCause:      SessionCompletionTransportFailure,
			wantErr:        controllerErr,
			appCanceled:    false,
			streamCanceled: false,
		},
		"cleanup": {
			recvErr:        io.EOF,
			cleanupErr:     cleanupErr,
			wantCode:       codes.Internal,
			wantMessages:   []string{"clean up Programmatic Control session", "cleanup failed"},
			wantCause:      SessionCompletionCleanupFailure,
			wantErr:        cleanupErr,
			appCanceled:    false,
			streamCanceled: false,
		},
	}
	for name, test := range tests {
		s.Run(name, func() {
			ctrl := gomock.NewController(s.T())
			session := NewMockHostSession(ctrl)
			stream := NewMockOpenStream(ctrl)
			appContext, cancelApp := context.WithCancel(s.T().Context())
			streamContext, cancelStream := context.WithCancel(s.T().Context())
			stream.EXPECT().Context().Return(streamContext).AnyTimes()
			if test.appCanceled {
				cancelApp()
			}
			if test.streamCanceled {
				cancelStream()
			}
			if !test.appCanceled && !test.streamCanceled {
				stream.EXPECT().Recv().Return(nil, test.recvErr)
			}
			session.EXPECT().CancelAndWait(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
				s.NoError(ctx.Err())
				return test.cleanupErr
			})
			service := New(appContext, session)

			err := service.open(stream)
			s.Equal(test.wantCode, status.Code(err))
			for _, message := range test.wantMessages {
				s.Contains(status.Convert(err).Message(), message)
			}
			completion := <-service.Completions()
			s.Equal(test.wantCause, completion.Cause)
			if test.wantErr == nil {
				s.Require().NoError(completion.Err)
			} else {
				s.Require().ErrorIs(completion.Err, test.wantErr)
			}
			s.Require().ErrorIs(completion.CleanupErr, test.cleanupErr)
			cancelApp()
			cancelStream()
		})
	}
}
