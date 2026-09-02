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
		SessionEntries:    nil,
		Kind:              domainui.FrameInitialization,
		Initialization:    mo.Some(initialization),
		Lifecycle:         mo.None[domainui.Lifecycle](),
		AuthorizationURL:  mo.None[string](),
		Text:              mo.None[string](),
		ErrorCode:         mo.None[string](),
		ModelSelection:    mo.None[domainui.ModelSelection](),
		SessionInfo:       mo.None[session.Info](),
		Sessions:          nil,
		SessionStatistics: mo.None[session.Statistics](),
		SessionTree:       mo.None[domainui.SessionTree](),
		TreeNavigation:    mo.None[domainui.TreeNavigationResult](),
	}
}

// lifecycleFrame creates one complete lifecycle frame.
func lifecycleFrame(lifecycle domainui.Lifecycle) domainui.Frame {
	return domainui.Frame{
		SessionEntries:    nil,
		Kind:              domainui.FrameLifecycle,
		Initialization:    mo.None[domainui.Initialization](),
		Lifecycle:         mo.Some(lifecycle),
		AuthorizationURL:  mo.None[string](),
		Text:              mo.None[string](),
		ErrorCode:         mo.None[string](),
		ModelSelection:    mo.None[domainui.ModelSelection](),
		SessionInfo:       mo.None[session.Info](),
		Sessions:          nil,
		SessionStatistics: mo.None[session.Statistics](),
		SessionTree:       mo.None[domainui.SessionTree](),
		TreeNavigation:    mo.None[domainui.TreeNavigationResult](),
	}
}

// authorizationFrame creates one complete OAuth URL frame.
func authorizationFrame(authorizationURL string) domainui.Frame {
	return domainui.Frame{
		SessionEntries:    nil,
		Kind:              domainui.FrameAuthorization,
		Initialization:    mo.None[domainui.Initialization](),
		Lifecycle:         mo.None[domainui.Lifecycle](),
		AuthorizationURL:  mo.Some(authorizationURL),
		Text:              mo.None[string](),
		ErrorCode:         mo.None[string](),
		ModelSelection:    mo.None[domainui.ModelSelection](),
		SessionInfo:       mo.None[session.Info](),
		Sessions:          nil,
		SessionStatistics: mo.None[session.Statistics](),
		SessionTree:       mo.None[domainui.SessionTree](),
		TreeNavigation:    mo.None[domainui.TreeNavigationResult](),
	}
}

// classifiedErrorFrame creates one connection error with an exact public category.
func classifiedErrorFrame(code, text string) domainui.Frame {
	return domainui.Frame{
		SessionEntries:    nil,
		Kind:              domainui.FrameError,
		Initialization:    mo.None[domainui.Initialization](),
		Lifecycle:         mo.None[domainui.Lifecycle](),
		AuthorizationURL:  mo.None[string](),
		Text:              mo.Some(text),
		ErrorCode:         mo.Some(code),
		ModelSelection:    mo.None[domainui.ModelSelection](),
		SessionInfo:       mo.None[session.Info](),
		Sessions:          nil,
		SessionStatistics: mo.None[session.Statistics](),
		SessionTree:       mo.None[domainui.SessionTree](),
		TreeNavigation:    mo.None[domainui.TreeNavigationResult](),
	}
}

// modelSelectionChangedFrame confirms one committed catalog selection.
func modelSelectionChangedFrame(selection domainui.ModelSelection) domainui.Frame {
	return domainui.Frame{
		SessionEntries:    nil,
		Kind:              domainui.FrameModelSelectionChanged,
		Initialization:    mo.None[domainui.Initialization](),
		Lifecycle:         mo.None[domainui.Lifecycle](),
		AuthorizationURL:  mo.None[string](),
		Text:              mo.None[string](),
		ErrorCode:         mo.None[string](),
		ModelSelection:    mo.Some(selection),
		SessionInfo:       mo.None[session.Info](),
		Sessions:          nil,
		SessionStatistics: mo.None[session.Statistics](),
		SessionTree:       mo.None[domainui.SessionTree](),
		TreeNavigation:    mo.None[domainui.TreeNavigationResult](),
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
