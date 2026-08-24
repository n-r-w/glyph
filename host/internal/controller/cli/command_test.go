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

func TestParseRPCInvocation(t *testing.T) {
	t.Parallel()

	command, err := Parse([]string{
		"rpc", "--extension-dir", "/extensions", "--socket", "run/control.sock",
	})

	require.NoError(t, err)
	assert.Equal(t, ModeRPC, command.Mode)
	assert.Equal(t, "/extensions", command.ExtensionDirectory)
	assert.Equal(t, "run/control.sock", command.SocketPath)
}

func TestParseRPCDefaultsOptionalPaths(t *testing.T) {
	t.Parallel()

	command, err := Parse([]string{"rpc"})

	require.NoError(t, err)
	assert.Equal(t, ModeRPC, command.Mode)
	assert.Empty(t, command.ExtensionDirectory)
	assert.Empty(t, command.SocketPath)
}

func TestParseRPCRejectsUIArguments(t *testing.T) {
	t.Parallel()

	for _, arguments := range [][]string{
		{"rpc", "--ui", "standard-ui"},
		{"rpc", "--ui-dir", "/uis"},
	} {
		_, err := Parse(arguments)

		require.Error(t, err)
		assert.ErrorContains(t, err, "parse Glyph RPC arguments")
	}
}

func TestParseRPCRejectsInvalidArguments(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		arguments []string
		errorText string
	}{
		"positional input": {
			arguments: []string{"rpc", "request"},
			errorText: "glyph RPC mode does not accept positional input",
		},
		"empty extension path": {
			arguments: []string{"rpc", "--extension-dir", " "},
			errorText: "--extension-dir requires a nonempty path",
		},
		"empty socket path": {
			arguments: []string{"rpc", "--socket", " "},
			errorText: "--socket requires a nonempty path",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse(test.arguments)

			require.EqualError(t, err, test.errorText)
		})
	}
}
