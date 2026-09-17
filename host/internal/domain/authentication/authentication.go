// Package authentication defines provider-neutral interactive sign-in values.
package authentication

import "github.com/samber/mo"

// Method identifies the user-selected interactive authentication flow.
type Method uint8

const (
	// MethodUnspecified represents a missing sign-in choice.
	MethodUnspecified Method = iota
	// MethodBrowser authorizes through a browser and a local callback.
	MethodBrowser
	// MethodDeviceCode authorizes with a code entered in a browser on any computer.
	MethodDeviceCode
)

// Challenge contains the user-facing information for an active sign-in attempt.
type Challenge struct {
	// URL opens the provider's authorization page.
	URL string
	// UserCode is present when the user must enter a one-time code on that page.
	UserCode mo.Option[string]
}
