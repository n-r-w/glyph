//go:build !integration

package extensionv1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/internal/operation"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestLocalWaitCancellationRetainsRemoteTerminal checks that callers can await the remote terminal
// after an interrupted local wait.
func TestLocalWaitCancellationRetainsRemoteTerminal(t *testing.T) {
	t.Parallel()
	// Arrange an operation whose remote terminal has not arrived.
	events := make(chan operation.Event[*extensionpb.ToolProgress, *extensionpb.ExtensionCompleted], 1)
	observed := &Operation{connection: nil, id: "local-wait", kind: requestExecute, events: events}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// Act with an interrupted local wait before delivering the remote canceled terminal.
	_, localCause := observed.Wait(ctx, nil)
	text := "later remote canceled diagnostic"
	events <- operation.Event[*extensionpb.ToolProgress, *extensionpb.ExtensionCompleted]{
		ID: "local-wait", Kind: operation.EventCanceled, Progress: nil, Result: nil, Code: "", Message: text,
	}
	_, remoteCause := observed.Wait(t.Context(), nil)
	// Assert that the SDK returns the local error immediately and receives the remote terminal on the later wait.
	require.ErrorIs(t, localCause, context.Canceled)
	require.NotContains(t, localCause.Error(), text)
	require.ErrorIs(t, remoteCause, context.Canceled)
	require.ErrorContains(t, remoteCause, text)
	require.IsType(t, &CanceledError{}, remoteCause)
	require.Empty(t, events)
}
