//go:build integration

package app

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// extensionQueueSSE requests a tool that waits for acceptance of both extension messages.
	extensionQueueSSE = `data: {"type":"response.completed","response":{"id":"resp-extension-queue",` +
		`"model":"gpt-active","status":"completed","output":[{"id":"fc-1","type":"function_call",` +
		`"call_id":"call-1","name":"external","arguments":"{\"mode\":\"queue-messages\"}",` +
		`"status":"completed"}]}}` + "\n\n" + "data: [DONE]\n\n"
	// extensionSteerText identifies the extension instruction delivered after the tool batch.
	extensionSteerText = "extension queued steer exact\ntext"
	// extensionFollowUpText identifies the extension task delivered after the final response.
	extensionFollowUpText = "extension queued followUp exact\ntext"
	// extensionQueueAccepted identifies a tool result produced only after queue acceptance.
	extensionQueueAccepted = "extension messages accepted without entries"
)

// TestPublicExtensionQueuedMessages checks nonblocking tool acceptance, delivery priority, and persisted origin.
func TestPublicExtensionQueuedMessages(t *testing.T) {
	t.Parallel()
	// Arrange a real extension whose tool waits for both append operations before returning.
	provider := startQueueProvider(t, 0, extensionQueueSSE, finalResponseSSE, finalResponseSSE)
	fixture := startQueueFixture(t, provider, buildPublicExtensionFixture(t))
	defer fixture.closeOwner(t)

	// Act through public Programmatic Control and wait for the complete agent run.
	require.NoError(t, fixture.stream.Send(userRequest(queueRunID, "first request")))
	terminal := waitForTerminalEvent(t, fixture, queueRunID)

	// Assert steer follows the tool and followUp reaches only the final provider request.
	require.True(t, terminal.HasCompleted(), "agent run did not complete: %v", terminal)
	bodies := provider.snapshot()
	require.Len(t, bodies, 3)
	require.Contains(t, bodies[1], extensionQueueAccepted)
	require.Contains(t, bodies[1], `extension queued steer exact\ntext`)
	require.NotContains(t, bodies[1], "extension queued followUp")
	require.Less(
		t,
		strings.Index(bodies[1], extensionQueueAccepted),
		strings.Index(bodies[1], "extension queued steer"),
	)
	require.Contains(t, bodies[2], `extension queued followUp exact\ntext`)
	entries := sendProgrammaticOperation(t, fixture, "queued-history", func(request *programmaticv1.OpenRequest) {
		programmaticRequest(request).SetGetSessionEntries(new(programmaticv1.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	var messages []*programmaticv1.ExtensionMessage
	for _, entry := range entries {
		if message := entry.GetExtensionMessage(); message != nil {
			messages = append(messages, message)
		}
	}
	require.Len(t, messages, 2)
	require.Equal(t, extensionSteerText, messages[0].GetText())
	require.Equal(t, extensionFollowUpText, messages[1].GetText())
	for _, message := range messages {
		require.Equal(t, "external", message.GetExtensionId())
		require.Equal(t, "queued-message", message.GetEntryType())
	}
	require.Equal(t, programmaticv1.ClientVisibility_CLIENT_VISIBILITY_VISIBLE, messages[0].GetVisibility())
	require.Equal(t, programmaticv1.ClientVisibility_CLIENT_VISIBILITY_HIDDEN, messages[1].GetVisibility())
}
