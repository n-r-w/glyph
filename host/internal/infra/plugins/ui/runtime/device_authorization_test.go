//go:build !integration

package runtime

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
)

// TestAuthorizationProgressPreservesDeviceCode checks that the operation progress carries both public values.
func TestAuthorizationProgressPreservesDeviceCode(t *testing.T) {
	t.Parallel()
	// Arrange an authorization frame containing a browser URL and one-time code.
	frame := controllerui.NewFrame(controllerui.FrameAuthorization)
	frame.AuthorizationURL = mo.Some("https://example.test/device")
	frame.AuthorizationCode = mo.Some("ABCD-EFGH")
	// Act through the production protobuf mapper.
	request, err := mapFrame(frame)
	// Assert the code does not disappear before the UI can display it.
	require.NoError(t, err)
	challenge := request.GetEvent().GetProgress().GetAuthorization()
	require.Equal(t, frame.AuthorizationURL.OrEmpty(), challenge.GetUrl())
	require.True(t, challenge.HasUserCode())
	require.Equal(t, "ABCD-EFGH", challenge.GetUserCode())
}
