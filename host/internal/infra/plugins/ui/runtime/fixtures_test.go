package runtime

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// testInitialization provides typed startup state for transport scenarios.
func testInitialization() hostui.Initialization {
	return hostui.Initialization{
		SelectedUIID:   "ui",
		StartupContent: []hostui.StartupContent{{Severity: hostui.ContentSeverityInformation, Text: "ready"}},
		Extensions: []hostui.ExtensionAvailability{
			{PluginID: "tools", Path: "/plugins/tools", Tools: []string{"read"}},
		},
		Availability:   hostui.AvailabilityCheckingAuthentication,
		Models:         nil,
		ModelSelection: mo.Some(model.Selection{}),
		SessionInfo:    session.Info{},
	}
}

// testLifecycleFrame creates one complete lifecycle mapping fixture.
func testLifecycleFrame() controllerui.Frame {
	return controllerui.Frame{
		NextInput:      mo.None[string](),
		SessionEntries: nil,
		Kind:           controllerui.FrameLifecycle,

		Lifecycle: mo.Some(controllerui.Lifecycle{
			Type:               controllerui.LifecycleToolExecutionUpdate,
			RunID:              mo.Some("run"),
			Text:               mo.Some("progress"),
			ToolResultContents: mo.None[[]tool.ResultContent](),
			ModelContent:       mo.None[controllerui.ModelContent](),
			ModelResponse:      mo.None[controllerui.ModelResponse](),
			ToolCallPreview:    mo.None[controllerui.ToolCallPreview](),
			FinalToolCall:      mo.None[controllerui.FinalToolCall](),
			ToolCallID:         mo.None[string](),
			ToolName:           mo.None[string](),
			ProgressChannel:    mo.Some(controllerui.ProgressChannelStdout),
			IsError:            mo.None[bool](),
			Outcome:            mo.None[string](),
			ErrorMessage:       mo.None[string](),
		}),
		AuthorizationURL: mo.None[string](),

		ModelSelection:         mo.None[model.Selection](),
		SessionInfo:            mo.None[session.Info](),
		Sessions:               nil,
		SessionStatistics:      mo.None[session.Statistics](),
		SessionTree:            mo.None[controllerui.SessionTree](),
		TreeNavigation:         mo.None[controllerui.TreeNavigationResult](),
		TreeNavigationProgress: mo.None[controllerui.TreeNavigationProgress](),
	}
}
