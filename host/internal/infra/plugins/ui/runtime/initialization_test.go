//go:build !integration

package runtime

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestCallerCancellationStartsInitializationGraceBeforeWriterJoin verifies cancellation ordering with blocked raw calls.
func TestCallerCancellationStartsInitializationGraceBeforeWriterJoin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Arrange blocked raw Send and Recv calls and an observable grace starter.
		controller := gomock.NewController(t)
		stream := NewMockUIService_OpenClient[uiv1.OpenRequest, uiv1.OpenResponse](controller)
		sendStarted := make(chan struct{})
		receiveStarted := make(chan struct{})
		releaseSend := make(chan struct{})
		releaseReceive := make(chan struct{})
		stream.EXPECT().Context().Return(t.Context()).AnyTimes()
		stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(*uiv1.OpenRequest) error {
			close(sendStarted)
			<-releaseSend
			return context.Canceled
		})
		stream.EXPECT().Recv().DoAndReturn(func() (*uiv1.OpenResponse, error) {
			close(receiveStarted)
			<-releaseReceive
			return nil, context.Canceled
		})
		service := New()
		service.stream = stream
		startupCause := errors.New("undelivered initialization report source")
		service.startupReport = startup.LoadReport{
			Issues:     []startup.Issue{{PluginIDs: nil, Path: "", Err: startupCause}},
			Extensions: nil,
		}
		callerCause := errors.New("caller canceled initialization")
		ctx, cancel := context.WithCancelCause(t.Context())
		graceStarted := make(chan struct{})
		result := make(chan error, 1)
		request := new(uiv1.HostRequest)
		request.SetInitialize(new(uiv1.Initialization))
		initialization := uiv1.OpenRequest_builder{
			OperationId: new(initializationOperationID), Request: request, Event: nil,
			ConnectionEvent: nil, Close: nil,
		}.Build()
		go func() {
			result <- service.initialize(ctx, initialization, func() { close(graceStarted) })
		}()
		synctest.Wait()

		// Act after both original transport calls are active.
		cancel(callerCause)
		synctest.Wait()

		// Assert grace started while the initialization writer and receive call remain blocked.
		sendActive := false
		select {
		case <-sendStarted:
			sendActive = true
		default:
		}
		receiveActive := false
		select {
		case <-receiveStarted:
			receiveActive = true
		default:
		}
		startedBeforeJoin := false
		select {
		case <-graceStarted:
			startedBeforeJoin = true
		default:
		}
		assert.True(t, sendActive, "raw Send must be active before cancellation")
		assert.True(t, receiveActive, "raw Recv must be active before cancellation")
		assert.True(t, startedBeforeJoin, "caller cancellation must start grace before the blocked writer join")

		// Release test transport work so the failed assertion cannot leave bubble goroutines blocked.
		close(releaseSend)
		close(releaseReceive)
		synctest.Wait()
		initializationErr := <-result
		require.Error(t, initializationErr)
		require.ErrorIs(t, initializationErr, startupCause)
	})
}
