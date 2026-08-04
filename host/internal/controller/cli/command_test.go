package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseUIInvocation verifies invocation-scoped UI flags and shared ID normalization.
func TestParseUIInvocation(t *testing.T) {
	t.Parallel()

	command, err := Parse([]string{
		"--extension-dir", "/extensions", "--ui-dir", "/uis", "--ui", " Standard_UI ",
	})

	require.NoError(t, err)
	assert.Equal(t, ModeUI, command.Mode)
	assert.Equal(t, "/extensions", command.ExtensionDirectory)
	assert.Equal(t, "/uis", command.UIDirectory)
	assert.Equal(t, "standard-ui", command.UIID)
}

// TestParseUIRejectsPositionalInput verifies task text cannot bypass the UI command stream.
func TestParseUIRejectsPositionalInput(t *testing.T) {
	t.Parallel()

	_, err := Parse([]string{"unexpected"})

	require.Error(t, err)
	assert.ErrorContains(t, err, "does not accept positional input")
}

// TestParseDelegatesHeadlessRun verifies the accepted one-shot command remains unchanged.
func TestParseDelegatesHeadlessRun(t *testing.T) {
	t.Parallel()

	command, err := Parse([]string{"run", "--extension-dir", "/extensions", "request"})

	require.NoError(t, err)
	assert.Equal(t, ModeHeadless, command.Mode)
	assert.Equal(t, "request", command.Headless.UserText)
	assert.Equal(t, "/extensions", command.Headless.ExtensionDirectory)
}

// TestParseHeadlessRejectsUIFlags verifies UI catalog inputs remain isolated from headless mode.
func TestParseHeadlessRejectsUIFlags(t *testing.T) {
	t.Parallel()

	_, err := Parse([]string{"run", "--ui", "standard-ui", "request"})

	require.Error(t, err)
	assert.ErrorContains(t, err, "cannot be combined")
}
