//go:build !integration

package runtime

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

func testInitializationFrame() domainui.Frame {
	return domainui.Frame{
		SessionEntries: nil,
		Kind:           domainui.FrameInitialization,
		Initialization: mo.Some(domainui.Initialization{
			SelectedUIID: "ui",
			StartupContent: []domainui.StartupContent{{
				Severity: domainui.ContentSeverityInformation,
				Text:     "ready",
			}},
			Extensions: []domainui.ExtensionAvailability{{
				PluginID: "tools",
				Path:     "/plugins/tools",
				Tools:    []string{"read"},
			}},
			Availability:   domainui.AvailabilityCheckingAuthentication,
			Models:         nil,
			ModelSelection: mo.Some(domainui.ModelSelection{}),
			SessionInfo:    session.Info{},
		}),
		Lifecycle:        mo.None[domainui.Lifecycle](),
		AuthorizationURL: mo.None[string](),
		Text:             mo.None[string](),
		ModelSelection:   mo.None[domainui.ModelSelection](),
		Sessions:         nil,
		SessionInfo: mo.None[session.
			Info](),
		SessionStatistics: mo.None[session.Statistics](),
		SessionTree:       mo.None[domainui.SessionTree](),
		TreeNavigation:    mo.None[domainui.TreeNavigationResult](),
	}
}

// testLifecycleFrame creates one complete lifecycle mapping fixture.
func testLifecycleFrame() domainui.Frame {
	return domainui.Frame{
		SessionEntries: nil,
		Kind:           domainui.FrameLifecycle,
		Initialization: mo.None[domainui.Initialization](),
		Lifecycle: mo.Some(domainui.Lifecycle{
			Type:               domainui.LifecycleToolExecutionUpdate,
			RunID:              mo.Some("run"),
			Text:               mo.Some("progress"),
			ToolResultContents: mo.None[[]tool.ResultContent](),
			ModelContent:       mo.None[domainui.ModelContent](),
			ModelResponse:      mo.None[domainui.ModelResponse](),
			ToolCallPreview:    mo.None[domainui.ToolCallPreview](),
			FinalToolCall:      mo.None[domainui.FinalToolCall](),
			ToolCallID:         mo.None[string](),
			ToolName:           mo.None[string](),
			ProgressChannel:    mo.Some(domainui.ProgressChannelStdout),
			IsError:            mo.None[bool](),
			Outcome:            mo.None[string](),
			ErrorMessage:       mo.None[string](),
			Availability:       mo.None[domainui.Availability](),
		}),
		AuthorizationURL:  mo.None[string](),
		Text:              mo.None[string](),
		ModelSelection:    mo.None[domainui.ModelSelection](),
		SessionInfo:       mo.None[session.Info](),
		Sessions:          nil,
		SessionStatistics: mo.None[session.Statistics](),
		SessionTree:       mo.None[domainui.SessionTree](),
		TreeNavigation:    mo.None[domainui.TreeNavigationResult](),
	}
}

// testSimpleFrame creates one authorization or information mapping fixture.
func testSimpleFrame(kind domainui.FrameKind, text string) domainui.Frame {
	if kind == domainui.FrameAuthorization {
		return domainui.Frame{
			SessionEntries:    nil,
			Kind:              kind,
			Initialization:    mo.None[domainui.Initialization](),
			Lifecycle:         mo.None[domainui.Lifecycle](),
			AuthorizationURL:  mo.Some(text),
			Text:              mo.None[string](),
			ModelSelection:    mo.None[domainui.ModelSelection](),
			SessionInfo:       mo.None[session.Info](),
			Sessions:          nil,
			SessionStatistics: mo.None[session.Statistics](),
			SessionTree:       mo.None[domainui.SessionTree](),
			TreeNavigation:    mo.None[domainui.TreeNavigationResult](),
		}
	}
	return domainui.Frame{
		SessionEntries:    nil,
		Kind:              kind,
		Initialization:    mo.None[domainui.Initialization](),
		Lifecycle:         mo.None[domainui.Lifecycle](),
		AuthorizationURL:  mo.None[string](),
		Text:              mo.Some(text),
		ModelSelection:    mo.None[domainui.ModelSelection](),
		SessionInfo:       mo.None[session.Info](),
		Sessions:          nil,
		SessionStatistics: mo.None[session.Statistics](),
		SessionTree:       mo.None[domainui.SessionTree](),
		TreeNavigation:    mo.None[domainui.TreeNavigationResult](),
	}
}

// testErrorFrame creates one retryable error mapping fixture.
func testErrorFrame() domainui.Frame {
	return domainui.Frame{
		SessionEntries:    nil,
		Kind:              domainui.FrameError,
		Initialization:    mo.None[domainui.Initialization](),
		Lifecycle:         mo.None[domainui.Lifecycle](),
		AuthorizationURL:  mo.None[string](),
		Text:              mo.Some("safe error"),
		ErrorCode:         mo.Some("AUTHENTICATION_FAILED"),
		ModelSelection:    mo.None[domainui.ModelSelection](),
		SessionInfo:       mo.None[session.Info](),
		Sessions:          nil,
		SessionStatistics: mo.None[session.Statistics](),
		SessionTree:       mo.None[domainui.SessionTree](),
		TreeNavigation:    mo.None[domainui.TreeNavigationResult](),
	}
}

// testModelSelectionFrame creates one Host-confirmed selection frame.
func testModelSelectionFrame() domainui.Frame {
	return domainui.Frame{
		SessionEntries:   nil,
		Kind:             domainui.FrameModelSelectionChanged,
		Initialization:   mo.None[domainui.Initialization](),
		Lifecycle:        mo.None[domainui.Lifecycle](),
		AuthorizationURL: mo.None[string](),
		Text:             mo.None[string](),
		ModelSelection: mo.Some(domainui.ModelSelection{
			ProviderID:      "openrouter",
			ModelID:         "sonnet",
			ReasoningChoice: domainui.ReasoningChoiceHigh,
		}),
		SessionInfo:       mo.None[session.Info](),
		Sessions:          nil,
		SessionStatistics: mo.None[session.Statistics](),
		SessionTree:       mo.None[domainui.SessionTree](),
		TreeNavigation:    mo.None[domainui.TreeNavigationResult](),
	}
}

func testUIReasoningCapabilities(choices ...domainui.ReasoningChoice) domainui.ReasoningCapabilities {
	return domainui.ReasoningCapabilities{
		Supported: true,
		Choices:   choices,
		Default:   choices[len(choices)-1],
	}
}
