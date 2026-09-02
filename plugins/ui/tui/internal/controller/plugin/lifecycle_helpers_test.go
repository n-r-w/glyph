//go:build !integration

package plugin

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/proto"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

func roundTripLifecycle(t *testing.T, lifecycle *uiv1.AgentEvent) *uiv1.AgentEvent {
	t.Helper()
	data, err := proto.Marshal(lifecycle)
	require.NoError(t, err)
	decoded := new(uiv1.AgentEvent)
	require.NoError(t, proto.Unmarshal(data, decoded))
	return decoded
}

// modelContentLifecycle builds a present model-content payload for discriminator boundary tests.
// modelContentLifecycle builds a valid nested model content lifecycle.
func modelContentLifecycle(
	outer uiv1.LifecycleType,
	nested uiv1.ModelContentType,
	kind uiv1.ModelContentKind,
) *uiv1.AgentEvent {
	var text *string
	if outer == uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA {
		text = new("")
	}
	return buildModelContentLifecycle(outer, nested, kind, text)
}

// modelContentLifecycleWithText builds a nested lifecycle with an explicit text field.
func modelContentLifecycleWithText(
	outer uiv1.LifecycleType,
	nested uiv1.ModelContentType,
	kind uiv1.ModelContentKind,
	text string,
) *uiv1.AgentEvent {
	return buildModelContentLifecycle(outer, nested, kind, new(text))
}

// buildModelContentLifecycle builds the shared generated lifecycle value.
func buildModelContentLifecycle(
	outer uiv1.LifecycleType,
	nested uiv1.ModelContentType,
	kind uiv1.ModelContentKind,
	text *string,
) *uiv1.AgentEvent {
	return uiv1.AgentEvent_builder{
		Type:            new(outer),
		RunId:           new("run"),
		Text:            nil,
		ToolCallId:      nil,
		ToolName:        nil,
		ProgressChannel: nil,
		IsError:         nil,
		Outcome:         nil,
		ErrorMessage:    nil,
		Availability:    nil,
		ModelContent: uiv1.ModelContent_builder{
			Type:     new(nested),
			Position: new(int64(0)),
			Kind:     new(kind),
			Text:     text,
		}.Build(),
		ModelResponse:      nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build()
}

func messageEndLifecycle(t *testing.T, content []*uiv1.ModelResponseContent) *uiv1.AgentEvent {
	t.Helper()
	return roundTripLifecycle(t, uiv1.AgentEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END), RunId: new("run"), Text: nil,
		ToolCallId: nil, ToolName: nil, ProgressChannel: nil, IsError: nil, Outcome: nil,
		ErrorMessage: nil, Availability: nil, ModelContent: nil,
		ModelResponse: uiv1.ModelResponse_builder{
			Text: nil, Outcome: nil, ErrorMessage: nil, Provider: nil, Model: nil,
			ResponseId: nil, Usage: nil, Diagnostics: nil, Content: content, ResponseModel: nil,
		}.Build(),
		ToolCallPreview: nil, FinalToolCall: nil, ToolResultContents: nil,
	}.Build())
}

// TestMapAuthenticationFailureConnectionEventsPreservesTextAndAvailability verifies retained authentication rendering.
func TestMapAuthenticationFailureConnectionEventsPreservesTextAndAvailability(t *testing.T) {
	t.Parallel()

	// Arrange the classified error and availability events emitted by Host.
	errorPayload := uiv1.Error_builder{Code: new("AUTHENTICATION_FAILED"), Text: new("Authentication failed.")}.Build()
	errorEvent := new(uiv1.HostConnectionEvent)
	errorEvent.SetError(errorPayload)
	availabilityEvent := new(uiv1.HostConnectionEvent)
	availabilityEvent.SetAvailabilityChanged(uiv1.AvailabilityChanged_builder{
		Availability: new(uiv1.Availability_AVAILABILITY_AUTHENTICATION_FAILED),
	}.Build())

	// Act through connection-event mapping.
	mappedError, err := mapConnectionEvent(errorEvent)
	require.NoError(t, err)
	mappedAvailability, err := mapConnectionEvent(availabilityEvent)
	require.NoError(t, err)

	// Assert complete error text and authentication-failed availability remain visible.
	assert.Equal(t, presentationdomain.EventError, mappedError.Kind)
	assert.Equal(t, mo.Some("Authentication failed."), mappedError.Text)
	assert.Equal(t, presentationdomain.EventAvailability, mappedAvailability.Kind)
	assert.Equal(t, mo.Some(presentationdomain.AvailabilityAuthenticationFailed), mappedAvailability.Availability)
}

// initializationRequest builds the first valid Host frame used by stream tests.
func initializationRequest() *uiv1.OpenRequest {
	initialization := uiv1.Initialization_builder{
		StartupContent: []*uiv1.StartupContent{uiv1.StartupContent_builder{
			Severity: new(uiv1.ContentSeverity_CONTENT_SEVERITY_INFORMATION), Text: new("ready"),
		}.Build()},
		Extensions: []*uiv1.ExtensionAvailability{uiv1.ExtensionAvailability_builder{
			PluginId: new("tools"), Tools: []string{"read"}, Path: new(""),
		}.Build()},
		Availability: new(uiv1.Availability_AVAILABILITY_IDLE),
		Models: []*uiv1.ConfiguredModel{uiv1.ConfiguredModel_builder{
			ProviderId: new("openai-codex"), ModelId: new("gpt"),
			Reasoning: testUIReasoning(uiv1.ReasoningChoice_REASONING_CHOICE_HIGH),
		}.Build()},
		ModelSelection: uiv1.ModelSelection_builder{
			ProviderId: new("openai-codex"), ModelId: new("gpt"),
			ReasoningChoice: new(uiv1.ReasoningChoice_REASONING_CHOICE_HIGH),
		}.Build(),
		SelectedUiId: new("glyph-tui"), SessionInfo: testSessionInfo(),
	}.Build()
	request := new(uiv1.HostRequest)
	request.SetInitialize(initialization)
	return uiv1.OpenRequest_builder{
		OperationId: new("initialize"), Request: request, Event: nil, ConnectionEvent: nil, Close: nil,
	}.Build()
}

func testSessionInfo() *uiv1.SessionInfo {
	createdAt := timestamppb.New(time.Unix(1, 0).UTC())
	return uiv1.SessionInfo_builder{
		Id:               new("session"),
		WorkingDirectory: new("/project"),
		CreatedTime:      createdAt,
		UpdateTime:       createdAt, Name: nil, StoragePath: nil,
	}.Build()
}

func testReasoning(choices ...presentationdomain.ReasoningChoice) presentationdomain.ReasoningCapabilities {
	return presentationdomain.ReasoningCapabilities{
		Supported: true,
		Choices:   choices,
		Default:   choices[len(choices)-1],
	}
}

func testUIReasoning(choices ...uiv1.ReasoningChoice) *uiv1.ReasoningCapabilities {
	return uiv1.ReasoningCapabilities_builder{
		Supported:     new(true),
		Choices:       choices,
		DefaultChoice: new(choices[len(choices)-1]),
	}.Build()
}
