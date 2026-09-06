//go:build integration

package uiv1

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/n-r-w/glyph/internal/operation"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestNotificationsPreserveWireOrderDuringDelayedApplication verifies one queue retains later appended state.
func TestNotificationsPreserveWireOrderDuringDelayedApplication(t *testing.T) {
	t.Parallel()

	// Arrange navigation progress, a later append, and completion before the consumer applies any notification.
	host, started := orderedHostOperation(t)
	require.NoError(t, host.handleOperationEvent("navigate", orderedNavigationProgressEvent()))
	require.NoError(t, host.deliverConnectionEvent(orderedSessionEntryAddedEvent()))
	require.NoError(t, host.handleOperationEvent("navigate", orderedNavigationCompletedEvent()))

	// Act by applying the already queued notifications through the sole ordered receive path.
	order := make([]string, 0, 3)
	clientLeaf := "initial"
	for range 3 {
		notification, err := host.Receive(t.Context())
		require.NoError(t, err)
		switch notification.Kind() {
		case NotificationProgress:
			order = append(order, "progress")
			clientLeaf = "navigation-leaf"
		case NotificationConnectionEvent:
			order = append(order, "append")
			clientLeaf = notification.ConnectionEvent().GetSessionEntryAdded().GetEntry().GetId()
		case NotificationCompleted:
			order = append(order, "complete")
		case NotificationFailed:
			require.FailNow(t, "unexpected failed notification")
		}
	}
	completed, err := started.Wait(t.Context())

	// Assert metadata-only completion and delayed application preserve exact order and the appended client leaf.
	require.NoError(t, err)
	require.NotNil(t, completed.GetSessionTreeNavigation())
	assert.Equal(t, []string{"progress", "append", "complete"}, order)
	assert.Equal(t, "appended", clientLeaf)
}

// TestNotificationsDoNotRequireOperationWait verifies an abandoned handle cannot block ordered state delivery.
func TestNotificationsDoNotRequireOperationWait(t *testing.T) {
	t.Parallel()

	// Arrange one started operation whose handle is intentionally never waited.
	host, _ := orderedHostOperation(t)
	require.NoError(t, host.handleOperationEvent("navigate", orderedNavigationProgressEvent()))
	require.NoError(t, host.deliverConnectionEvent(orderedSessionEntryAddedEvent()))
	require.NoError(t, host.handleOperationEvent("navigate", orderedNavigationCompletedEvent()))

	// Act by consuming only the SDK notification queue.
	kinds := make([]NotificationKind, 0, 3)
	for range 3 {
		notification, err := host.Receive(t.Context())
		require.NoError(t, err)
		kinds = append(kinds, notification.Kind())
	}

	// Assert progress, append, and terminal metadata remain healthy without Operation.Wait.
	assert.Equal(t, []NotificationKind{
		NotificationProgress, NotificationConnectionEvent, NotificationCompleted,
	}, kinds)
}

// TestCanceledWaitDoesNotBlockLaterNotifications verifies permanent local abandonment is isolated from delivery.
func TestCanceledWaitDoesNotBlockLaterNotifications(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// Arrange one Wait blocked before Host publishes a terminal event.
		host, started := orderedHostOperation(t)
		waitContext, cancelWait := context.WithCancel(t.Context())
		waitDone := make(chan error, 1)
		go func() {
			_, waitErr := started.Wait(waitContext)
			waitDone <- waitErr
		}()
		synctest.Wait()

		// Act by canceling the local Wait permanently, then publishing later state notifications.
		cancelWait()
		synctest.Wait()
		require.ErrorIs(t, <-waitDone, context.Canceled)
		require.NoError(t, host.handleOperationEvent("navigate", orderedNavigationProgressEvent()))
		require.NoError(t, host.deliverConnectionEvent(orderedSessionEntryAddedEvent()))
		require.NoError(t, host.handleOperationEvent("navigate", orderedNavigationCompletedEvent()))
		for range 3 {
			_, receiveErr := host.Receive(t.Context())
			require.NoError(t, receiveErr)
		}

		// Assert no later Wait is needed to keep the notification consumer healthy.
		assert.Empty(t, host.events)
	})
}

// TestLaterWaitReceivesTerminalAfterLocalCancellation verifies a canceled local wait does not consume settlement.
func TestLaterWaitReceivesTerminalAfterLocalCancellation(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// Arrange one locally canceled Wait before terminal publication.
		host, started := orderedHostOperation(t)
		waitContext, cancelWait := context.WithCancel(t.Context())
		waitDone := make(chan error, 1)
		go func() {
			_, waitErr := started.Wait(waitContext)
			waitDone <- waitErr
		}()
		synctest.Wait()
		cancelWait()
		synctest.Wait()
		require.ErrorIs(t, <-waitDone, context.Canceled)
		require.NoError(t, host.handleOperationEvent("navigate", orderedNavigationCompletedEvent()))

		// Act with a later Wait on the same asynchronous handle.
		completed, err := started.Wait(t.Context())

		// Assert the terminal result remains available and the notification remains independently queued.
		require.NoError(t, err)
		require.NotNil(t, completed.GetSessionTreeNavigation())
		notification, err := host.Receive(t.Context())
		require.NoError(t, err)
		assert.Equal(t, NotificationCompleted, notification.Kind())
	})
}

// TestCanceledWaitBeforeCallPreservesQueuedTerminal verifies an already canceled caller cannot consume settlement.
func TestCanceledWaitBeforeCallPreservesQueuedTerminal(t *testing.T) {
	t.Parallel()

	// Arrange one queued terminal result and a caller context canceled before Wait starts.
	host, started := orderedHostOperation(t)
	require.NoError(t, host.handleOperationEvent("navigate", orderedNavigationCompletedEvent()))
	waitContext, cancelWait := context.WithCancel(t.Context())
	cancelWait()

	// Act with the canceled caller, then wait again with a live context.
	_, canceledErr := started.Wait(waitContext)
	completed, err := started.Wait(t.Context())

	// Assert local cancellation wins without consuming the retained terminal result.
	require.ErrorIs(t, canceledErr, context.Canceled)
	require.NoError(t, err)
	require.NotNil(t, completed.GetSessionTreeNavigation())
}

// TestQueuedTerminalWinsConnectionClosure verifies cleanup cannot hide settled Host work.
func TestQueuedTerminalWinsConnectionClosure(t *testing.T) {
	t.Parallel()

	// Arrange one queued terminal result before the connection closes.
	hostContext, cancelHost := context.WithCancelCause(t.Context())
	host, started := orderedHostOperationWithContext(t, hostContext)
	require.NoError(t, host.handleOperationEvent("navigate", orderedNavigationCompletedEvent()))
	cancelHost(errors.New("UI connection closed after settlement"))

	// Act after both the terminal result and connection cause are ready.
	completed, err := started.Wait(t.Context())

	// Assert the retained terminal result wins over later connection cleanup.
	require.NoError(t, err)
	require.NotNil(t, completed.GetSessionTreeNavigation())
}

// TestConsumedProgressVolumeDoesNotFillOperationQueue verifies progress has one bounded delivery owner.
func TestConsumedProgressVolumeDoesNotFillOperationQueue(t *testing.T) {
	t.Parallel()

	// Arrange one started operation with no Operation.Wait consumer.
	host, _ := orderedHostOperation(t)
	const progressCount = 256

	// Act by publishing and consuming more progress than the former per-operation queue capacity.
	for range progressCount {
		require.NoError(t, host.handleOperationEvent("navigate", orderedNavigationProgressEvent()))
		notification, err := host.Receive(t.Context())
		require.NoError(t, err)
		require.Equal(t, NotificationProgress, notification.Kind())
	}
	require.NoError(t, host.handleOperationEvent("navigate", orderedNavigationCompletedEvent()))
	terminal, err := host.Receive(t.Context())

	// Assert notification consumption, not Wait, drains every progress payload and terminal metadata.
	require.NoError(t, err)
	assert.Equal(t, NotificationCompleted, terminal.Kind())
}

// TestConnectionClosureSettlesPendingWaitWithCompleteCause verifies close causes survive tracker shutdown.
func TestConnectionClosureSettlesPendingWaitWithCompleteCause(t *testing.T) {
	t.Parallel()

	// Arrange one pending operation on a cancelable production Host and tracker.
	hostContext, cancelHost := context.WithCancelCause(t.Context())
	host, started := orderedHostOperationWithContext(t, hostContext)
	cause := errors.New("UI connection closed: transport EOF")

	// Act by closing the Host context and tracker with the complete transport cause.
	cancelHost(cause)
	host.tracker.Close()
	host.closeEvents()
	_, err := started.Wait(t.Context())

	// Assert the pending handle exposes the original close cause instead of a generic closed error.
	require.ErrorIs(t, err, cause)
	assert.Contains(t, err.Error(), "transport EOF")
}

// TestNotificationQueueFailureSettlesPendingWaitWithCompleteCause verifies bounded overflow propagation.
func TestNotificationQueueFailureSettlesPendingWaitWithCompleteCause(t *testing.T) {
	t.Parallel()

	// Arrange one pending operation and a full bounded notification queue.
	hostContext, cancelHost := context.WithCancelCause(t.Context())
	host, started := orderedHostOperationWithContext(t, hostContext)
	for range notificationQueueCapacity {
		require.NoError(t, host.deliverConnectionEvent(orderedSessionEntryAddedEvent()))
	}
	queueErr := host.deliverConnectionEvent(orderedSessionEntryAddedEvent())
	require.ErrorIs(t, queueErr, operation.ErrQueueFull)
	cause := fmt.Errorf("receive Host message: %w", queueErr)

	// Act as connection ownership does after stream receipt fails.
	cancelHost(cause)
	host.tracker.Close()
	host.closeEvents()
	_, err := started.Wait(t.Context())

	// Assert the pending handle retains the bounded queue cause and committed state is not involved.
	require.ErrorIs(t, err, operation.ErrQueueFull)
	assert.Contains(t, err.Error(), "receive Host message")
}

// orderedHostOperation creates one accepted and running navigation operation.
func orderedHostOperation(t *testing.T) (*Host, *Operation) {
	t.Helper()
	return orderedHostOperationWithContext(t, t.Context())
}

// orderedHostOperationWithContext creates one accepted and running navigation operation on the supplied context.
func orderedHostOperationWithContext(t *testing.T, hostContext context.Context) (*Host, *Operation) {
	t.Helper()
	tracker := operation.NewTracker[*uiv1.HostProgress, *uiv1.HostCompleted]()
	writer := operation.NewWriter(func(*uiv1.OpenResponse) error { return nil })
	host := newHost(hostContext, writer, tracker, func(error) {}, func() {})
	started, err := host.Start(t.Context(), "navigate", orderedNavigationRequest())
	require.NoError(t, err)
	require.NoError(t, host.handleOperationEvent("navigate", orderedAcceptedEvent()))
	require.NoError(t, host.handleOperationEvent("navigate", orderedRunningEvent()))
	return host, started
}

// orderedNavigationRequest creates one valid navigation request.
func orderedNavigationRequest() *uiv1.UIRequest {
	request := new(uiv1.UIRequest)
	request.SetNavigateSessionTree(uiv1.NavigateSessionTreeCommand_builder{
		TargetEntryId: new("target"), SummaryMode: new(uiv1.SummaryMode_SUMMARY_MODE_NO_SUMMARY),
		CustomFocus: nil,
	}.Build())
	return request
}

// orderedAcceptedEvent creates one accepted lifecycle event.
func orderedAcceptedEvent() *uiv1.HostEvent {
	event := new(uiv1.HostEvent)
	event.SetAccepted(new(operationv1.Accepted))
	return event
}

// orderedRunningEvent creates one running lifecycle event.
func orderedRunningEvent() *uiv1.HostEvent {
	event := new(uiv1.HostEvent)
	event.SetRunning(new(operationv1.Running))
	return event
}

// orderedNavigationProgressEvent creates one valid navigation progress event.
func orderedNavigationProgressEvent() *uiv1.HostEvent {
	progress := new(uiv1.HostProgress)
	progress.SetSessionTreeNavigation(uiv1.SessionTreeNavigationProgress_builder{
		Tree: new(uiv1.SessionTree), ActiveBranch: nil,
	}.Build())
	event := new(uiv1.HostEvent)
	event.SetProgress(progress)
	return event
}

// orderedSessionEntryAddedEvent creates one valid visible extension-message connection event.
func orderedSessionEntryAddedEvent() *uiv1.HostConnectionEvent {
	entry := uiv1.SessionTreeEntry_builder{
		Id: new("appended"), ParentId: new("navigation-leaf"), CreatedTime: timestamppb.Now(), Label: new(""),
		User: nil, Model: nil, ToolResult: nil, Extension: nil, BranchSummary: nil,
		ExtensionMessage: uiv1.ExtensionMessage_builder{
			ExtensionId: new("extension"), EntryType: new("note"), Text: new("observer appended"),
			Visibility: new(uiv1.ClientVisibility_CLIENT_VISIBILITY_VISIBLE),
		}.Build(),
	}.Build()
	added := new(uiv1.SessionEntryAdded)
	added.SetEntry(entry)
	event := new(uiv1.HostConnectionEvent)
	event.SetSessionEntryAdded(added)
	return event
}

// orderedNavigationCompletedEvent creates one valid metadata-only navigation completion.
func orderedNavigationCompletedEvent() *uiv1.HostEvent {
	completed := new(uiv1.HostCompleted)
	completed.SetSessionTreeNavigation(uiv1.SessionTreeNavigationResult_builder{
		Status:        new(uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_COMMITTED),
		DestinationId: new("navigation-leaf"), ActiveLeafId: new("navigation-leaf"),
		CreatedSummary: nil, NextInput: new("exact input"), Issues: nil,
	}.Build())
	event := new(uiv1.HostEvent)
	event.SetCompleted(completed)
	return event
}
