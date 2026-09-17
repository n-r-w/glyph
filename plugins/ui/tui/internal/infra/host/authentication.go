package host

import (
	"errors"
	"fmt"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	"github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// mapAuthenticationCommand requires an explicit user choice before starting either sign-in flow.
func mapAuthenticationCommand(method presentation.AuthenticationMethod) (*uiv1.UIRequest, error) {
	var selected uiv1.AuthenticationMethod
	switch method {
	case presentation.AuthenticationMethodBrowser:
		selected = uiv1.AuthenticationMethod_AUTHENTICATION_METHOD_BROWSER
	case presentation.AuthenticationMethodDeviceCode:
		selected = uiv1.AuthenticationMethod_AUTHENTICATION_METHOD_DEVICE_CODE
	case presentation.AuthenticationMethodUnspecified:
		return nil, errors.New("UI authentication method is missing")
	default:
		return nil, fmt.Errorf("unknown UI authentication method %d", method)
	}
	request := new(uiv1.UIRequest)
	request.SetRetryAuthentication(uiv1.RetryAuthenticationCommand_builder{Method: new(selected)}.Build())
	return request, nil
}
