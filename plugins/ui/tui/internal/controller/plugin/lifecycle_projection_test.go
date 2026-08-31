//go:build !integration

package plugin

import (
	"testing"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/proto"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestMapLifecycleProjectsModelToolSettlementAndAvailability verifies every approved lifecycle mapping.
func TestMapLifecycleProjectsModelToolSettlementAndAvailability(t *testing.T) {
	t.Parallel()

	// Arrange valid model, tool, settlement, and availability lifecycle cases.
	testCases := []struct {
		name      string
		lifecycle *uiv1.LifecycleEvent
		expected  presentationdomain.Event
	}{
		{
			name: "model delta",
			lifecycle: uiv1.LifecycleEvent_builder{
				Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA),
				ModelContent: uiv1.ModelContent_builder{
					Type:     new(uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA),
					Position: new(int32(2)),
					Text:     new("delta"),
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
			expected: presentationdomain.Event{
				RestoredTranscript:   nil,
				Kind:                 presentationdomain.EventModelDelta,
				Position:             mo.Some(2),
				Text:                 mo.Some("delta"),
				Startup:              nil,
				Extensions:           nil,
				Availability:         mo.None[presentationdomain.Availability](),
				ModelContentKind:     mo.Some(presentationdomain.ModelContentText),
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
				TreeEvent:            mo.None[presentationdomain.TreeEvent](),
			},
		},
		{
			name: "tool start",
			lifecycle: uiv1.LifecycleEvent_builder{
				Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START),
				ToolCallId:         new("call-1"),
				ToolName:           new("read"),
				RunId:              new("run"),
				Text:               nil,
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
			expected: presentationdomain.Event{
				RestoredTranscript:   nil,
				Kind:                 presentationdomain.EventToolStarted,
				ToolCallID:           mo.Some("call-1"),
				ToolName:             mo.Some("read"),
				Status:               mo.Some("started"),
				Startup:              nil,
				Extensions:           nil,
				Availability:         mo.None[presentationdomain.Availability](),
				Position:             mo.None[int](),
				ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
				ModelResponseContent: nil,
				Stream:               mo.None[presentationdomain.OutputStream](),
				Text:                 mo.None[string](),
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
				TreeEvent:            mo.None[presentationdomain.TreeEvent](),
			},
		},
		{
			name: "tool stderr",
			lifecycle: uiv1.LifecycleEvent_builder{
				Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE),
				ToolCallId:         new("call-1"),
				ProgressChannel:    new(uiv1.ProgressChannel_PROGRESS_CHANNEL_STDERR),
				Text:               new("warning"),
				RunId:              new("run"),
				ToolName:           nil,
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
			expected: presentationdomain.Event{
				RestoredTranscript:   nil,
				Kind:                 presentationdomain.EventToolOutput,
				ToolCallID:           mo.Some("call-1"),
				Stream:               mo.Some(presentationdomain.OutputStderr),
				Text:                 mo.Some("warning"),
				Startup:              nil,
				Extensions:           nil,
				Availability:         mo.None[presentationdomain.Availability](),
				Position:             mo.None[int](),
				ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
				ModelResponseContent: nil,
				ToolName:             mo.None[string](),
				Status:               mo.None[string](),
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
				TreeEvent:            mo.None[presentationdomain.TreeEvent](),
			},
		},
		{
			name: "failed tool result",
			lifecycle: uiv1.LifecycleEvent_builder{
				Type:       new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT),
				ToolCallId: new("call-1"),
				ToolName:   new("read"),
				IsError:    new(true),
				ToolResultContents: []*uiv1.ToolResultContent{
					//nolint:exhaustruct_v5 // uiv1.ToolResultContent_builder sets only the active Text field.
					uiv1.ToolResultContent_builder{
						Text: new("denied"),
					}.Build(),
				},
				RunId:           new("run"),
				Text:            nil,
				ProgressChannel: nil,
				Outcome:         nil,
				ErrorMessage:    nil,
				Availability:    nil,
				ModelContent:    nil,
				ModelResponse:   nil,
				ToolCallPreview: nil,
				FinalToolCall:   nil,
			}.Build(),
			expected: presentationdomain.Event{
				RestoredTranscript: nil,
				Kind:               presentationdomain.EventToolResult,
				ToolCallID:         mo.Some("call-1"),
				ToolName:           mo.Some("read"),
				Failure:            mo.Some(true),
				Contents: mo.Some([]presentationdomain.Content{{
					Text:      mo.Some("denied"),
					MediaType: mo.None[string](),
					Data:      mo.None[[]byte](),
				}}),
				Startup:              nil,
				Extensions:           nil,
				Availability:         mo.None[presentationdomain.Availability](),
				Position:             mo.None[int](),
				ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
				ModelResponseContent: nil,
				Status:               mo.None[string](),
				Stream:               mo.None[presentationdomain.OutputStream](),
				Text:                 mo.None[string](),
				ErrorText:            mo.None[string](),
				ExitCode:             mo.None[int](),
				ToolCall:             mo.None[presentationdomain.ToolCallState](),
				Models:               nil,
				ModelSelection:       mo.None[presentationdomain.ModelSelection](),
				SessionInfo:          mo.None[presentationdomain.SessionInfo](),
				Sessions:             nil,
				SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
				TreeEvent:            mo.None[presentationdomain.TreeEvent](),
			},
		},
		{
			name: "failed settlement",
			lifecycle: uiv1.LifecycleEvent_builder{
				Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED),
				Outcome:            new("error"),
				ErrorMessage:       new("safe failure"),
				RunId:              new("run"),
				Text:               nil,
				ToolCallId:         nil,
				ToolName:           nil,
				ProgressChannel:    nil,
				IsError:            nil,
				Availability:       nil,
				ModelContent:       nil,
				ModelResponse:      nil,
				ToolCallPreview:    nil,
				FinalToolCall:      nil,
				ToolResultContents: nil,
			}.Build(),
			expected: presentationdomain.Event{
				RestoredTranscript:   nil,
				Kind:                 presentationdomain.EventAgentSettled,
				Text:                 mo.Some("safe failure"),
				ErrorText:            mo.Some("safe failure"),
				Status:               mo.Some("error"),
				Failure:              mo.Some(true),
				Startup:              nil,
				Extensions:           nil,
				Availability:         mo.None[presentationdomain.Availability](),
				Position:             mo.None[int](),
				ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
				ModelResponseContent: nil,
				ToolCallID:           mo.None[string](),
				ToolName:             mo.None[string](),
				Stream:               mo.None[presentationdomain.OutputStream](),
				Contents:             mo.None[[]presentationdomain.Content](),
				ExitCode:             mo.None[int](),
				ToolCall:             mo.None[presentationdomain.ToolCallState](),
				Models:               nil,
				ModelSelection:       mo.None[presentationdomain.ModelSelection](),
				SessionInfo:          mo.None[presentationdomain.SessionInfo](),
				Sessions:             nil,
				SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
				TreeEvent:            mo.None[presentationdomain.TreeEvent](),
			},
		},
		{
			name: "availability",
			lifecycle: uiv1.LifecycleEvent_builder{
				Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED),
				Availability:       new(uiv1.Availability_AVAILABILITY_RUNNING),
				RunId:              new("run"),
				Text:               nil,
				ToolCallId:         nil,
				ToolName:           nil,
				ProgressChannel:    nil,
				IsError:            nil,
				Outcome:            nil,
				ErrorMessage:       nil,
				ModelContent:       nil,
				ModelResponse:      nil,
				ToolCallPreview:    nil,
				FinalToolCall:      nil,
				ToolResultContents: nil,
			}.Build(),
			expected: presentationdomain.Event{
				RestoredTranscript:   nil,
				Kind:                 presentationdomain.EventAvailability,
				Availability:         mo.Some(presentationdomain.AvailabilityRunning),
				Startup:              nil,
				Extensions:           nil,
				Position:             mo.None[int](),
				ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
				ModelResponseContent: nil,
				ToolCallID:           mo.None[string](),
				ToolName:             mo.None[string](),
				Status:               mo.None[string](),
				Stream:               mo.None[presentationdomain.OutputStream](),
				Text:                 mo.None[string](),
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
				TreeEvent:            mo.None[presentationdomain.TreeEvent](),
			},
		},
	}

	// Act by mapping each lifecycle case independently.
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			event, err := mapLifecycle(testCase.lifecycle)
			// Assert every lifecycle maps to its exact presentation event.
			require.NoError(t, err)
			assert.Equal(t, testCase.expected, event)
		})
	}
}

// TestMapToolCallPreviewPreservesCompleteSnapshot verifies direct protobuf projection without truncation.
func TestMapToolCallPreviewPreservesCompleteSnapshot(t *testing.T) {
	t.Parallel()

	completeValue, err := structpb.NewValue(map[string]any{
		"nested": []any{"value", float64(2), true},
	})
	require.NoError(t, err)
	nullValue, err := structpb.NewValue(nil)
	require.NoError(t, err)
	preview := uiv1.ToolCallPreview_builder{
		CallId:      new("call-17"),
		Name:        new("sample"),
		Position:    new(int32(23)),
		Provisional: new(true),
		Fields: []*uiv1.ToolCallPreviewField{
			uiv1.ToolCallPreviewField_builder{
				Name:   new("complete"),
				Value:  proto.ValueOrDefault(completeValue),
				Prefix: nil,
			}.Build(),
			uiv1.ToolCallPreviewField_builder{
				Name:   new("null"),
				Value:  proto.ValueOrDefault(nullValue),
				Prefix: nil,
			}.Build(),
			uiv1.ToolCallPreviewField_builder{
				Name:   new("prefix"),
				Prefix: new(`{"partial":`),
				Value:  nil,
			}.Build(),
		},
	}.Build()

	mapped, err := mapToolCallPreview(preview)
	require.NoError(t, err)
	assert.Equal(t, presentationdomain.ToolCallState{
		CallID:      "call-17",
		Name:        "sample",
		Position:    23,
		Provisional: true,
		Fields: []presentationdomain.ToolCallField{
			{
				Name: "complete",
				Value: mo.Some[any](map[string]any{
					"nested": []any{"value", float64(2), true},
				}),
				Prefix: mo.None[string](),
			},
			{
				Name:   "null",
				Value:  mo.Some[any](nil),
				Prefix: mo.None[string](),
			},
			{
				Name:   "prefix",
				Value:  mo.None[any](),
				Prefix: mo.Some(`{"partial":`),
			},
		},
		Arguments: nil,
	}, mapped)
}
