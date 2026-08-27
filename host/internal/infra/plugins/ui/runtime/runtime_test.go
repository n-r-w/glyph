package runtime

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
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

// closeContractService holds one real gRPC receive open until its stream context is canceled.
type closeContractService struct {
	uipb.UnimplementedUIServiceServer
	opened chan struct{}
}

// Open reports stream readiness and waits for client-side cancellation.
func (s *closeContractService) Open(stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse]) error {
	close(s.opened)
	<-stream.Context().Done()
	return stream.Context().Err()
}

// TestChannelCloseUnblocksPendingReceive verifies cancellation through the owned stream context.
func TestChannelCloseUnblocksPendingReceive(t *testing.T) {
	t.Parallel()

	service := &closeContractService{
		UnimplementedUIServiceServer: uipb.UnimplementedUIServiceServer{},
		opened:                       make(chan struct{}),
	}
	client := uisdk.TestClient(t, service)
	streamContext, cancel := context.WithCancel(t.Context())
	stream, err := client.Open(streamContext)
	require.NoError(t, err)
	transport := &channel{
		stream:    stream,
		cancel:    cancel,
		closeOnce: sync.Once{},
		mutex:     sync.Mutex{},
	}
	receiveStarted := make(chan struct{})
	receiveDone := make(chan error, 1)
	go func() {
		close(receiveStarted)
		_, receiveErr := transport.Receive()
		receiveDone <- receiveErr
	}()

	<-service.opened
	<-receiveStarted
	transport.Close()

	require.Equal(t, codes.Canceled, status.Code(<-receiveDone))
}

// TestChannelMapsEveryFrameAndCommand verifies the complete generated transport boundary.
func TestChannelMapsEveryFrameAndCommand(t *testing.T) {
	t.Parallel()

	// Arrange a runtime stream with every frame and command contract variant.
	service := &runtimeContractService{
		UnimplementedUIServiceServer: uipb.UnimplementedUIServiceServer{},
		received:                     make(chan *uipb.OpenRequest, 6),
	}
	client := uisdk.TestClient(t, service)
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	_, cancel := context.WithCancel(t.Context())
	transport := &channel{
		stream:    stream,
		cancel:    cancel,
		closeOnce: sync.Once{},
		mutex:     sync.Mutex{},
	}
	frames := []domainui.Frame{
		testInitializationFrame(),
		testLifecycleFrame(),
		testSimpleFrame(domainui.FrameAuthorization, "https://auth.example"),
		testSimpleFrame(domainui.FrameInformation, "information"),
		testErrorFrame(),
		testModelSelectionFrame(),
	}

	// Act by sending every host frame and receiving every UI command.
	for _, frame := range frames {
		require.NoError(t, transport.Send(frame))
	}
	// Assert every frame reaches the service and every command maps exactly.
	for range frames {
		assert.NotNil(t, <-service.received)
	}
	for _, expected := range []domainui.Command{
		{
			Kind:            domainui.CommandSubmit,
			Text:            mo.Some("request"),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		},
		{
			Kind:            domainui.CommandStop,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		},
		{
			Kind:            domainui.CommandRetryAuthentication,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		},
		{
			Kind:            domainui.CommandQuit,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		},
		{
			Kind:            domainui.CommandSelectModel,
			ProviderID:      mo.Some("openrouter"),
			ModelID:         mo.Some("sonnet"),
			Text:            mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		},
		{
			Kind:            domainui.CommandSelectReasoningChoice,
			ReasoningChoice: mo.Some(domainui.ReasoningChoiceXHigh),
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		},
	} {
		command, receiveErr := transport.Receive()
		require.NoError(t, receiveErr)
		assert.Equal(t, expected, command)
	}
}

// TestRestoredSessionImageDataPresence verifies restored image presence and ownership after UI serialization.
func TestRestoredSessionImageDataPresence(t *testing.T) {
	t.Parallel()

	// Arrange user and tool-result images for every observable data-presence state.
	tests := []struct {
		name        string
		data        mo.Option[[]byte]
		expectError bool
		expectData  []byte
	}{
		{name: "absent data", data: mo.None[[]byte](), expectError: true, expectData: nil},
		{name: "present nil data", data: mo.Some[[]byte](nil), expectError: false, expectData: []byte{}},
		{name: "present non-nil empty data", data: mo.Some([]byte{}), expectError: false, expectData: []byte{}},
		{name: "nonempty data", data: mo.Some([]byte{1, 2, 3}), expectError: false, expectData: []byte{1, 2, 3}},
	}

	for _, test := range tests {
		t.Run("user "+test.name, func(t *testing.T) {
			t.Parallel()
			inputData := test.data
			if data, present := test.data.Get(); present {
				inputData = mo.Some(bytes.Clone(data))
			}
			// Act by mapping and serializing a restored user image.
			mapped, err := mapRestoredSessionEntries([]domainui.SessionEntry{{
				ID: "user", CreatedAt: time.Unix(1, 0), Kind: domainui.SessionEntryUser,
				User: mo.Some(model.Message{Content: []model.InputContent{{
					Kind: model.InputContentImage, Text: mo.None[string](),
					MediaType: mo.Some("image/png"), Data: inputData,
				}}}),
				Model: mo.None[domainui.ModelResponse](), ToolResult: mo.None[agent.ToolResult](),
			}})

			// Assert validation, oneof selection, presence, bytes, and ownership.
			if test.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if source, present := inputData.Get(); present && len(source) != 0 {
				source[0] = 99
			}
			payload, err := proto.Marshal(mapped[0])
			require.NoError(t, err)
			roundTripped := new(uipb.SessionEntry)
			require.NoError(t, proto.Unmarshal(payload, roundTripped))
			require.Len(t, roundTripped.GetUser().GetContent(), 1)
			content := roundTripped.GetUser().GetContent()[0]
			assert.Equal(t, uipb.UserContent_Image_case, content.WhichContent())
			require.NotNil(t, content.GetImage())
			assert.True(t, content.GetImage().HasData())
			assert.Equal(t, test.expectData, content.GetImage().GetData())
		})

		t.Run("tool result "+test.name, func(t *testing.T) {
			t.Parallel()
			inputData := test.data
			if data, present := test.data.Get(); present {
				inputData = mo.Some(bytes.Clone(data))
			}
			image := mo.None[tool.ResultImage]()
			if data, present := inputData.Get(); present {
				image = mo.Some(tool.ResultImage{MediaType: "image/png", Data: data})
			}
			// Act by mapping and serializing a restored tool-result image.
			mapped, err := mapRestoredSessionEntries([]domainui.SessionEntry{{
				ID: "tool", CreatedAt: time.Unix(1, 0), Kind: domainui.SessionEntryToolResult,
				User: mo.None[model.Message](), Model: mo.None[domainui.ModelResponse](),
				ToolResult: mo.Some(agent.ToolResult{
					CallID: "call", ToolName: "render", IsError: false,
					Contents: []tool.ResultContent{{
						Kind: tool.ResultContentImage, Text: mo.None[string](), Image: image,
					}},
				}),
			}})

			// Assert absent images stay absent and present image bytes retain presence and ownership.
			require.NoError(t, err)
			if test.expectError {
				require.Empty(t, mapped[0].GetToolResult().GetContents())
				return
			}
			if source, present := inputData.Get(); present && len(source) != 0 {
				source[0] = 99
			}
			payload, err := proto.Marshal(mapped[0])
			require.NoError(t, err)
			roundTripped := new(uipb.SessionEntry)
			require.NoError(t, proto.Unmarshal(payload, roundTripped))
			require.Len(t, roundTripped.GetToolResult().GetContents(), 1)
			content := roundTripped.GetToolResult().GetContents()[0]
			assert.Equal(t, uipb.ToolResultContent_Image_case, content.WhichContent())
			require.NotNil(t, content.GetImage())
			assert.True(t, content.GetImage().HasData())
			assert.Equal(t, test.expectData, content.GetImage().GetData())
		})
	}
}

func TestMapSessionCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response *uipb.OpenResponse
		expected domainui.Command
	}{
		{name: "create", response: func() *uipb.OpenResponse {
			value := new(uipb.OpenResponse)
			value.SetCreateSession(new(uipb.CreateSessionCommand))
			return value
		}(), expected: emptySessionCommand(domainui.CommandCreateSession)},
		{name: "list", response: func() *uipb.OpenResponse {
			value := new(uipb.OpenResponse)
			value.SetListSessions(new(uipb.ListSessionsCommand))
			return value
		}(), expected: emptySessionCommand(domainui.CommandListSessions)},
		{name: "information", response: func() *uipb.OpenResponse {
			value := new(uipb.OpenResponse)
			value.SetGetSessionInfo(new(uipb.GetSessionInfoCommand))
			return value
		}(), expected: emptySessionCommand(domainui.CommandGetSessionInfo)},
		{name: "resume", response: func() *uipb.OpenResponse {
			value := new(uipb.OpenResponse)
			value.SetResumeSession(uipb.ResumeSessionCommand_builder{SessionId: new("stored")}.Build())
			return value
		}(), expected: func() domainui.Command {
			value := emptySessionCommand(domainui.CommandResumeSession)
			value.SessionID = mo.Some("stored")
			return value
		}()},
		{name: "name", response: func() *uipb.OpenResponse {
			value := new(uipb.OpenResponse)
			value.SetSetSessionName(uipb.SetSessionNameCommand_builder{Name: new("named")}.Build())
			return value
		}(), expected: func() domainui.Command {
			value := emptySessionCommand(domainui.CommandSetSessionName)
			value.SessionName = mo.Some("named")
			return value
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			actual, err := mapCommand(test.response)
			require.NoError(t, err)
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestMapCommandRequiresSelectedScalarPresence(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		responseIndex int
		clear         func(*uipb.OpenResponse)
	}{
		"submit text": {
			responseIndex: 0,
			clear:         func(response *uipb.OpenResponse) { response.GetSubmit().ClearText() },
		},
		"provider ID": {
			responseIndex: 4,
			clear:         func(response *uipb.OpenResponse) { response.GetSelectModel().ClearProviderId() },
		},
		"model ID": {
			responseIndex: 4,
			clear:         func(response *uipb.OpenResponse) { response.GetSelectModel().ClearModelId() },
		},
		"reasoning choice": {
			responseIndex: 5,
			clear: func(response *uipb.OpenResponse) {
				response.GetSelectReasoningChoice().ClearChoice()
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			response := runtimeCommandResponses("request", "openrouter", "sonnet")[test.responseIndex]
			test.clear(response)
			_, err := mapCommand(response)
			require.Error(t, err)
		})
	}
}

// TestMapCommandPreservesPresentEmptySubmit verifies an explicit empty string remains active.
func TestMapCommandPreservesPresentEmptySubmit(t *testing.T) {
	t.Parallel()

	response := runtimeCommandResponses("", "openrouter", "sonnet")[0]
	command, err := mapCommand(response)
	require.NoError(t, err)
	assert.Equal(t, mo.Some(""), command.Text)
}

// TestMapCommandRejectsEmptySelectedModel verifies Protobuf validation stays at the runtime boundary.
func TestMapCommandRejectsEmptySelectedModel(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		providerID string
		modelID    string
	}{
		{name: "provider", providerID: "", modelID: "sonnet"},
		{name: "model", providerID: "openrouter", modelID: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			responses := runtimeCommandResponses("request", test.providerID, test.modelID)
			_, err := mapCommand(responses[4])
			require.EqualError(t, err, "receive UI command: provider and model are required")
		})
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
		SessionInfo: session.Info{},
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
					Text: "hidden", ToolCall: mo.None[domainui.FinalToolCall](),
				},
				{
					Kind: domainui.ModelContentKindText,
					Text: "visible", ToolCall: mo.None[domainui.FinalToolCall](),
				},
				{
					Kind: domainui.ModelContentKindRefusal,
					Text: "cannot help", ToolCall: mo.None[domainui.FinalToolCall](),
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
			SessionEntries:      nil,
			Kind:                kind,
			Initialization:      mo.None[domainui.Initialization](),
			Lifecycle:           mo.None[domainui.Lifecycle](),
			AuthorizationURL:    mo.None[string](),
			Text:                mo.None[string](),
			RetryAuthentication: mo.None[bool](),
			ModelSelection:      mo.None[domainui.ModelSelection](),
			SessionInfo:         mo.None[session.Info](),
			Sessions:            nil,
			SessionStatistics:   mo.None[session.Statistics](),
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
		//nolint:exhaustruct // uipb.OpenResponse_builder sets only the active Submit field.
		uipb.OpenResponse_builder{
			Submit: uipb.SubmitCommand_builder{
				Text: new(text),
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
				ProviderId: new(providerID),
				ModelId:    new(modelID),
			}.Build(),
		}.Build(),
		//nolint:exhaustruct // uipb.OpenResponse_builder sets only the active SelectReasoningChoice field.
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
	}
}

func testUIReasoningCapabilities(choices ...domainui.ReasoningChoice) domainui.ReasoningCapabilities {
	return domainui.ReasoningCapabilities{
		Supported: true,
		Choices:   choices,
		Default:   choices[len(choices)-1],
	}
}
