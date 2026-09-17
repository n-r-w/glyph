package presentation

import inputcontroller "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"

// AuthenticationMethods returns the sign-in choices in their presentation order.
func AuthenticationMethods() []AuthenticationMethod {
	return []AuthenticationMethod{AuthenticationMethodBrowser, AuthenticationMethodDeviceCode}
}

// openAuthenticationSelector asks for an explicit sign-in choice without starting network work.
func (model interaction) openAuthenticationSelector() (interaction, *commandIntent) {
	model.selectorOpen = true
	model.authenticationSelector = true
	model.sessionSelector = false
	model.selectorRow = 0
	return model, nil
}

// updateAuthenticationSelector handles only sign-in method navigation, confirmation, and cancellation.
func (model interaction) updateAuthenticationSelector(key inputcontroller.Key) (interaction, *commandIntent) {
	methods := AuthenticationMethods()
	switch key.Code {
	case inputcontroller.KeyUp:
		model.selectorRow = (model.selectorRow - 1 + len(methods)) % len(methods)
	case inputcontroller.KeyDown:
		model.selectorRow = (model.selectorRow + 1) % len(methods)
	case inputcontroller.KeyEnter:
		command := emptyCommand(CommandRetryAuthentication)
		command.AuthenticationMethod = methods[model.selectorRow]
		model = model.cancelSelector()
		return model.emitCommand(command)
	case inputcontroller.KeyEscape:
		model = model.cancelSelector()
	}
	return model, nil
}
