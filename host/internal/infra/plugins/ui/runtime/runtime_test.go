package runtime

import (
	"context"
	"sync"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

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
	transport := &channel{
		stream: stream,
		mutex:  sync.Mutex{},
	}
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
		{
			Kind:            domainui.CommandSubmit,
			Text:            "request",
			ProviderID:      "",
			ModelID:         "",
			ReasoningChoice: 0,
		},
		{
			Kind:            domainui.CommandStop,
			Text:            "",
			ProviderID:      "",
			ModelID:         "",
			ReasoningChoice: 0,
		},
		{
			Kind:            domainui.CommandRetryAuthentication,
			Text:            "",
			ProviderID:      "",
			ModelID:         "",
			ReasoningChoice: 0,
		},
		{
			Kind:            domainui.CommandQuit,
			Text:            "",
			ProviderID:      "",
			ModelID:         "",
			ReasoningChoice: 0,
		},
		{
			Kind:            domainui.CommandSelectModel,
			ProviderID:      "openrouter",
			ModelID:         "sonnet",
			Text:            "",
			ReasoningChoice: 0,
		},
		{
			Kind:            domainui.CommandSelectReasoningChoice,
			ReasoningChoice: domainui.ReasoningChoiceXHigh,
			Text:            "",
			ProviderID:      "",
			ModelID:         "",
		},
	} {
		command, receiveErr := transport.Receive()
		require.NoError(t, receiveErr)
		assert.Equal(t, expected, command)
	}
}

// TestMapInitializationPreservesWarningAndExtensionPath verifies public UI diagnostics mapping.
func TestMapInitializationPreservesWarningAndExtensionPath(t *testing.T) {
	t.Parallel()

	mapped, err := mapInitialization(domainui.Initialization{
		SelectedUIID: "ui",
		StartupContent: []domainui.StartupContent{{
			Severity: domainui.ContentSeverityWarning,
			Text:     "excluded optional UI",
		}},
		Extensions: []domainui.ExtensionAvailability{{
			PluginID: "tools",
			Path:     "/plugins/tools",
			Tools:    []string{"read"},
		}},
		Availability: domainui.AvailabilityCheckingAuthentication,
		Models: []domainui.ConfiguredModel{{
			ProviderID: "openrouter",
			ModelID:    "sonnet",
			Reasoning:  testUIReasoningCapabilities(domainui.ReasoningChoiceOff, domainui.ReasoningChoiceXHigh),
		}},
		ModelSelection: mo.Some(domainui.ModelSelection{
			ProviderID:      "openrouter",
			ModelID:         "sonnet",
			ReasoningChoice: domainui.ReasoningChoiceXHigh,
		}),
	})

	require.NoError(t, err)
	require.Len(t, mapped.GetStartupContent(), 1)
	assert.Equal(t, uipb.ContentSeverity_CONTENT_SEVERITY_WARNING, mapped.GetStartupContent()[0].GetSeverity())
	require.Len(t, mapped.GetExtensions(), 1)
	assert.Equal(t, "/plugins/tools", mapped.GetExtensions()[0].GetPath())
	require.Len(t, mapped.GetModels(), 1)
	assert.Equal(t, "openrouter", mapped.GetModels()[0].GetProviderId())
	assert.Equal(t, []uipb.ReasoningChoice{
		uipb.ReasoningChoice_REASONING_CHOICE_OFF,
		uipb.ReasoningChoice_REASONING_CHOICE_XHIGH,
	}, mapped.GetModels()[0].GetReasoning().GetChoices())
	assert.Equal(t, uipb.ReasoningChoice_REASONING_CHOICE_XHIGH, mapped.GetModelSelection().GetReasoningChoice())
}

// TestReasoningMappingsCoverEveryValue verifies the closed UI reasoning contract.
func TestReasoningMappingsCoverEveryValue(t *testing.T) {
	t.Parallel()

	values := []struct {
		domain domainui.ReasoningChoice
		proto  uipb.ReasoningChoice
	}{
		{domainui.ReasoningChoiceOff, uipb.ReasoningChoice_REASONING_CHOICE_OFF},
		{domainui.ReasoningChoiceMinimal, uipb.ReasoningChoice_REASONING_CHOICE_MINIMAL},
		{domainui.ReasoningChoiceLow, uipb.ReasoningChoice_REASONING_CHOICE_LOW},
		{domainui.ReasoningChoiceMedium, uipb.ReasoningChoice_REASONING_CHOICE_MEDIUM},
		{domainui.ReasoningChoiceHigh, uipb.ReasoningChoice_REASONING_CHOICE_HIGH},
		{domainui.ReasoningChoiceXHigh, uipb.ReasoningChoice_REASONING_CHOICE_XHIGH},
		{domainui.ReasoningChoiceMax, uipb.ReasoningChoice_REASONING_CHOICE_MAX},
	}
	for _, value := range values {
		assert.Equal(t, value.proto, mapReasoningChoice(value.domain))
		mapped, err := mapReasoningChoiceFromProto(value.proto)
		require.NoError(t, err)
		assert.Equal(t, value.domain, mapped)
	}
	_, err := mapReasoningChoiceFromProto(uipb.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED)
	require.Error(t, err)
	_, err = mapReasoningChoiceFromProto(uipb.ReasoningChoice(99))
	require.Error(t, err)
}

// TestMapLifecycleCarriesTypedTerminalData verifies the generated terminal contract mapping.
func TestMapLifecycleCarriesTypedTerminalData(t *testing.T) {
	t.Parallel()

	event := domainui.Lifecycle{
		Type:               domainui.LifecycleMessageEnd,
		RunID:              mo.Some("run"),
		Text:               mo.None[string](),
		ToolResultContents: mo.None[[]tool.ResultContent](),
		ModelContent:       mo.None[domainui.ModelContent](),
		ModelResponse: mo.Some(domainui.ModelResponse{
			Text:          "visible",
			Outcome:       mo.Some("stop"),
			ErrorMessage:  mo.Some(""),
			Provider:      mo.Some("openai-codex"),
			Model:         mo.Some("gpt-test"),
			ResponseModel: mo.Some("gpt-actual"),
			ResponseID:    mo.Some("resp-1"),
			Content: []domainui.ModelResponseContent{
				{
					Kind: domainui.ModelContentKindReasoning,
					Text: "hidden",
				},
				{
					Kind: domainui.ModelContentKindText,
					Text: "visible",
				},
				{
					Kind: domainui.ModelContentKindRefusal,
					Text: "cannot help",
				},
			},
			Usage: mo.Some(domainui.ModelUsage{
				InputTokens:       10,
				OutputTokens:      7,
				CachedInputTokens: 4,
				CacheWriteTokens:  1,
				ReasoningTokens:   3,
				TotalTokens:       17,
			}),
			Diagnostics: []domainui.ModelDiagnostic{{
				Code:    "recovered_output",
				Message: "safe",
			}},
		}),
		ToolCallPreview: mo.None[domainui.ToolCallPreview](),
		FinalToolCall:   mo.None[domainui.FinalToolCall](),
		ToolCallID:      mo.None[string](),
		ToolName:        mo.None[string](),
		ProgressChannel: mo.None[domainui.ProgressChannel](),
		IsError:         mo.None[bool](),
		Outcome:         mo.None[string](),
		ErrorMessage:    mo.None[string](),
		Availability:    mo.None[domainui.Availability](),
	}

	mappedLifecycle, err := mapLifecycle(event)
	require.NoError(t, err)
	mapped := mappedLifecycle.GetModelResponse()

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

	contents := []tool.ResultContent{
		{
			Kind:  tool.ResultContentText,
			Text:  mo.Some("first"),
			Image: mo.None[tool.ResultImage](),
		},
		{
			Kind: tool.ResultContentImage,
			Text: mo.None[string](),
			Image: mo.Some(tool.ResultImage{
				MediaType: "image/png",
				Data:      []byte{1, 2, 3},
			}),
		},
	}
	event := domainui.Lifecycle{
		Type:               domainui.LifecycleToolResult,
		RunID:              mo.Some("run"),
		Text:               mo.None[string](),
		ToolResultContents: mo.Some(contents),
		ModelContent:       mo.None[domainui.ModelContent](),
		ModelResponse:      mo.None[domainui.ModelResponse](),
		ToolCallPreview:    mo.None[domainui.ToolCallPreview](),
		FinalToolCall:      mo.None[domainui.FinalToolCall](),
		ToolCallID:         mo.Some("call"),
		ToolName:           mo.Some("read"),
		ProgressChannel:    mo.None[domainui.ProgressChannel](),
		IsError:            mo.Some(false),
		Outcome:            mo.None[string](),
		ErrorMessage:       mo.None[string](),
		Availability:       mo.None[domainui.Availability](),
	}
	mappedLifecycle, err := mapLifecycle(event)
	require.NoError(t, err)
	mapped := mappedLifecycle.GetToolResultContents()
	image, present := contents[1].Image.Get()
	require.True(t, present)
	image.Data[0] = 9

	require.Len(t, mapped, 2)
	assert.Equal(t, "first", mapped[0].GetText())
	assert.Equal(t, "image/png", mapped[1].GetImage().GetMediaType())
	assert.Equal(t, []byte{1, 2, 3}, mapped[1].GetImage().GetData())
}

// TestMappingRejectsMissingPayloads verifies malformed stream items fail explicitly.
func TestMappingRejectsMissingPayloads(t *testing.T) {
	t.Parallel()

	for _, kind := range []domainui.FrameKind{
		domainui.FrameInitialization,
		domainui.FrameLifecycle,
		domainui.FrameAuthorization,
		domainui.FrameInformation,
		domainui.FrameError,
		domainui.FrameModelSelectionChanged,
	} {
		_, err := mapFrame(domainui.Frame{
			Kind:                kind,
			Initialization:      mo.None[domainui.Initialization](),
			Lifecycle:           mo.None[domainui.Lifecycle](),
			AuthorizationURL:    mo.None[string](),
			Text:                mo.None[string](),
			RetryAuthentication: mo.None[bool](),
			ModelSelection:      mo.None[domainui.ModelSelection](),
		})
		require.Error(t, err)
	}
	_, err := mapCommand(&uipb.OpenResponse{})
	require.Error(t, err)
}

// TestMapLifecycleRejectsMissingSelectedPayload verifies required lifecycle alternatives.
func TestMapLifecycleRejectsMissingSelectedPayload(t *testing.T) {
	t.Parallel()

	for _, lifecycleType := range []domainui.LifecycleType{
		domainui.LifecycleModelContentStart,
		domainui.LifecycleModelTextDelta,
		domainui.LifecycleModelContentEnd,
		domainui.LifecycleMessageEnd,
		domainui.LifecycleToolCallStart,
		domainui.LifecycleToolCallDelta,
		domainui.LifecycleToolCallEnd,
		domainui.LifecycleToolExecutionStart,
		domainui.LifecycleToolExecutionUpdate,
		domainui.LifecycleToolExecutionEnd,
		domainui.LifecycleToolResult,
		domainui.LifecycleTurnEnd,
		domainui.LifecycleAgentEnd,
		domainui.LifecycleAvailabilityChanged,
	} {
		event := domainui.Lifecycle{
			Type:               lifecycleType,
			RunID:              mo.Some("run"),
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
			Availability:       mo.None[domainui.Availability](),
		}
		_, err := mapLifecycle(event)
		require.Error(t, err)
	}
	_, err := mapLifecycle(domainui.Lifecycle{
		Type:  domainui.LifecycleModelTextDelta,
		RunID: mo.Some("run"),
		ModelContent: mo.Some(domainui.ModelContent{
			Type: domainui.ModelContentTextDelta, Kind: domainui.ModelContentKindText,
			Position: 0, Text: mo.None[string](),
		}),
		Text:               mo.None[string](),
		ToolResultContents: mo.None[[]tool.ResultContent](),
		ModelResponse:      mo.None[domainui.ModelResponse](),
		ToolCallPreview:    mo.None[domainui.ToolCallPreview](),
		FinalToolCall:      mo.None[domainui.FinalToolCall](),
		ToolCallID:         mo.None[string](),
		ToolName:           mo.None[string](),
		ProgressChannel:    mo.None[domainui.ProgressChannel](),
		IsError:            mo.None[bool](),
		Outcome:            mo.None[string](),
		ErrorMessage:       mo.None[string](),
		Availability:       mo.None[domainui.Availability](),
	})
	require.Error(t, err)
}

// TestMapToolCallPreviewPreservesPresentZeroValues verifies oneof presence at the Protobuf boundary.
func TestMapToolCallPreviewPreservesPresentZeroValues(t *testing.T) {
	t.Parallel()

	mapped, err := mapToolCallPreview(domainui.ToolCallPreview{
		CallID:      "call",
		Name:        "tool",
		Position:    0,
		Provisional: false,
		Fields: []domainui.ToolCallPreviewField{
			{Name: "value", Value: mo.Some[any](nil), Prefix: mo.None[string](), Complete: true},
			{Name: "prefix", Value: mo.None[any](), Prefix: mo.Some(""), Complete: false},
		},
	})

	require.NoError(t, err)
	require.Len(t, mapped.GetFields(), 2)
	assert.True(t, mapped.GetFields()[0].HasValue())
	assert.Equal(t, structpb.NullValue_NULL_VALUE, mapped.GetFields()[0].GetValue().GetNullValue())
	assert.True(t, mapped.GetFields()[1].HasPrefix())
	assert.Empty(t, mapped.GetFields()[1].GetPrefix())
}

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
	for _, response := range []*uipb.OpenResponse{
		//nolint:exhaustruct // uipb.OpenResponse_builder sets only the active Submit field.
		uipb.OpenResponse_builder{
			Submit: uipb.SubmitCommand_builder{
				Text: new("request"),
			}.Build(),
		}.Build(),
		//nolint:exhaustruct // uipb.OpenResponse_builder sets only the active Stop field.
		uipb.OpenResponse_builder{
			Stop: &uipb.StopCommand{},
		}.Build(),
		//nolint:exhaustruct // uipb.OpenResponse_builder sets only the active RetryAuthentication field.
		uipb.OpenResponse_builder{
			RetryAuthentication: &uipb.RetryAuthenticationCommand{},
		}.Build(),
		//nolint:exhaustruct // uipb.OpenResponse_builder sets only the active Quit field.
		uipb.OpenResponse_builder{
			Quit: &uipb.QuitCommand{},
		}.Build(),
		//nolint:exhaustruct // uipb.OpenResponse_builder sets only the active SelectModel field.
		uipb.OpenResponse_builder{
			SelectModel: uipb.SelectModelCommand_builder{
				ProviderId: new("openrouter"),
				ModelId:    new("sonnet"),
			}.Build(),
		}.Build(),
		//nolint:exhaustruct // uipb.OpenResponse_builder sets only the active SelectReasoningChoice field.
		uipb.OpenResponse_builder{
			SelectReasoningChoice: uipb.SelectReasoningChoiceCommand_builder{
				Choice: new(uipb.ReasoningChoice_REASONING_CHOICE_XHIGH),
			}.Build(),
		}.Build(),
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
		}),
		Lifecycle:           mo.None[domainui.Lifecycle](),
		AuthorizationURL:    mo.None[string](),
		Text:                mo.None[string](),
		RetryAuthentication: mo.None[bool](),
		ModelSelection:      mo.None[domainui.ModelSelection](),
	}
}

// testLifecycleFrame creates one complete lifecycle mapping fixture.
func testLifecycleFrame() domainui.Frame {
	return domainui.Frame{
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
	}
}

// testSimpleFrame creates one authorization or information mapping fixture.
func testSimpleFrame(kind domainui.FrameKind, text string) domainui.Frame {
	if kind == domainui.FrameAuthorization {
		return domainui.Frame{
			Kind:                kind,
			Initialization:      mo.None[domainui.Initialization](),
			Lifecycle:           mo.None[domainui.Lifecycle](),
			AuthorizationURL:    mo.Some(text),
			Text:                mo.None[string](),
			RetryAuthentication: mo.None[bool](),
			ModelSelection:      mo.None[domainui.ModelSelection](),
		}
	}
	return domainui.Frame{
		Kind:                kind,
		Initialization:      mo.None[domainui.Initialization](),
		Lifecycle:           mo.None[domainui.Lifecycle](),
		AuthorizationURL:    mo.None[string](),
		Text:                mo.Some(text),
		RetryAuthentication: mo.None[bool](),
		ModelSelection:      mo.None[domainui.ModelSelection](),
	}
}

// testErrorFrame creates one retryable error mapping fixture.
func testErrorFrame() domainui.Frame {
	return domainui.Frame{
		Kind:                domainui.FrameError,
		Initialization:      mo.None[domainui.Initialization](),
		Lifecycle:           mo.None[domainui.Lifecycle](),
		AuthorizationURL:    mo.None[string](),
		Text:                mo.Some("safe error"),
		RetryAuthentication: mo.Some(true),
		ModelSelection:      mo.None[domainui.ModelSelection](),
	}
}

// testModelSelectionFrame creates one Host-confirmed selection frame.
func testModelSelectionFrame() domainui.Frame {
	return domainui.Frame{
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
	}
}

func testUIReasoningCapabilities(choices ...domainui.ReasoningChoice) domainui.ReasoningCapabilities {
	return domainui.ReasoningCapabilities{
		Supported: true,
		Choices:   choices,
		Default:   choices[len(choices)-1],
	}
}
