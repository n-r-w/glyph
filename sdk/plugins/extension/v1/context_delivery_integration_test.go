//go:build integration

package extensionv1

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	operationpb "github.com/n-r-w/glyph/pkg/operation/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/internal/operation"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestHostRunningQueueFailureStopsConnection verifies lost lifecycle delivery settles the entire stream.
func TestHostRunningQueueFailureStopsConnection(t *testing.T) {
	t.Parallel()

	// Arrange: fill the shared ordered writer without starting its transport drain.
	connection, delivery, _ := isolatedHostDelivery(t)
	for {
		err := connection.writer.Enqueue(new(extensionpb.OpenRequest), nil)
		if err != nil {
			require.ErrorIs(t, err, operation.ErrQueueFull)
			break
		}
	}

	// Act: fail running-event delivery after the operation has been accepted.
	err := delivery.Running("operation")

	// Assert: the initiator cannot remain waiting on a connection that lost its lifecycle event.
	require.ErrorIs(t, err, operation.ErrQueueFull)
	require.Error(t, connection.connectionError())
	require.ErrorIs(t, connection.ctx.Err(), context.Canceled)
}

// TestHostInvalidTerminalCategoryStopsConnection verifies invalid local categories retain their cause and stop waiting.
func TestHostInvalidTerminalCategoryStopsConnection(t *testing.T) {
	t.Parallel()

	// Arrange: supply an invalid local terminal category with its original cause.
	connection, delivery, _ := isolatedHostDelivery(t)
	cause := errors.New("complete local cause")

	// Act: reject the invalid result instead of publishing an open-ended category.
	_, err := delivery.Terminal("operation", operation.Failed[*extensionpb.HostCompleted]("UNKNOWN", cause))

	// Assert: invalid local output terminates connection work and retains the complete failure.
	require.Error(t, err)
	require.ErrorIs(t, err, cause)
	require.ErrorContains(t, connection.connectionError(), cause.Error())
	require.ErrorIs(t, connection.ctx.Err(), context.Canceled)
}

// TestHostDuplicateIDsAndInactiveCancellation verifies independent rejection without losing the accepted target.
func TestHostDuplicateIDsAndInactiveCancellation(t *testing.T) {
	t.Parallel()

	// Arrange: keep one accepted catalog read active while the writer records public lifecycle envelopes.
	controller := gomock.NewController(t)
	host := NewMockHostService(controller)
	read := NewMockHostOperation(controller)
	connection, _, frames := isolatedHostDelivery(t)
	connection.BindHostService(host)
	writerDone := make(chan error, 1)
	go func() { writerDone <- connection.writer.Run(connection.ctx) }()
	t.Cleanup(func() { connection.writer.Close(); <-writerDone })
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)
	host.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).Return(read, nil)
	read.EXPECT().
		Run(gomock.Any()).
		DoAndReturn(func(context.Context) (*extensionpb.HostCompleted, error) { <-gate; return emptyModelCatalogue(), nil })
	read.EXPECT().Release()
	request := new(extensionpb.ExtensionRequest)
	request.SetGetModels(extensionpb.GetModelsRequest_builder{Context: nil}.Build())
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, connection.handleHostRequest("same", request))
	require.NotNil(t, awaitHostDisconnectSignal(t, ctx, frames, "acceptance").GetEvent().GetAccepted())
	require.NotNil(t, awaitHostDisconnectSignal(t, ctx, frames, "running").GetEvent().GetRunning())

	// Act: send a duplicate in the extension namespace while its original request remains active.
	require.NoError(t, connection.handleHostRequest("same", request))
	rejected := awaitHostDisconnectSignal(t, ctx, frames, "duplicate rejection")
	assert.Equal(t, "OPERATION_ID_IN_USE", rejected.GetEvent().GetRejected().GetCode())
	_, active := connection.hostOwner.Cancellation("same")
	require.True(t, active)
	release()
	require.NotNil(t, awaitHostDisconnectSignal(t, ctx, frames, "completion").GetEvent().GetCompleted())
	if _, err := connection.hostOwner.CancelAndWait(ctx, "same"); err != nil {
		require.ErrorIs(t, err, operation.ErrTargetNotActive)
	}
	cancellation := new(extensionpb.ExtensionRequest)
	cancellation.SetCancel(operationpb.CancelOperation_builder{TargetOperationId: new("same")}.Build())
	require.NoError(t, connection.handleHostRequest("cancel", cancellation))
	inactive := awaitHostDisconnectSignal(t, ctx, frames, "inactive cancellation rejection")

	// Assert: the completion-before-cancel outcome is TARGET_NOT_ACTIVE and does not fail the connection.
	assert.Equal(t, "TARGET_NOT_ACTIVE", inactive.GetEvent().GetRejected().GetCode())
	assert.Contains(t, inactive.GetEvent().GetRejected().GetMessage(), "same")
	assert.NoError(t, connection.connectionError())
	assert.NoError(t, connection.ctx.Err())
}

// isolatedHostDelivery connects the production owner and writer without a network sender.
func isolatedHostDelivery(t *testing.T) (*Connection, *hostDelivery, <-chan *extensionpb.OpenRequest) {
	t.Helper()
	ctx, cancel := context.WithCancelCause(t.Context())
	frames := make(chan *extensionpb.OpenRequest, 256)
	connection := &Connection{
		ctx:           ctx,
		cancel:        cancel,
		stream:        nil,
		writer:        operation.NewWriter(func(frame *extensionpb.OpenRequest) error { frames <- frame; return nil }),
		tracker:       operation.NewTracker[*extensionpb.ToolProgress, *extensionpb.ExtensionCompleted](),
		hostOwner:     nil,
		hostService:   nil,
		mutex:         sync.Mutex{},
		kinds:         make(map[string]requestKind),
		err:           nil,
		completionErr: nil, completionFailures: nil,
		writerDone:  nil,
		receiveDone: nil,
		closeOnce:   sync.Once{},
	}
	delivery := &hostDelivery{connection: connection}
	connection.hostOwner = operation.NewOwner[struct{}, *extensionpb.HostCompleted](ctx, delivery)
	t.Cleanup(func() { cancel(context.Canceled); connection.hostOwner.Close(); connection.writer.Close() })
	return connection, delivery, frames
}
