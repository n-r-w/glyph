package terminal

import "github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"

const (
	// authenticationSelectorTitle identifies the sign-in method dialog.
	authenticationSelectorTitle = "Sign in"
	// authenticationBrowserLabel describes the local browser callback flow.
	authenticationBrowserLabel = "Browser (on this computer)"
	// authenticationDeviceLabel describes the remote-browser code flow.
	authenticationDeviceLabel = "Device code (SSH / remote)"
	// authorizationCodeLabel explains where the displayed one-time code must be entered.
	authorizationCodeLabel = "Enter this code on the page above: "
)

// authenticationSelectorLines renders the application-owned sign-in choice and focus.
func (model Model) authenticationSelectorLines() []string {
	methods := presentation.AuthenticationMethods()
	lines := make([]string, 0, len(methods)+selectorFixedLineCount)
	lines = append(lines, authenticationSelectorTitle)
	for index, method := range methods {
		prefix := inactiveSelectorPrefix
		if index == model.snapshot.SelectorRow {
			prefix = activeSelectorPrefix
		}
		label := authenticationBrowserLabel
		if method == presentation.AuthenticationMethodDeviceCode {
			label = authenticationDeviceLabel
		}
		lines = append(lines, prefix+label)
	}
	return append(lines, selectorHelpText)
}
