//go:build integration

package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/internal/operation"
	"github.com/n-r-w/glyph/internal/testsupport/operationmock"
	"github.com/n-r-w/glyph/internal/testsupport/pluginmock"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// TestChannelReceivesLaterRequestWhileOperationRuns verifies real stream receipt remains active.
func TestChannelReceivesLaterRequestWhileOperationRuns(t *testing.T) {
	t.Parallel()

	// Arrange one SDK service and two prepared Host operations.
	mockController := gomock.NewController(t)
	firstPrepared := operationmock.NewMockOperationPrepared[controllerui.Frame, controllerui.Frame](mockController)
	secondPrepared := operationmock.NewMockOperationPrepared[controllerui.Frame, controllerui.Frame](mockController)
	firstPrepared.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ operation.Reporter[controllerui.Frame]) operation.Outcome[controllerui.Frame] {
			<-ctx.Done()
			return operation.Canceled[controllerui.Frame]()
		},
	)
	firstPrepared.EXPECT().Release()
	secondPrepared.EXPECT().Run(gomock.Any(), gomock.Any()).Return(
		operation.Completed(controllerui.NewFrame(controllerui.FrameSubmitCompleted)),
	)
	secondPrepared.EXPECT().Release()
	secondResult := make(chan error, 1)
	service := pluginmock.NewMockUIService(mockController)
	initializationOperation := pluginmock.NewMockUIInitializeOperation(mockController)
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
		_, waitErr := later.Wait(ctx)
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
	transport := &Service{
		client:           nil,
		openOnce:         sync.Once{},
		openErr:          nil,
		stream:           stream,
		cancel:           cancel,
		closed:           atomic.Bool{},
		mutex:            sync.Mutex{},
		ready:            true,
		writer:           nil,
		progressReporter: operation.Reporter[controllerui.Frame]{}, progressBound: false, failConnection: nil,
	}
	prepare := func(
		_ context.Context,
		command controllerui.Command,
	) (operation.Prepared[controllerui.Frame, controllerui.Frame], error) {
		if command.OperationID == "first" {
			return firstPrepared, nil
		}
		return secondPrepared, nil
	}

	// Act through the production prepared-operation receiver.
	err = runTestOperations(t, transport, t.Context(), func() {}, prepare)

	// Assert the later request completed while the first operation was still active.
	require.NoError(t, err)
	assert.NoError(t, <-secondResult)
}

// TestChannelCancellationReportsActualTargetState verifies targeted cancellation and joining.
func TestChannelCancellationReportsActualTargetState(t *testing.T) {
	t.Parallel()

	// Arrange one target whose work stops only after its operation context is canceled.
	mockController := gomock.NewController(t)
	prepared := operationmock.NewMockOperationPrepared[controllerui.Frame, controllerui.Frame](mockController)
	runStarted := make(chan struct{})
	prepared.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ operation.Reporter[controllerui.Frame]) operation.Outcome[controllerui.Frame] {
			close(runStarted)
			<-ctx.Done()
			return operation.Canceled[controllerui.Frame]()
		},
	)
	prepared.EXPECT().Release()
	resultChannel := make(chan *operationv1.CancelCompleted, 1)
	service := pluginmock.NewMockUIService(mockController)
	initializationOperation := pluginmock.NewMockUIInitializeOperation(mockController)
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
	err := runTestOperations(t, transport, t.Context(), func() {}, func(
		context.Context,
		controllerui.Command,
	) (operation.Prepared[controllerui.Frame, controllerui.Frame], error) {
		return prepared, nil
	})

	// Assert cancellation completes only after the target reports its actual terminal state.
	require.NoError(t, err)
	result := <-resultChannel
	assert.Equal(t, operationv1.TerminalState_TERMINAL_STATE_CANCELED, result.GetTargetState())
}

// TestChannelPreservesPublicOperationErrors verifies Host errors across the real UI SDK stream.
func TestChannelPreservesPublicOperationErrors(t *testing.T) {
	t.Parallel()

	// Arrange one malformed request followed by accepted work that fails with a classified cause.
	mockController := gomock.NewController(t)
	failureCause := errors.New("submit accepted but provider failed completely")
	prepared := operationmock.NewMockOperationPrepared[controllerui.Frame, controllerui.Frame](mockController)
	prepared.EXPECT().Run(gomock.Any(), gomock.Any()).Return(
		operation.Failed[controllerui.Frame]("INTERNAL", failureCause),
	)
	prepared.EXPECT().Release()
	result := make(chan error, 2)
	service := pluginmock.NewMockUIService(mockController)
	initializationOperation := pluginmock.NewMockUIInitializeOperation(mockController)
	service.EXPECT().PrepareInitialize(gomock.Any(), gomock.Any()).Return(initializationOperation, nil)
	initializationOperation.EXPECT().Run(gomock.Any()).Return(new(uiv1.Initialized), nil)
	initializationOperation.EXPECT().Release()
	service.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, host *uisdk.Host) error {
		malformed := new(uiv1.UIRequest)
		malformed.SetSubmit(new(uiv1.SubmitCommand))
		rejected, startErr := host.Start(ctx, "rejected", malformed)
		if startErr != nil {
			return startErr
		}
		_, waitErr := rejected.Wait(ctx)
		result <- waitErr

		valid := new(uiv1.UIRequest)
		valid.SetSubmit(uiv1.SubmitCommand_builder{Text: new("accepted")}.Build())
		failed, startErr := host.Start(ctx, "failed", valid)
		if startErr != nil {
			return startErr
		}
		_, waitErr = failed.Wait(ctx)
		result <- waitErr
		return host.Close(context.WithoutCancel(ctx))
	})
	service.EXPECT().Close().Return(nil)
	transport := openInitializedIntegrationChannel(t, service)

	// Act through the production Host operation receiver and real UI SDK stream.
	err := runTestOperations(t, transport, t.Context(), func() {}, func(
		context.Context,
		controllerui.Command,
	) (operation.Prepared[controllerui.Frame, controllerui.Frame], error) {
		return prepared, nil
	})

	// Assert both public SDK error types preserve their exact categories and complete text.
	require.NoError(t, err)
	rejectionErr := <-result
	var rejection *uisdk.RejectionError
	require.ErrorAs(t, rejectionErr, &rejection)
	assert.Equal(t, "INVALID_ARGUMENT", rejection.Code())
	require.EqualError(t, rejectionErr, "receive UI command: submit text is required")
	failureErr := <-result
	var failure *uisdk.FailureError
	require.ErrorAs(t, failureErr, &failure)
	assert.Equal(t, "INTERNAL", failure.Code())
	require.EqualError(t, failureErr, failureCause.Error())
}

// TestChannelStreamLossWaitsForRelease verifies real UI transport loss joins Host-owned work.
func TestChannelStreamLossWaitsForRelease(t *testing.T) {
	t.Parallel()

	// Arrange one Host operation whose Release is held after stream loss stops Run.
	mockController := gomock.NewController(t)
	runStarted := make(chan struct{})
	runStopped := make(chan struct{})
	releaseGate := make(chan struct{})
	releaseFinished := make(chan struct{})
	prepared := operationmock.NewMockOperationPrepared[controllerui.Frame, controllerui.Frame](mockController)
	prepared.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ operation.Reporter[controllerui.Frame]) operation.Outcome[controllerui.Frame] {
			close(runStarted)
			<-ctx.Done()
			close(runStopped)
			return operation.Canceled[controllerui.Frame]()
		},
	)
	prepared.EXPECT().Release().Do(func() {
		<-releaseGate
		close(releaseFinished)
	})
	service := pluginmock.NewMockUIService(mockController)
	initializationOperation := pluginmock.NewMockUIInitializeOperation(mockController)
	service.EXPECT().PrepareInitialize(gomock.Any(), gomock.Any()).Return(initializationOperation, nil)
	initializationOperation.EXPECT().Run(gomock.Any()).Return(new(uiv1.Initialized), nil)
	initializationOperation.EXPECT().Release()
	service.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, host *uisdk.Host) error {
		request := new(uiv1.UIRequest)
		request.SetSubmit(uiv1.SubmitCommand_builder{Text: new("blocked")}.Build())
		_, startErr := host.Start(ctx, "blocked", request)
		if startErr != nil {
			return startErr
		}
		<-ctx.Done()
		return context.Cause(ctx)
	})
	service.EXPECT().Close().Return(nil)
	transport := openInitializedIntegrationChannel(t, service)
	result := make(chan error, 1)
	go func() {
		result <- runTestOperations(t, transport, t.Context(), func() {}, func(
			context.Context,
			controllerui.Command,
		) (operation.Prepared[controllerui.Frame, controllerui.Frame], error) {
			return prepared, nil
		})
	}()
	<-runStarted

	// Act by canceling the real UI stream context.
	transport.cancel()
	<-runStopped

	// Assert controller execution cannot return until Release finishes.
	select {
	case err := <-result:
		t.Fatalf("RunOperations returned before Release: %v", err)
	default:
	}
	close(releaseGate)
	<-releaseFinished
	require.Error(t, <-result)
}

// TestChannelDeliversIdleExtensionFailureThroughHostReceive verifies public connection-event delivery.
func TestChannelDeliversIdleExtensionFailureThroughHostReceive(t *testing.T) {
	t.Parallel()

	// Arrange a real UI stream whose SDK service receives one production-mapped connection event.
	mockController := gomock.NewController(t)
	received := make(chan *uiv1.HostConnectionEvent, 1)
	service := pluginmock.NewMockUIService(mockController)
	initializationOperation := pluginmock.NewMockUIInitializeOperation(mockController)
	service.EXPECT().PrepareInitialize(gomock.Any(), gomock.Any()).Return(initializationOperation, nil)
	initializationOperation.EXPECT().Run(gomock.Any()).Return(new(uiv1.Initialized), nil)
	initializationOperation.EXPECT().Release()
	service.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, host *uisdk.Host) error {
		notification, receiveErr := host.Receive(ctx)
		if receiveErr != nil {
			return receiveErr
		}
		received <- notification.ConnectionEvent()
		return host.Close(context.WithoutCancel(ctx))
	})
	service.EXPECT().Close().Return(nil)
	transport := openInitializedIntegrationChannel(t, service)
	activated := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- runTestOperations(t, transport, t.Context(), func() { close(activated) }, func(
			context.Context,
			controllerui.Command,
		) (operation.Prepared[controllerui.Frame, controllerui.Frame], error) {
			return nil, errors.New("unexpected UI operation")
		})
	}()
	<-activated

	// Act through typed connection output and the real UI stream.
	require.NoError(
		t,
		transport.ReportError(
			"EXTENSION_UNAVAILABLE",
			"extension crashed-plugin unavailable: extension process exited",
		),
	)
	event := <-received

	// Assert Host.Receive gets the exact no-operation connection failure and the stream closes cleanly.
	assert.Equal(t, "EXTENSION_UNAVAILABLE", event.GetError().GetCode())
	assert.Equal(t, "extension crashed-plugin unavailable: extension process exited", event.GetError().GetText())
	require.NoError(t, <-result)
}

// openInitializedIntegrationChannel opens and initializes one SDK test service.
func openInitializedIntegrationChannel(t *testing.T, service uisdk.Service) *Service {
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
	return &Service{
		client:           nil,
		openOnce:         sync.Once{},
		openErr:          nil,
		stream:           stream,
		cancel:           cancel,
		closed:           atomic.Bool{},
		mutex:            sync.Mutex{},
		ready:            true,
		writer:           nil,
		progressReporter: operation.Reporter[controllerui.Frame]{},
		progressBound:    false,
		failConnection:   nil,
	}
}
