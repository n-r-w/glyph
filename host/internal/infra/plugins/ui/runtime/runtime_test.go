//nolint:exhaustruct // Protobuf oneof builders intentionally set only the active field.
package runtime

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"google.golang.org/protobuf/proto"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// runtimeContractService records Host frames and returns every supported UI command.
type runtimeContractService struct {
	uipb.UnimplementedUIServiceServer
	received chan *uipb.OpenRequest
}

// TestChannelMapsEveryFrameAndCommand verifies the complete generated transport boundary.
func TestChannelMapsEveryFrameAndCommand(t *testing.T) {
	t.Parallel()

	service := &runtimeContractService{
		UnimplementedUIServiceServer: uipb.UnimplementedUIServiceServer{},
		received:                     make(chan *uipb.OpenRequest, 6),
	}
	client := uisdk.TestClient(t, service)
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	transport := &channel{stream: stream, mutex: sync.Mutex{}}
	frames := []domainui.Frame{
		testInitializationFrame(),
		testLifecycleFrame(),
		testSimpleFrame(domainui.FrameAuthorization, "https://auth.example"),
		testSimpleFrame(domainui.FrameInformation, "information"),
		testErrorFrame(),
		testModelSelectionFrame(),
	}

	for _, frame := range frames {
		require.NoError(t, transport.Send(frame))
	}
	for range frames {
		assert.NotNil(t, <-service.received)
	}
	for _, expected := range []domainui.Command{
		{Kind: domainui.CommandSubmit, Text: "request"},
		{Kind: domainui.CommandStop, Text: ""},
		{Kind: domainui.CommandRetryAuthentication, Text: ""},
		{Kind: domainui.CommandQuit, Text: ""},
		{Kind: domainui.CommandSelectModel, ProviderID: "openrouter", ModelID: "sonnet"},
		{Kind: domainui.CommandSelectReasoningLevel, ReasoningLevel: domainui.ReasoningLevelXHigh},
	} {
		command, receiveErr := transport.Receive()
		require.NoError(t, receiveErr)
		assert.Equal(t, expected, command)
	}
}

// TestMapInitializationPreservesWarningAndExtensionPath verifies public UI diagnostics mapping.
func TestMapInitializationPreservesWarningAndExtensionPath(t *testing.T) {
	t.Parallel()

	mapped := mapInitialization(domainui.Initialization{
		SelectedUIID: "ui",
		StartupContent: []domainui.StartupContent{{
			Severity: domainui.ContentSeverityWarning, Text: "excluded optional UI",
		}},
		Extensions: []domainui.ExtensionAvailability{{
			PluginID: "tools", Path: "/plugins/tools", Tools: []string{"read"},
		}},
		Availability: domainui.AvailabilityCheckingAuthentication,
		Models: []domainui.ConfiguredModel{{
			ProviderID: "openrouter", ModelID: "sonnet",
			ReasoningLevels: []domainui.ReasoningLevel{domainui.ReasoningLevelNone, domainui.ReasoningLevelXHigh},
		}},
		ModelSelection: domainui.ModelSelection{
			ProviderID: "openrouter", ModelID: "sonnet", ReasoningLevel: domainui.ReasoningLevelXHigh,
		},
	})

	require.Len(t, mapped.GetStartupContent(), 1)
	assert.Equal(t, uipb.ContentSeverity_CONTENT_SEVERITY_WARNING, mapped.GetStartupContent()[0].GetSeverity())
	require.Len(t, mapped.GetExtensions(), 1)
	assert.Equal(t, "/plugins/tools", mapped.GetExtensions()[0].GetPath())
	require.Len(t, mapped.GetModels(), 1)
	assert.Equal(t, "openrouter", mapped.GetModels()[0].GetProviderId())
	assert.Equal(t, []uipb.ReasoningLevel{
		uipb.ReasoningLevel_REASONING_LEVEL_NONE,
		uipb.ReasoningLevel_REASONING_LEVEL_XHIGH,
	}, mapped.GetModels()[0].GetReasoningLevels())
	assert.Equal(t, uipb.ReasoningLevel_REASONING_LEVEL_XHIGH, mapped.GetModelSelection().GetReasoningLevel())
}

// TestReasoningMappingsCoverEveryValue verifies the closed UI reasoning contract.
func TestReasoningMappingsCoverEveryValue(t *testing.T) {
	t.Parallel()

	values := []struct {
		domain domainui.ReasoningLevel
		proto  uipb.ReasoningLevel
	}{
		{domainui.ReasoningLevelNone, uipb.ReasoningLevel_REASONING_LEVEL_NONE},
		{domainui.ReasoningLevelMinimal, uipb.ReasoningLevel_REASONING_LEVEL_MINIMAL},
		{domainui.ReasoningLevelLow, uipb.ReasoningLevel_REASONING_LEVEL_LOW},
		{domainui.ReasoningLevelMedium, uipb.ReasoningLevel_REASONING_LEVEL_MEDIUM},
		{domainui.ReasoningLevelHigh, uipb.ReasoningLevel_REASONING_LEVEL_HIGH},
		{domainui.ReasoningLevelXHigh, uipb.ReasoningLevel_REASONING_LEVEL_XHIGH},
		{domainui.ReasoningLevelMax, uipb.ReasoningLevel_REASONING_LEVEL_MAX},
	}
	for _, value := range values {
		assert.Equal(t, value.proto, mapReasoningLevel(value.domain))
		mapped, err := mapReasoningLevelFromProto(value.proto)
		require.NoError(t, err)
		assert.Equal(t, value.domain, mapped)
	}
	_, err := mapReasoningLevelFromProto(uipb.ReasoningLevel_REASONING_LEVEL_UNSPECIFIED)
	require.Error(t, err)
	_, err = mapReasoningLevelFromProto(uipb.ReasoningLevel(99))
	require.Error(t, err)
}

// TestMapLifecycleCarriesTypedTerminalData verifies the generated terminal contract mapping.
func TestMapLifecycleCarriesTypedTerminalData(t *testing.T) {
	t.Parallel()

	event := emptyTestLifecycle()
	event.Type = domainui.LifecycleMessageEnd
	actualModel := "gpt-actual"
	event.ModelResponse = domainui.ModelResponse{
		Text: "visible", Outcome: "stop", ErrorMessage: "", Provider: "openai-codex",
		Model: "gpt-test", ResponseModel: &actualModel, ResponseID: "resp-1",
		Content: []domainui.ModelResponseContent{
			{Kind: domainui.ModelContentKindReasoning, Text: "hidden"},
			{Kind: domainui.ModelContentKindText, Text: "visible"},
			{Kind: domainui.ModelContentKindRefusal, Text: "cannot help"},
		},
		Usage: domainui.ModelUsage{
			InputTokens: 10, OutputTokens: 7, CachedInputTokens: 4,
			CacheWriteTokens: 1, ReasoningTokens: 3, TotalTokens: 17,
		},
		Diagnostics: []domainui.ModelDiagnostic{{Code: "recovered_output", Message: "safe"}},
	}

	mapped := mapLifecycle(event).GetModelResponse()

	require.NotNil(t, mapped)
	assert.Equal(t, "openai-codex", mapped.GetProvider())
	assert.Equal(t, "gpt-test", mapped.GetModel())
	require.NotNil(t, proto.ValueOrNil(mapped.HasResponseModel(), mapped.GetResponseModel))
	assert.Equal(t, "gpt-actual", mapped.GetResponseModel())
	assert.Equal(t, "resp-1", mapped.GetResponseId())
	assert.Equal(t, int64(17), mapped.GetUsage().GetTotalTokens())
	require.Len(t, mapped.GetContent(), 3)
	assert.Equal(t, uipb.ModelContentKind_MODEL_CONTENT_KIND_REASONING, mapped.GetContent()[0].GetKind())
	assert.Equal(t, uipb.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL, mapped.GetContent()[2].GetKind())
	require.Len(t, mapped.GetDiagnostics(), 1)
}

// TestMapLifecycleCarriesToolResultBlocks verifies ordered text and exact image bytes.
func TestMapLifecycleCarriesToolResultBlocks(t *testing.T) {
	t.Parallel()

	event := emptyTestLifecycle()
	event.Type = domainui.LifecycleToolResult
	event.ToolResultContents = []tool.ResultContent{
		{Kind: tool.ResultContentText, Text: "first"},
		{Kind: tool.ResultContentImage, Image: tool.ResultImage{MediaType: "image/png", Data: []byte{1, 2, 3}}},
	}
	mapped := mapLifecycle(event).GetToolResultContents()
	event.ToolResultContents[1].Image.Data[0] = 9

	require.Len(t, mapped, 2)
	assert.Equal(t, "first", mapped[0].GetText())
	assert.Equal(t, "image/png", mapped[1].GetImage().GetMediaType())
	assert.Equal(t, []byte{1, 2, 3}, mapped[1].GetImage().GetData())
}

// TestMappingRejectsMissingPayloads verifies malformed stream items fail explicitly.
func TestMappingRejectsMissingPayloads(t *testing.T) {
	t.Parallel()

	_, err := mapFrame(domainui.Frame{
		Kind: 0, Initialization: emptyTestInitialization(), Lifecycle: emptyTestLifecycle(),
		AuthorizationURL: "", Text: "", RetryAuthentication: false,
	})
	require.Error(t, err)
	_, err = mapCommand(&uipb.OpenResponse{})
	require.Error(t, err)
}

// GetCapabilities returns the non-terminal capability used by the transport test.
func (*runtimeContractService) GetCapabilities(
	_ context.Context,
	_ *uipb.GetCapabilitiesRequest,
) (*uipb.GetCapabilitiesResponse, error) {
	return uipb.GetCapabilitiesResponse_builder{ControlsTerminal: new(false)}.Build(), nil
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
	for _, response := range []*uipb.OpenResponse{
		uipb.OpenResponse_builder{Submit: uipb.SubmitCommand_builder{Text: new("request")}.Build()}.Build(),
		uipb.OpenResponse_builder{Stop: &uipb.StopCommand{}}.Build(),
		uipb.OpenResponse_builder{RetryAuthentication: &uipb.RetryAuthenticationCommand{}}.Build(),
		uipb.OpenResponse_builder{Quit: &uipb.QuitCommand{}}.Build(),
		uipb.OpenResponse_builder{SelectModel: uipb.SelectModelCommand_builder{
			ProviderId: new("openrouter"), ModelId: new("sonnet"),
		}.Build()}.Build(),
		uipb.OpenResponse_builder{SelectReasoningLevel: uipb.SelectReasoningLevelCommand_builder{
			Level: new(uipb.ReasoningLevel_REASONING_LEVEL_XHIGH),
		}.Build()}.Build(),
	} {
		if err := stream.Send(response); err != nil {
			return err
		}
	}
	return nil
}

// testInitializationFrame creates one complete initialization mapping fixture.
func testInitializationFrame() domainui.Frame {
	return domainui.Frame{
		Kind: domainui.FrameInitialization,
		Initialization: domainui.Initialization{
			SelectedUIID: "ui",
			StartupContent: []domainui.StartupContent{{
				Severity: domainui.ContentSeverityInformation, Text: "ready",
			}},
			Extensions: []domainui.ExtensionAvailability{{
				PluginID: "tools", Path: "/plugins/tools", Tools: []string{"read"},
			}},
			Availability: domainui.AvailabilityCheckingAuthentication,
		},
		Lifecycle: emptyTestLifecycle(), AuthorizationURL: "", Text: "", RetryAuthentication: false,
	}
}

// testLifecycleFrame creates one complete lifecycle mapping fixture.
func testLifecycleFrame() domainui.Frame {
	lifecycle := emptyTestLifecycle()
	lifecycle.Type = domainui.LifecycleToolExecutionUpdate
	lifecycle.RunID = "run"
	lifecycle.ModelContent.Position = 2
	lifecycle.Text = "progress"
	lifecycle.ToolCallID = "call"
	lifecycle.ToolName = "read"
	lifecycle.ProgressChannel = domainui.ProgressChannelStdout
	lifecycle.IsError = true
	lifecycle.Outcome = "failed"
	lifecycle.ErrorMessage = "safe"
	lifecycle.Availability = domainui.AvailabilityRunning
	return domainui.Frame{
		Kind: domainui.FrameLifecycle, Initialization: emptyTestInitialization(), Lifecycle: lifecycle,
		AuthorizationURL: "", Text: "", RetryAuthentication: false,
	}
}

// testSimpleFrame creates one authorization or information mapping fixture.
func testSimpleFrame(kind domainui.FrameKind, text string) domainui.Frame {
	frame := domainui.Frame{
		Kind: kind, Initialization: emptyTestInitialization(), Lifecycle: emptyTestLifecycle(),
		AuthorizationURL: "", Text: "", RetryAuthentication: false,
	}
	if kind == domainui.FrameAuthorization {
		frame.AuthorizationURL = text
	} else {
		frame.Text = text
	}
	return frame
}

// testErrorFrame creates one retryable error mapping fixture.
func testErrorFrame() domainui.Frame {
	return domainui.Frame{
		Kind: domainui.FrameError, Initialization: emptyTestInitialization(), Lifecycle: emptyTestLifecycle(),
		AuthorizationURL: "", Text: "safe error", RetryAuthentication: true,
	}
}

// testModelSelectionFrame creates one Host-confirmed selection frame.
func testModelSelectionFrame() domainui.Frame {
	return domainui.Frame{
		Kind:           domainui.FrameModelSelectionChanged,
		Initialization: emptyTestInitialization(), Lifecycle: emptyTestLifecycle(),
		ModelSelection: domainui.ModelSelection{
			ProviderID: "openrouter", ModelID: "sonnet", ReasoningLevel: domainui.ReasoningLevelHigh,
		},
	}
}

// emptyTestInitialization returns explicit zero values for non-initialization fixtures.
func emptyTestInitialization() domainui.Initialization {
	return domainui.Initialization{
		SelectedUIID: "", StartupContent: nil, Extensions: nil, Availability: 0,
		Models: nil, ModelSelection: domainui.ModelSelection{},
	}
}

// emptyTestLifecycle returns explicit zero values for non-lifecycle fixtures.
func emptyTestLifecycle() domainui.Lifecycle {
	return domainui.Lifecycle{
		Type: 0, RunID: "", Text: "",
		ModelContent: domainui.ModelContent{Type: 0, Kind: 0, Position: 0, Text: ""},
		ModelResponse: domainui.ModelResponse{
			Text: "", Outcome: "", ErrorMessage: "", Provider: "", Model: "", ResponseModel: nil, ResponseID: "",
			Content: nil,
			Usage: domainui.ModelUsage{
				InputTokens: 0, OutputTokens: 0, CachedInputTokens: 0,
				CacheWriteTokens: 0, ReasoningTokens: 0, TotalTokens: 0,
			},
			Diagnostics: nil,
		},
		ToolCallPreview: domainui.ToolCallPreview{
			CallID: "", Name: "", Position: 0, Provisional: false, Fields: nil,
		},
		FinalToolCall: domainui.FinalToolCall{CallID: "", Name: "", Position: 0, Arguments: nil},
		ToolCallID:    "", ToolName: "", ProgressChannel: 0, IsError: false,
		Outcome: "", ErrorMessage: "", Availability: 0,
	}
}
