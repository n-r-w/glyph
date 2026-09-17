//go:build !integration

package host

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	"github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// TestAuthenticationCommandCarriesMethod checks that the TUI sends the selected flow rather than a default.
func TestAuthenticationCommandCarriesMethod(t *testing.T) {
	t.Parallel()
	// Arrange both supported choices and the malformed unspecified choice.
	for _, test := range []struct {
		// method is the application-owned selection.
		method presentation.AuthenticationMethod
		// wire is the matching public-contract value.
		wire uiv1.AuthenticationMethod
	}{
		{method: presentation.AuthenticationMethodBrowser, wire: uiv1.AuthenticationMethod_AUTHENTICATION_METHOD_BROWSER},
		{
			method: presentation.AuthenticationMethodDeviceCode,
			wire:   uiv1.AuthenticationMethod_AUTHENTICATION_METHOD_DEVICE_CODE,
		},
		{
			method: presentation.AuthenticationMethodUnspecified,
			wire:   uiv1.AuthenticationMethod_AUTHENTICATION_METHOD_UNSPECIFIED,
		},
	} {
		command := commandFixture(presentation.CommandRetryAuthentication, mo.None[string]())
		command.AuthenticationMethod = test.method
		// Act through the outgoing command mapper.
		request, err := mapCommand(command)
		// Assert valid methods survive and missing choices fail before any network send.
		if test.method == presentation.AuthenticationMethodUnspecified {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, test.wire, request.GetRetryAuthentication().GetMethod())
	}
}
