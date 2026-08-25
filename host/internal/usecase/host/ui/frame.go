package ui

import domainui "github.com/n-r-w/glyph/host/internal/domain/ui"

// initializationFrame creates the one complete startup frame.
func initializationFrame(initialization domainui.Initialization) domainui.Frame {
	return domainui.Frame{
		Kind: domainui.FrameInitialization, Initialization: initialization,
		Lifecycle: emptyLifecycle(), AuthorizationURL: "", Text: "",
		RetryAuthentication: false, ModelSelection: emptyModelSelection(),
	}
}

// lifecycleFrame creates one complete lifecycle frame.
func lifecycleFrame(lifecycle domainui.Lifecycle) domainui.Frame {
	return domainui.Frame{
		Kind: domainui.FrameLifecycle, Initialization: emptyInitialization(),
		Lifecycle: lifecycle, AuthorizationURL: "", Text: "", RetryAuthentication: false,
		ModelSelection: emptyModelSelection(),
	}
}

// authorizationFrame creates one complete OAuth URL frame.
func authorizationFrame(authorizationURL string) domainui.Frame {
	return domainui.Frame{
		Kind: domainui.FrameAuthorization, Initialization: emptyInitialization(),
		Lifecycle: emptyLifecycle(), AuthorizationURL: authorizationURL,
		Text: "", RetryAuthentication: false, ModelSelection: emptyModelSelection(),
	}
}

// informationFrame creates one complete notification frame.
func informationFrame(text string) domainui.Frame {
	return domainui.Frame{
		Kind: domainui.FrameInformation, Initialization: emptyInitialization(),
		Lifecycle: emptyLifecycle(), AuthorizationURL: "", Text: text,
		RetryAuthentication: false, ModelSelection: emptyModelSelection(),
	}
}

// errorFrame creates one complete safe error frame.
func errorFrame(text string, retryAuthentication bool) domainui.Frame {
	return domainui.Frame{
		Kind: domainui.FrameError, Initialization: emptyInitialization(),
		Lifecycle: emptyLifecycle(), AuthorizationURL: "", Text: text,
		RetryAuthentication: retryAuthentication, ModelSelection: emptyModelSelection(),
	}
}

// modelSelectionChangedFrame confirms one committed catalog selection.
func modelSelectionChangedFrame(selection domainui.ModelSelection) domainui.Frame {
	return domainui.Frame{
		Kind: domainui.FrameModelSelectionChanged, Initialization: emptyInitialization(),
		Lifecycle: emptyLifecycle(), AuthorizationURL: "", Text: "", RetryAuthentication: false,
		ModelSelection: selection,
	}
}

// emptyInitialization returns explicit zero values for non-initialization frames.
func emptyInitialization() domainui.Initialization {
	return domainui.Initialization{
		SelectedUIID: "", StartupContent: nil, Extensions: nil, Availability: 0,
		Models: nil, ModelSelection: emptyModelSelection(),
	}
}

// emptyModelSelection returns explicit zero values for non-selection frames.
func emptyModelSelection() domainui.ModelSelection {
	return domainui.ModelSelection{ProviderID: "", ModelID: "", ReasoningLevel: 0}
}

// emptyLifecycle returns explicit zero values for non-lifecycle frames.
func emptyLifecycle() domainui.Lifecycle {
	return domainui.Lifecycle{
		Type: 0, RunID: "", Text: "", ToolResultContents: nil,
		ModelContent:  domainui.ModelContent{Type: 0, Kind: 0, Position: 0, Text: ""},
		ModelResponse: emptyModelResponse(),
		ToolCallPreview: domainui.ToolCallPreview{
			CallID: "", Name: "", Position: 0, Provisional: false, Fields: nil,
		},
		FinalToolCall: domainui.FinalToolCall{CallID: "", Name: "", Position: 0, Arguments: nil},
		ToolCallID:    "", ToolName: "",
		ProgressChannel: 0, IsError: false, Outcome: "", ErrorMessage: "", Availability: 0,
	}
}

func emptyModelResponse() domainui.ModelResponse {
	return domainui.ModelResponse{
		Text: "", Outcome: "", ErrorMessage: "", Provider: "", Model: "", ResponseModel: nil, ResponseID: "",
		Content: nil,
		Usage: domainui.ModelUsage{
			InputTokens: 0, OutputTokens: 0, CachedInputTokens: 0,
			CacheWriteTokens: 0, ReasoningTokens: 0, TotalTokens: 0,
		},
		Diagnostics: nil,
	}
}

// availabilityLifecycle creates a complete state lifecycle payload.
func availabilityLifecycle(availability domainui.Availability) domainui.Lifecycle {
	return domainui.Lifecycle{
		Type: domainui.LifecycleAvailabilityChanged, RunID: "", Text: "", ToolResultContents: nil,
		ModelContent:  domainui.ModelContent{Type: 0, Kind: 0, Position: 0, Text: ""},
		ModelResponse: emptyModelResponse(),
		ToolCallPreview: domainui.ToolCallPreview{
			CallID: "", Name: "", Position: 0, Provisional: false, Fields: nil,
		},
		FinalToolCall: domainui.FinalToolCall{CallID: "", Name: "", Position: 0, Arguments: nil},
		ToolCallID:    "", ToolName: "", ProgressChannel: 0,
		IsError: false, Outcome: "", ErrorMessage: "", Availability: availability,
	}
}
