//go:build integration

package extensionv1

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestConnectionHandlerFailureLeavesCleanupToOwner checks failure from accepted nested Host work over gRPC.
func TestConnectionHandlerFailureLeavesCleanupToOwner(t *testing.T) {
	t.Parallel()

	// Arrange: an accepted Host handler reports failure and retains its worker until released.
	controller := gomock.NewController(t)
	service := NewMockService(controller)
	registration := NewMockRegisterOperation(controller)
	execution := NewMockExecuteOperation(controller)
	host := NewMockHostService(controller)
	read := NewMockHostOperation(controller)
	failureCause := errors.New("handler detected invalid peer data")
	sourceCause := errors.New("accepted Host operation failed during cleanup")
	handlerResult := make(chan error, 1)
	handlerCanceled := make(chan error, 1)
	workerGate := make(chan struct{})
	released := make(chan struct{})
	releaseWorker := sync.OnceFunc(func() { close(workerGate) })
	service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).Return(registration, nil)
	registration.EXPECT().Run(gomock.Any()).Return(contractRegistration(), nil)
	registration.EXPECT().Release()
	service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(execution, nil)
	execution.EXPECT().Release()
	host.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).Return(read, nil)
	client := newContextTestClient(t, service)
	connection, err := client.Open(t.Context())
	require.NoError(t, err)
	connection.BindHostService(host)
	t.Cleanup(func() { releaseWorker(); require.ErrorIs(t, connection.Close(), failureCause) })
	read.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ *HostProgressReporter) (*extensionpb.HostCompleted, error) {
			failureReport := make(chan error, 1)
			go func() { failureReport <- connection.Fail(failureCause) }()
			// The gate also permits teardown when failure reporting incorrectly joins this worker.
			select {
			case failureErr := <-failureReport:
				handlerResult <- failureErr
			case <-workerGate:
				return nil, sourceCause
			}
			<-ctx.Done()
			handlerCanceled <- context.Cause(ctx)
			<-workerGate
			return nil, sourceCause
		})
	read.EXPECT().Release().Do(func() { close(released) })
	execution.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ *ProgressReporter) (*extensionpb.ToolResult, error) {
			binding, contextErr := ContextFrom(ctx)
			if contextErr != nil {
				return nil, contextErr
			}
			models, contextErr := binding.StartGetModels(ctx)
			if contextErr != nil {
				return nil, contextErr
			}
			_, contextErr = models.Wait(ctx)
			return nil, contextErr
		})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	registerRequest := new(extensionpb.HostRequest)
	registerRequest.SetRegister(new(extensionpb.RegisterRequest))
	registered, err := connection.Start(ctx, "register", registerRequest)
	require.NoError(t, err)
	_, err = registered.Wait(ctx, nil)
	require.NoError(t, err)

	// Act: invoke extension work that initiates the failing Host handler.
	request := new(extensionpb.HostRequest)
	request.SetExecute(extensionpb.ExecuteRequest_builder{
		ToolName: new("contract"), ArgumentsJson: []byte(`{}`), Context: testInvocationIdentity(),
	}.Build())
	invoked, err := connection.Start(ctx, "invoke", request)
	require.NoError(t, err)
	select {
	case err = <-handlerResult:
		require.ErrorIs(t, err, failureCause)
		assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	case <-ctx.Done():
		t.Error("handler Fail waited for its own worker completion")
		return
	}

	// Assert: failure cancels accepted work; only the owner joins its eventual release and source error.
	require.ErrorIs(t, <-handlerCanceled, failureCause)
	_, err = invoked.Wait(ctx, nil)
	require.ErrorIs(t, err, failureCause)
	_, err = connection.Start(ctx, "after-failure", registerRequest)
	require.ErrorIs(t, err, failureCause)
	closeResult := make(chan error, 1)
	go func() { closeResult <- connection.Close() }()
	select {
	case <-closeResult:
		t.Error("Close returned while the accepted Host worker was still held")
		return
	case <-time.After(50 * time.Millisecond):
	}
	releaseWorker()
	select {
	case err = <-closeResult:
		require.ErrorIs(t, err, failureCause)
		require.ErrorIs(t, err, sourceCause)
		require.ErrorContains(t, err, sourceCause.Error())
		require.ErrorIs(t, connection.CompletionFailures(), sourceCause)
	case <-ctx.Done():
		t.Fatal("Close did not complete after worker release")
	}
	select {
	case <-released:
	default:
		t.Error("Close did not join accepted Host worker release")
	}
}
