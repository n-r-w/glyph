package ui

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// initializationFrame creates the one complete startup frame.
func initializationFrame(initialization domainui.Initialization) domainui.Frame {
	return domainui.Frame{
		SessionEntries:      nil,
		Kind:                domainui.FrameInitialization,
		Initialization:      mo.Some(initialization),
		Lifecycle:           mo.None[domainui.Lifecycle](),
		AuthorizationURL:    mo.None[string](),
		Text:                mo.None[string](),
		RetryAuthentication: mo.None[bool](),
		ModelSelection:      mo.None[domainui.ModelSelection](),
		SessionInfo:         mo.None[session.Info](),
		Sessions:            nil,
		SessionStatistics:   mo.None[session.Statistics](),
	}
}

// lifecycleFrame creates one complete lifecycle frame.
func lifecycleFrame(lifecycle domainui.Lifecycle) domainui.Frame {
	return domainui.Frame{
		SessionEntries:      nil,
		Kind:                domainui.FrameLifecycle,
		Initialization:      mo.None[domainui.Initialization](),
		Lifecycle:           mo.Some(lifecycle),
		AuthorizationURL:    mo.None[string](),
		Text:                mo.None[string](),
		RetryAuthentication: mo.None[bool](),
		ModelSelection:      mo.None[domainui.ModelSelection](),
		SessionInfo:         mo.None[session.Info](),
		Sessions:            nil,
		SessionStatistics:   mo.None[session.Statistics](),
	}
}

// authorizationFrame creates one complete OAuth URL frame.
func authorizationFrame(authorizationURL string) domainui.Frame {
	return domainui.Frame{
		SessionEntries:      nil,
		Kind:                domainui.FrameAuthorization,
		Initialization:      mo.None[domainui.Initialization](),
		Lifecycle:           mo.None[domainui.Lifecycle](),
		AuthorizationURL:    mo.Some(authorizationURL),
		Text:                mo.None[string](),
		RetryAuthentication: mo.None[bool](),
		ModelSelection:      mo.None[domainui.ModelSelection](),
		SessionInfo:         mo.None[session.Info](),
		Sessions:            nil,
		SessionStatistics:   mo.None[session.Statistics](),
	}
}

// informationFrame creates one complete notification frame.
func informationFrame(text string) domainui.Frame {
	return domainui.Frame{
		SessionEntries:      nil,
		Kind:                domainui.FrameInformation,
		Initialization:      mo.None[domainui.Initialization](),
		Lifecycle:           mo.None[domainui.Lifecycle](),
		AuthorizationURL:    mo.None[string](),
		Text:                mo.Some(text),
		RetryAuthentication: mo.None[bool](),
		ModelSelection:      mo.None[domainui.ModelSelection](),
		SessionInfo:         mo.None[session.Info](),
		Sessions:            nil,
		SessionStatistics:   mo.None[session.Statistics](),
	}
}

// errorFrame creates one complete safe error frame.
func errorFrame(text string, retryAuthentication bool) domainui.Frame {
	return domainui.Frame{
		SessionEntries:      nil,
		Kind:                domainui.FrameError,
		Initialization:      mo.None[domainui.Initialization](),
		Lifecycle:           mo.None[domainui.Lifecycle](),
		AuthorizationURL:    mo.None[string](),
		Text:                mo.Some(text),
		RetryAuthentication: mo.Some(retryAuthentication),
		ModelSelection:      mo.None[domainui.ModelSelection](),
		SessionInfo:         mo.None[session.Info](),
		Sessions:            nil,
		SessionStatistics:   mo.None[session.Statistics](),
	}
}

// modelSelectionChangedFrame confirms one committed catalog selection.
func modelSelectionChangedFrame(selection domainui.ModelSelection) domainui.Frame {
	return domainui.Frame{
		SessionEntries:      nil,
		Kind:                domainui.FrameModelSelectionChanged,
		Initialization:      mo.None[domainui.Initialization](),
		Lifecycle:           mo.None[domainui.Lifecycle](),
		AuthorizationURL:    mo.None[string](),
		Text:                mo.None[string](),
		RetryAuthentication: mo.None[bool](),
		ModelSelection:      mo.Some(selection),
		SessionInfo:         mo.None[session.Info](),
		Sessions:            nil,
		SessionStatistics:   mo.None[session.Statistics](),
	}
}

// availabilityLifecycle creates a complete state lifecycle payload.
func availabilityLifecycle(availability domainui.Availability) domainui.Lifecycle {
	return domainui.Lifecycle{
		Type:               domainui.LifecycleAvailabilityChanged,
		RunID:              mo.None[string](),
		Text:               mo.None[string](),
		ToolResultContents: mo.None[[]tool.ResultContent](),
		ModelContent:       mo.None[domainui.ModelContent](),
		ModelResponse:      mo.None[domainui.ModelResponse](),
		ToolCallPreview:    mo.None[domainui.ToolCallPreview](),
		FinalToolCall:      mo.None[domainui.FinalToolCall](),
		ToolCallID:         mo.None[string](),
		ToolName:           mo.None[string](),
		ProgressChannel:    mo.None[domainui.ProgressChannel](),
		IsError:            mo.None[bool](),
		Outcome:            mo.None[string](),
		ErrorMessage:       mo.None[string](),
		Availability:       mo.Some(availability),
	}
}
