package ui

import domainui "github.com/n-r-w/glyph/host/internal/domain/ui"

// initializationFrame creates the one complete startup frame.
func initializationFrame(initialization domainui.Initialization) domainui.Frame {
	return domainui.Frame{
		Kind: domainui.FrameInitialization, Initialization: initialization,
		Lifecycle: emptyLifecycle(), AuthorizationURL: "", Text: "",
		RetryAuthentication: false,
	}
}

// lifecycleFrame creates one complete lifecycle frame.
func lifecycleFrame(lifecycle domainui.Lifecycle) domainui.Frame {
	return domainui.Frame{
		Kind: domainui.FrameLifecycle, Initialization: emptyInitialization(),
		Lifecycle: lifecycle, AuthorizationURL: "", Text: "", RetryAuthentication: false,
	}
}

// authorizationFrame creates one complete OAuth URL frame.
func authorizationFrame(authorizationURL string) domainui.Frame {
	return domainui.Frame{
		Kind: domainui.FrameAuthorization, Initialization: emptyInitialization(),
		Lifecycle: emptyLifecycle(), AuthorizationURL: authorizationURL,
		Text: "", RetryAuthentication: false,
	}
}

// informationFrame creates one complete notification frame.
func informationFrame(text string) domainui.Frame {
	return domainui.Frame{
		Kind: domainui.FrameInformation, Initialization: emptyInitialization(),
		Lifecycle: emptyLifecycle(), AuthorizationURL: "", Text: text,
		RetryAuthentication: false,
	}
}

// errorFrame creates one complete safe error frame.
func errorFrame(text string, retryAuthentication bool) domainui.Frame {
	return domainui.Frame{
		Kind: domainui.FrameError, Initialization: emptyInitialization(),
		Lifecycle: emptyLifecycle(), AuthorizationURL: "", Text: text,
		RetryAuthentication: retryAuthentication,
	}
}

// emptyInitialization returns explicit zero values for non-initialization frames.
func emptyInitialization() domainui.Initialization {
	return domainui.Initialization{
		SelectedUIID: "", StartupContent: nil, Extensions: nil, Availability: 0,
	}
}

// emptyLifecycle returns explicit zero values for non-lifecycle frames.
func emptyLifecycle() domainui.Lifecycle {
	return domainui.Lifecycle{
		Type: 0, RunID: "", Position: 0, Text: "", ToolCallID: "", ToolName: "",
		ProgressChannel: 0, IsError: false, Outcome: "", ErrorMessage: "", Availability: 0,
	}
}

// availabilityLifecycle creates a complete state lifecycle payload.
func availabilityLifecycle(availability domainui.Availability) domainui.Lifecycle {
	return domainui.Lifecycle{
		Type: domainui.LifecycleAvailabilityChanged, RunID: "", Position: 0,
		Text: "", ToolCallID: "", ToolName: "", ProgressChannel: 0,
		IsError: false, Outcome: "", ErrorMessage: "", Availability: availability,
	}
}
