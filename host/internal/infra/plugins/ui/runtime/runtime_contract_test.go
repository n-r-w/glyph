package runtime

import (
	"context"

	"github.com/samber/mo"

	"google.golang.org/grpc"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// GetCapabilities returns the non-terminal capability used by the transport test.
func (*runtimeContractService) GetCapabilities(
	_ context.Context,
	_ *uipb.GetCapabilitiesRequest,
) (*uipb.GetCapabilitiesResponse, error) {
	return uipb.GetCapabilitiesResponse_builder{
		ControlsTerminal: new(false),
	}.Build(), nil
}

// Open receives every Host frame before returning the complete command set.
func (s *runtimeContractService) Open(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
) error {
	for range cap(s.received) {
		request, err := stream.Recv()
		if err != nil {
			return err
		}
		s.received <- request
	}
	for _, response := range runtimeCommandResponses("request", "openrouter", "sonnet") {
		if err := stream.Send(response); err != nil {
			return err
		}
	}
	return nil
}

// runtimeCommandResponses builds the generated alternatives used by runtime command tests.
func runtimeCommandResponses(text string, providerID string, modelID string) []*uipb.OpenResponse {
	return []*uipb.OpenResponse{
		//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active Submit field.
		uipb.OpenResponse_builder{
			Submit: uipb.SubmitCommand_builder{
				Text: new(text),
			}.Build(),
		}.Build(),
		//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active Stop field.
		uipb.OpenResponse_builder{
			Stop: &uipb.StopCommand{},
		}.Build(),
		//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active RetryAuthentication field.
		uipb.OpenResponse_builder{
			RetryAuthentication: &uipb.RetryAuthenticationCommand{},
		}.Build(),
		//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active Quit field.
		uipb.OpenResponse_builder{
			Quit: &uipb.QuitCommand{},
		}.Build(),
		//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active SelectModel field.
		uipb.OpenResponse_builder{
			SelectModel: uipb.SelectModelCommand_builder{
				ProviderId: new(providerID),
				ModelId:    new(modelID),
			}.Build(),
		}.Build(),
		//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active SelectReasoningChoice field.
		uipb.OpenResponse_builder{
			SelectReasoningChoice: uipb.SelectReasoningChoiceCommand_builder{
				Choice: new(uipb.ReasoningChoice_REASONING_CHOICE_XHIGH),
			}.Build(),
		}.Build(),
	}
}

// testInitializationFrame creates one complete initialization mapping fixture.
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
		Lifecycle:           mo.None[domainui.Lifecycle](),
		AuthorizationURL:    mo.None[string](),
		Text:                mo.None[string](),
		RetryAuthentication: mo.None[bool](),
		ModelSelection:      mo.None[domainui.ModelSelection](),
		Sessions:            nil,
		SessionInfo: mo.None[session.
			Info](),
		SessionStatistics: mo.None[session.Statistics](),
		SessionTree:       mo.None[domainui.SessionTree](),
		TreeNavigation:    mo.None[domainui.TreeNavigationResult](),
		TreeFailure:       mo.None[domainui.TreeFailure](),
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
		AuthorizationURL:    mo.None[string](),
		Text:                mo.None[string](),
		RetryAuthentication: mo.None[bool](),
		ModelSelection:      mo.None[domainui.ModelSelection](),
		SessionInfo:         mo.None[session.Info](),
		Sessions:            nil,
		SessionStatistics:   mo.None[session.Statistics](),
		SessionTree:         mo.None[domainui.SessionTree](),
		TreeNavigation:      mo.None[domainui.TreeNavigationResult](),
		TreeFailure:         mo.None[domainui.TreeFailure](),
	}
}

// testSimpleFrame creates one authorization or information mapping fixture.
func testSimpleFrame(kind domainui.FrameKind, text string) domainui.Frame {
	if kind == domainui.FrameAuthorization {
		return domainui.Frame{
			SessionEntries:      nil,
			Kind:                kind,
			Initialization:      mo.None[domainui.Initialization](),
			Lifecycle:           mo.None[domainui.Lifecycle](),
			AuthorizationURL:    mo.Some(text),
			Text:                mo.None[string](),
			RetryAuthentication: mo.None[bool](),
			ModelSelection:      mo.None[domainui.ModelSelection](),
			SessionInfo:         mo.None[session.Info](),
			Sessions:            nil,
			SessionStatistics:   mo.None[session.Statistics](),
			SessionTree:         mo.None[domainui.SessionTree](),
			TreeNavigation:      mo.None[domainui.TreeNavigationResult](),
			TreeFailure:         mo.None[domainui.TreeFailure](),
		}
	}
	return domainui.Frame{
		SessionEntries:      nil,
		Kind:                kind,
		Initialization:      mo.None[domainui.Initialization](),
		Lifecycle:           mo.None[domainui.Lifecycle](),
		AuthorizationURL:    mo.None[string](),
		Text:                mo.Some(text),
		RetryAuthentication: mo.None[bool](),
		ModelSelection:      mo.None[domainui.ModelSelection](),
		SessionInfo:         mo.None[session.Info](),
		Sessions:            nil,
		SessionStatistics:   mo.None[session.Statistics](),
		SessionTree:         mo.None[domainui.SessionTree](),
		TreeNavigation:      mo.None[domainui.TreeNavigationResult](),
		TreeFailure:         mo.None[domainui.TreeFailure](),
	}
}

// testErrorFrame creates one retryable error mapping fixture.
func testErrorFrame() domainui.Frame {
	return domainui.Frame{
		SessionEntries:      nil,
		Kind:                domainui.FrameError,
		Initialization:      mo.None[domainui.Initialization](),
		Lifecycle:           mo.None[domainui.Lifecycle](),
		AuthorizationURL:    mo.None[string](),
		Text:                mo.Some("safe error"),
		RetryAuthentication: mo.Some(true),
		ModelSelection:      mo.None[domainui.ModelSelection](),
		SessionInfo:         mo.None[session.Info](),
		Sessions:            nil,
		SessionStatistics:   mo.None[session.Statistics](),
		SessionTree:         mo.None[domainui.SessionTree](),
		TreeNavigation:      mo.None[domainui.TreeNavigationResult](),
		TreeFailure:         mo.None[domainui.TreeFailure](),
	}
}

// testModelSelectionFrame creates one Host-confirmed selection frame.
func testModelSelectionFrame() domainui.Frame {
	return domainui.Frame{
		SessionEntries:      nil,
		Kind:                domainui.FrameModelSelectionChanged,
		Initialization:      mo.None[domainui.Initialization](),
		Lifecycle:           mo.None[domainui.Lifecycle](),
		AuthorizationURL:    mo.None[string](),
		Text:                mo.None[string](),
		RetryAuthentication: mo.None[bool](),
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
		TreeFailure:       mo.None[domainui.TreeFailure](),
	}
}

func testUIReasoningCapabilities(choices ...domainui.ReasoningChoice) domainui.ReasoningCapabilities {
	return domainui.ReasoningCapabilities{
		Supported: true,
		Choices:   choices,
		Default:   choices[len(choices)-1],
	}
}
