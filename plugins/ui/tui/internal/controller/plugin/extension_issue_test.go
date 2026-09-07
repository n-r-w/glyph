//go:build !integration

package plugin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestMapExtensionIssueUsesNotificationConsumer verifies the single connection-event path maps observer issues.
func TestMapExtensionIssueUsesNotificationConsumer(t *testing.T) {
	t.Parallel()

	// Arrange one typed issue from the unified SDK notification stream.
	connection := new(uiv1.HostConnectionEvent)
	connection.SetExtensionIssue(uiv1.ExtensionIssue_builder{
		ExtensionId: new("example"), HandlerId: new("observer"),
		Code: new("OBSERVER_ERROR"), Text: new("complete cause"),
	}.Build())

	// Act through the connection-event mapper used by the sole notification consumer.
	event, err := DecodeConnectionEvent(connection)

	// Assert the TUI presents complete identity and cause as one error event.
	require.NoError(t, err)
	assert.Equal(t, TextError, event.Text.Kind)
	assert.Equal(t, "extension example handler observer [OBSERVER_ERROR]: complete cause", event.Text.Text)
}
