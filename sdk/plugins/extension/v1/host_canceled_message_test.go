//go:build !integration

package extensionv1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/internal/operation"
	operationpb "github.com/n-r-w/glyph/pkg/operation/v1"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestContextOperationCanceledMessage checks that the SDK retains received Host cancellation diagnostics.
func TestContextOperationCanceledMessage(t *testing.T) {
	t.Parallel()
	// Arrange a canceled Host terminal in the operation's validated event channel.
	text := "full Host canceled diagnostic\nUnicode: λ  "
	payload := new(extensionpb.HostEvent)
	payload.SetCanceled(operationpb.Canceled_builder{Message: new(text)}.Build())
	mapped, terminal, err := mapHostEvent("host-canceled", hostRequestModels, payload)
	require.NoError(t, err)
	events := make(chan operation.Event[*extensionpb.HostProgress, *extensionpb.HostCompleted], 1)
	events <- mapped
	observed := &contextOperation{
		initiator: nil,
		id:        "host-canceled",
		kind:      hostRequestModels,
		events:    events,
		done:      make(chan struct{}),
	}
	// Act through the actual received-terminal reader.
	_, cause := observed.wait(t.Context(), nil)
	// Assert that the SDK preserves text and cancellation identity without a nested failure category.
	require.True(t, terminal)
	require.Equal(t, text, mapped.Message)
	require.ErrorContains(t, cause, text)
	require.ErrorIs(t, cause, context.Canceled)
	require.IsType(t, &CanceledError{}, cause)
}
