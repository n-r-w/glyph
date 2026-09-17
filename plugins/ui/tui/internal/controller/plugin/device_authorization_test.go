//go:build !integration

package plugin

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestDeviceAuthorizationProgressRetainsCode checks the public input decoder before application projection.
func TestDeviceAuthorizationProgressRetainsCode(t *testing.T) {
	t.Parallel()
	// Arrange device-code authorization progress from the Host.
	progress := new(uiv1.HostProgress)
	progress.SetAuthorization(uiv1.AuthorizationRequest_builder{
		Url: new("https://example.test/device"), UserCode: new("ABCD-EFGH"),
	}.Build())
	// Act at the TUI transport boundary.
	payload, err := mapHostProgress(progress)
	// Assert both values reach the application without losing code presence.
	require.NoError(t, err)
	require.Equal(t, TextAuthorization, payload.Text.Kind)
	require.Equal(t, "https://example.test/device", payload.Text.Text)
	require.Equal(t, mo.Some("ABCD-EFGH"), payload.Text.AuthorizationCode)
}
