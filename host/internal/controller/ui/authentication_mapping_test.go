//go:build !integration

package ui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/authentication"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestAuthenticationCommandPreservesSelectedMethod checks explicit browser and device-code selection.
func TestAuthenticationCommandPreservesSelectedMethod(t *testing.T) {
	t.Parallel()
	// Arrange both supported wire methods and their domain values.
	for _, test := range []struct {
		// wire is the selected public sign-in method.
		wire uiv1.AuthenticationMethod
		// method is the corresponding provider-neutral value.
		method authentication.Method
	}{
		{wire: uiv1.AuthenticationMethod_AUTHENTICATION_METHOD_BROWSER, method: authentication.MethodBrowser},
		{wire: uiv1.AuthenticationMethod_AUTHENTICATION_METHOD_DEVICE_CODE, method: authentication.MethodDeviceCode},
	} {
		t.Run(test.wire.String(), func(t *testing.T) {
			t.Parallel()
			request := new(uiv1.UIRequest)
			request.SetRetryAuthentication(uiv1.RetryAuthenticationCommand_builder{Method: new(test.wire)}.Build())
			// Act at the public command boundary.
			command, err := mapUIRequest(request)
			// Assert the chosen method reaches the Host use case unchanged.
			require.NoError(t, err)
			require.Equal(t, test.method, command.AuthenticationMethod)
		})
	}
}

// TestAuthenticationCommandRequiresMethod rejects missing and unknown choices before admission.
func TestAuthenticationCommandRequiresMethod(t *testing.T) {
	t.Parallel()
	// Arrange requests with no valid explicit method.
	for _, method := range []*uiv1.AuthenticationMethod{
		nil, new(uiv1.AuthenticationMethod(0)), new(uiv1.AuthenticationMethod(99)),
	} {
		request := new(uiv1.UIRequest)
		request.SetRetryAuthentication(uiv1.RetryAuthenticationCommand_builder{Method: method}.Build())
		// Act by decoding the public request.
		_, err := mapUIRequest(request)
		// Assert malformed choices are not silently converted to browser authentication.
		require.Error(t, err)
	}
}
