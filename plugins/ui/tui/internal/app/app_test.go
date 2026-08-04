package app

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

const helperProcessEnvironment = "GLYPH_TUI_HELPER_PROCESS"

// TestUIPluginHelperProcess runs the isolated plugin server used by process tests.
func TestUIPluginHelperProcess(t *testing.T) {
	t.Parallel()
	if os.Getenv(helperProcessEnvironment) == "" {
		return
	}
	require.NoError(t, Serve())
}

// TestServeProcessReportsCapabilitiesWithoutOpeningTTY verifies capability discovery is terminal-free.
func TestServeProcessReportsCapabilitiesWithoutOpeningTTY(t *testing.T) {
	t.Parallel()

	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestUIPluginHelperProcess$")
	command.Env = append(os.Environ(), helperProcessEnvironment+"=1")
	client, err := uisdk.Connect(t.Context(), command)
	require.NoError(t, err)
	assert.True(t, client.Capabilities().GetControlsTerminal())
	assert.Equal(t, uisdk.ProtocolVersion, client.NegotiatedVersion())

	client.Close()
	select {
	case <-client.Done():
	case <-t.Context().Done():
		t.Fatal("TUI plugin process did not stop")
	}
	assert.True(t, client.Exited())
}
