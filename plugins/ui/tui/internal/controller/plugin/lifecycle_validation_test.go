package plugin

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/proto"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
	presentationusecase "github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// testTextEvent creates one complete presentation text event.
func testTextEvent(kind presentationdomain.EventKind, text string) presentationdomain.Event {
	return presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 kind,
		Text:                 mo.Some(text),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	}
}

// TestMapRequestRejectsUnknownLifecycleAndMapsSafeError verifies malformed frames and safe errors.
func TestMapRequestRejectsUnknownLifecycleAndMapsSafeError(t *testing.T) {
	t.Parallel()

	// Arrange unknown lifecycle and safe-error requests.
	//nolint:exhaustruct_v5 // uiv1.OpenRequest_builder sets only the active Lifecycle field.
	unknownLifecycle := uiv1.OpenRequest_builder{
		Lifecycle:          &uiv1.LifecycleEvent{},
		SessionList:        nil,
		SessionChanged:     nil,
		SessionInformation: nil, SessionTree: nil, SessionTreeNavigation: nil, SessionTreeFailed: nil,
	}.Build()
	//nolint:exhaustruct_v5 // uiv1.OpenRequest_builder sets only the active Error field.
	safeError := uiv1.OpenRequest_builder{
		Error: uiv1.Error_builder{
			Text:                new("safe error"),
			RetryAuthentication: new(false),
		}.Build(),
		SessionList:        nil,
		SessionChanged:     nil,
		SessionInformation: nil, SessionTree: nil, SessionTreeNavigation: nil, SessionTreeFailed: nil,
	}.Build()

	// Act by mapping both requests.
	_, unknownErr := mapRequest(unknownLifecycle)
	event, err := mapRequest(safeError)

	// Assert the unknown lifecycle fails while the safe error maps without private data.
	require.Error(t, unknownErr)
	require.NoError(t, err)
	assert.Equal(t, testTextEvent(presentationdomain.EventError, "safe error"), event)
}

// TestMapLifecycleRejectsEmptyToolResultContents verifies missing terminal output fails at the UI boundary.
func TestMapLifecycleRejectsEmptyToolResultContents(t *testing.T) {
	t.Parallel()

	_, err := mapLifecycle(uiv1.LifecycleEvent_builder{
		Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT),
		RunId:              new("run"),
		Text:               nil,
		ToolCallId:         new("call"),
		ToolName:           new("tool"),
		ProgressChannel:    nil,
		IsError:            new(false),
		Outcome:            nil,
		ErrorMessage:       nil,
		Availability:       nil,
		ModelContent:       nil,
		ModelResponse:      nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build())
	require.ErrorContains(t, err, "tool result contents are empty")
}

// TestMapLifecycleRejectsMissingToolResultContent verifies malformed blocks fail at the UI boundary.
func TestMapLifecycleRejectsMissingToolResultContent(t *testing.T) {
	t.Parallel()

	_, err := mapLifecycle(uiv1.LifecycleEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT),
		ToolResultContents: []*uiv1.ToolResultContent{
			uiv1.ToolResultContent_builder{}.Build(),
		},
		RunId:           new("run"),
		Text:            nil,
		ToolCallId:      new("call"),
		ToolName:        new("tool"),
		ProgressChannel: nil,
		IsError:         new(false),
		Outcome:         nil,
		ErrorMessage:    nil,
		Availability:    nil,
		ModelContent:    nil,
		ModelResponse:   nil,
		ToolCallPreview: nil,
		FinalToolCall:   nil,
	}.Build())
	require.ErrorContains(t, err, "tool result content 0 is missing")
}

// TestMapLifecycleRejectsEmptyToolResultImage prevents empty image payloads from reaching presentation.
func TestMapLifecycleRejectsEmptyToolResultImage(t *testing.T) {
	t.Parallel()

	_, err := mapLifecycle(uiv1.LifecycleEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT),
		ToolResultContents: []*uiv1.ToolResultContent{
			//nolint:exhaustruct_v5 // uiv1.ToolResultContent_builder sets only the active Image field.
			uiv1.ToolResultContent_builder{
				Image: uiv1.ToolResultImage_builder{
					MediaType: new("image/png"),
					Data:      nil,
				}.Build(),
			}.Build(),
		},
		RunId:           new("run"),
		Text:            nil,
		ToolCallId:      new("call"),
		ToolName:        new("tool"),
		ProgressChannel: nil,
		IsError:         new(false),
		Outcome:         nil,
		ErrorMessage:    nil,
		Availability:    nil,
		ModelContent:    nil,
		ModelResponse:   nil,
		ToolCallPreview: nil,
		FinalToolCall:   nil,
	}.Build())
	require.ErrorContains(t, err, "tool result image 0 is invalid")
}

// TestHostMessageEndFinalizesTextStreamAtDifferentPosition verifies complete terminal model projection.
func TestHostMessageEndFinalizesTextStreamAtDifferentPosition(t *testing.T) {
	t.Parallel()

	// Arrange model lifecycle frames whose terminal response uses another stream position.
	projection := presentationusecase.New()
	state := presentationdomain.State{}
	frames := []*uiv1.LifecycleEvent{
		uiv1.LifecycleEvent_builder{
			Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START),
			RunId:              new("run"),
			Text:               nil,
			ToolCallId:         nil,
			ToolName:           nil,
			ProgressChannel:    nil,
			IsError:            nil,
			Outcome:            nil,
			ErrorMessage:       nil,
			Availability:       nil,
			ModelContent:       nil,
			ModelResponse:      nil,
			ToolCallPreview:    nil,
			FinalToolCall:      nil,
			ToolResultContents: nil,
		}.Build(),
		uiv1.LifecycleEvent_builder{
			Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA),
			ModelContent: uiv1.ModelContent_builder{
				Type:     new(uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA),
				Position: new(int32(1)),
				Text:     new("complete answer"),
				Kind:     new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
			}.Build(),
			RunId:              new("run"),
			Text:               nil,
			ToolCallId:         nil,
			ToolName:           nil,
			ProgressChannel:    nil,
			IsError:            nil,
			Outcome:            nil,
			ErrorMessage:       nil,
			Availability:       nil,
			ModelResponse:      nil,
			ToolCallPreview:    nil,
			FinalToolCall:      nil,
			ToolResultContents: nil,
		}.Build(),
		uiv1.LifecycleEvent_builder{
			Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END),
			ModelResponse: uiv1.ModelResponse_builder{
				Text:       new("complete answer"),
				Provider:   new("openai-codex"),
				Model:      new("gpt-test"),
				ResponseId: new("resp-1"),
				Usage: uiv1.ModelUsage_builder{
					InputTokens:       new(int64(3)),
					OutputTokens:      new(int64(2)),
					TotalTokens:       new(int64(5)),
					CachedInputTokens: nil,
					CacheWriteTokens:  nil,
					ReasoningTokens:   nil,
				}.Build(),
				Diagnostics: []*uiv1.ModelDiagnostic{uiv1.ModelDiagnostic_builder{
					Code:    new("recovered_output"),
					Message: new("hidden diagnostic"),
				}.Build()},
				Content: []*uiv1.ModelResponseContent{
					uiv1.ModelResponseContent_builder{
						Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING),
						Text: new("hidden reasoning"), ToolCall: nil,
					}.Build(),
					uiv1.ModelResponseContent_builder{
						Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
						Text: new("complete answer"), ToolCall: nil,
					}.Build(),
				},
				Outcome:       nil,
				ErrorMessage:  nil,
				ResponseModel: nil,
			}.Build(),
			RunId:              new("run"),
			Text:               nil,
			ToolCallId:         nil,
			ToolName:           nil,
			ProgressChannel:    nil,
			IsError:            nil,
			Outcome:            nil,
			ErrorMessage:       nil,
			Availability:       nil,
			ModelContent:       nil,
			ToolCallPreview:    nil,
			FinalToolCall:      nil,
			ToolResultContents: nil,
		}.Build(),
	}
	// Act by mapping and applying the lifecycle frame sequence.
	for _, lifecycle := range frames {
		//nolint:exhaustruct_v5 // uiv1.OpenRequest_builder sets only the active Lifecycle field.
		event, err := mapRequest(uiv1.OpenRequest_builder{
			Lifecycle:          proto.ValueOrDefault(lifecycle),
			SessionList:        nil,
			SessionChanged:     nil,
			SessionInformation: nil, SessionTree: nil, SessionTreeNavigation: nil, SessionTreeFailed: nil,
		}.Build())
		require.NoError(t, err)
		state = projection.Apply(state, event)
	}

	// Assert the terminal response finalizes the complete ordered transcript without active fragments.
	assert.Equal(t, []presentationdomain.Line{
		{
			Kind:     presentationdomain.LineReasoning,
			Text:     mo.Some("hidden reasoning"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
		{
			Kind:     presentationdomain.LineModel,
			Text:     mo.Some("complete answer"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
	}, state.Transcript)
	assert.Empty(t, state.ActiveModel)
}
