//go:build !integration

package plugin

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestMapCommandsBuildsEveryRetainedOperationRequest verifies retained TUI commands map to UI requests.
func TestMapCommandsBuildsEveryRetainedOperationRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command presentationdomain.Command
	}{
		{name: "submit", command: commandFixture(presentationdomain.CommandSubmit, mo.Some("hello"))},
		{
			name:    "retry authentication",
			command: commandFixture(presentationdomain.CommandRetryAuthentication, mo.None[string]()),
		},
		{name: "create session", command: commandFixture(presentationdomain.CommandCreateSession, mo.None[string]())},
		{name: "list sessions", command: commandFixture(presentationdomain.CommandListSessions, mo.None[string]())},
		{
			name:    "get session information",
			command: commandFixture(presentationdomain.CommandGetSessionInfo, mo.None[string]()),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange the inline payload for mapCommand to verify retained TUI commands map to UI requests.
			// Act by invoking mapCommand to exercise retained TUI commands map to UI requests.
			request, err := mapCommand(test.command)
			// Assert retained TUI commands map to UI requests.
			require.NoError(t, err)
			require.NotNil(t, request)
			assert.NotEqual(t, 0, int(request.WhichRequest()))
		})
	}
}

// TestMapCommandsPreservesSessionAndSelectionValues verifies typed command arguments.
func TestMapCommandsPreservesSessionAndSelectionValues(t *testing.T) {
	t.Parallel()
	// Arrange resume for mapCommand to verify typed command arguments.

	resume := commandFixture(presentationdomain.CommandResumeSession, mo.None[string]())
	resume.SessionID = mo.Some("stored")
	// Act by invoking mapCommand to exercise typed command arguments.
	resumeRequest, err := mapCommand(resume)
	// Assert typed command arguments.
	require.NoError(t, err)
	assert.Equal(t, "stored", resumeRequest.GetResumeSession().GetSessionId())

	name := commandFixture(presentationdomain.CommandSetSessionName, mo.None[string]())
	name.SessionName = mo.Some("named")
	nameRequest, err := mapCommand(name)
	require.NoError(t, err)
	assert.Equal(t, "named", nameRequest.GetSetSessionName().GetName())

	model := commandFixture(presentationdomain.CommandSelectModel, mo.None[string]())
	model.ProviderID = mo.Some("provider")
	model.ModelID = mo.Some("model")
	modelRequest, err := mapCommand(model)
	require.NoError(t, err)
	assert.Equal(t, "provider", modelRequest.GetSelectModel().GetProviderId())
	assert.Equal(t, "model", modelRequest.GetSelectModel().GetModelId())
}

// commandFixture creates one complete presentation command.
func commandFixture(kind presentationdomain.CommandKind, text mo.Option[string]) presentationdomain.Command {
	return presentationdomain.Command{
		Kind: kind, Text: text, ProviderID: mo.None[string](), ModelID: mo.None[string](),
		ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](), SessionID: mo.None[string](),
		SessionName: mo.None[string](), TreeCommand: mo.None[presentationdomain.TreeCommand](),
	}
}
