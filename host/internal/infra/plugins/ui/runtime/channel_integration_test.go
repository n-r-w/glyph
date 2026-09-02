//go:build integration

package runtime

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	"github.com/n-r-w/glyph/internal/operation"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// TestChannelReceivesLaterRequestWhileOperationRuns verifies real stream receipt remains active.
func TestChannelReceivesLaterRequestWhileOperationRuns(t *testing.T) {
	t.Parallel()

	// Arrange one SDK service and two prepared Host operations.
	mockController := gomock.NewController(t)
	firstPrepared := operation.NewMockPrepared[domainui.Frame, domainui.Frame](mockController)
	secondPrepared := operation.NewMockPrepared[domainui.Frame, domainui.Frame](mockController)
	firstPrepared.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ operation.Reporter[domainui.Frame]) operation.Outcome[domainui.Frame] {
			<-ctx.Done()
			return operation.Canceled[domainui.Frame]()
		},
	)
	firstPrepared.EXPECT().Release()
	secondPrepared.EXPECT().Run(gomock.Any(), gomock.Any()).Return(
		operation.Completed(domainui.NewFrame(domainui.FrameSubmitCompleted)),
	)
	secondPrepared.EXPECT().Release()
	secondResult := make(chan error, 1)
	service := uisdk.NewMockService(mockController)
	initializationOperation := uisdk.NewMockInitializeOperation(mockController)
	service.EXPECT().PrepareInitialize(gomock.Any(), gomock.Any()).Return(initializationOperation, nil)
	initializationOperation.EXPECT().Run(gomock.Any()).Return(new(uiv1.Initialized), nil)
	initializationOperation.EXPECT().Release()
	service.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, host *uisdk.Host) error {
		firstRequest := new(uiv1.UIRequest)
		firstRequest.SetSubmit(uiv1.SubmitCommand_builder{Text: new("blocked")}.Build())
		if _, startErr := host.Start(ctx, "first", firstRequest); startErr != nil {
			return startErr
		}
		laterRequest := new(uiv1.UIRequest)
		laterRequest.SetSubmit(uiv1.SubmitCommand_builder{Text: new("later")}.Build())
		later, startErr := host.Start(ctx, "second", laterRequest)
		if startErr != nil {
			return startErr
		}
		_, waitErr := later.Wait(ctx, nil)
		secondResult <- waitErr
		return host.Close(context.WithoutCancel(ctx))
	})
	service.EXPECT().Close().Return(nil)
	client := uisdk.TestClient(t, service)
	streamContext, cancel := context.WithCancel(t.Context())
	stream, err := client.Open(streamContext)
	require.NoError(t, err)
	initialize := new(uiv1.HostRequest)
	initialize.SetInitialize(new(uiv1.Initialization))
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new(initializationOperationID), Request: initialize, Event: nil,
		ConnectionEvent: nil, Close: nil,
	}.Build()))
	for range 3 {
		_, err = stream.Recv()
		require.NoError(t, err)
	}
	transport := &channel{
		stream:           stream,
		cancel:           cancel,
		closed:           atomic.Bool{},
		mutex:            sync.Mutex{},
		ready:            true,
		writer:           nil,
		progressReporter: operation.Reporter[domainui.Frame]{}, progressBound: false, failConnection: nil,
	}
	prepare := func(
		_ context.Context,
		command domainui.Command,
	) (operation.Prepared[domainui.Frame, domainui.Frame], error) {
		if command.OperationID == "first" {
			return firstPrepared, nil
		}
		return secondPrepared, nil
	}

	// Act through the production prepared-operation receiver.
	err = transport.RunOperations(t.Context(), func() {}, prepare)

	// Assert the later request completed while the first operation was still active.
	require.NoError(t, err)
	assert.NoError(t, <-secondResult)
}

// TestChannelCancellationReportsActualTargetState verifies targeted cancellation and joining.
func TestChannelCancellationReportsActualTargetState(t *testing.T) {
	t.Parallel()

	// Arrange one target whose work stops only after its operation context is canceled.
	mockController := gomock.NewController(t)
	prepared := operation.NewMockPrepared[domainui.Frame, domainui.Frame](mockController)
	runStarted := make(chan struct{})
	prepared.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ operation.Reporter[domainui.Frame]) operation.Outcome[domainui.Frame] {
			close(runStarted)
			<-ctx.Done()
			return operation.Canceled[domainui.Frame]()
		},
	)
	prepared.EXPECT().Release()
	resultChannel := make(chan *operationv1.CancelCompleted, 1)
	service := uisdk.NewMockService(mockController)
	initializationOperation := uisdk.NewMockInitializeOperation(mockController)
	service.EXPECT().PrepareInitialize(gomock.Any(), gomock.Any()).Return(initializationOperation, nil)
	initializationOperation.EXPECT().Run(gomock.Any()).Return(new(uiv1.Initialized), nil)
	initializationOperation.EXPECT().Release()
	service.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, host *uisdk.Host) error {
		request := new(uiv1.UIRequest)
		request.SetSubmit(uiv1.SubmitCommand_builder{Text: new("blocked")}.Build())
		if _, startErr := host.Start(ctx, "target", request); startErr != nil {
			return startErr
		}
		select {
		case <-runStarted:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
		cancellation, cancelErr := host.Cancel(ctx, "cancel", "target")
		if cancelErr != nil {
			return cancelErr
		}
		result, waitErr := cancellation.Wait(ctx)
		if waitErr != nil {
			return waitErr
		}
		resultChannel <- result
		return host.Close(context.WithoutCancel(ctx))
	})
	service.EXPECT().Close().Return(nil)
	transport := openInitializedIntegrationChannel(t, service)

	// Act through the production operation owner.
	err := transport.RunOperations(t.Context(), func() {}, func(
		context.Context,
		domainui.Command,
	) (operation.Prepared[domainui.Frame, domainui.Frame], error) {
		return prepared, nil
	})

	// Assert cancellation completes only after the target reports its actual terminal state.
	require.NoError(t, err)
	result := <-resultChannel
	assert.Equal(t, operationv1.TerminalState_TERMINAL_STATE_CANCELED, result.GetTargetState())
}

// openInitializedIntegrationChannel opens and initializes one SDK test service.
func openInitializedIntegrationChannel(t *testing.T, service uisdk.Service) *channel {
	t.Helper()
	client := uisdk.TestClient(t, service)
	streamContext, cancel := context.WithCancel(t.Context())
	stream, err := client.Open(streamContext)
	require.NoError(t, err)
	initialize := new(uiv1.HostRequest)
	initialize.SetInitialize(new(uiv1.Initialization))
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new(initializationOperationID), Request: initialize, Event: nil,
		ConnectionEvent: nil, Close: nil,
	}.Build()))
	for range 3 {
		_, err = stream.Recv()
		require.NoError(t, err)
	}
	return &channel{
		stream: stream, cancel: cancel, closed: atomic.Bool{}, mutex: sync.Mutex{}, ready: true,
		writer: nil, progressReporter: operation.Reporter[domainui.Frame]{}, progressBound: false, failConnection: nil,
	}
}
