package plugin

import (
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"

	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// TestGetCapabilitiesIsPure verifies discovery does not open the controlling terminal.
func TestGetCapabilitiesIsPure(t *testing.T) {
	t.Parallel()

	mockController := gomock.NewController(t)
	client := uisdk.TestClient(t, New(
		NewMockTerminal(mockController),
		NewMockProgramFactory(mockController),
	))

	capabilities, err := client.GetCapabilities(t.Context(), &uiv1.GetCapabilitiesRequest{})
	require.NoError(t, err)
	assert.True(t, capabilities.GetControlsTerminal())
}
