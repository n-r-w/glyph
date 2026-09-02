//go:build !integration

package programmatic

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// invalidModelOutcomeEvent creates a message-end event with an unspecified outcome.
func invalidModelOutcomeEvent() AgentEvent {
	return AgentEvent{
		Type: AgentEventMessageEnd,
		ModelResponse: mo.Some(ModelResponse{
			Outcome:       mo.Some(ModelOutcomeUnspecified),
			Text:          "",
			ErrorMessage:  mo.None[string](),
			Provider:      mo.None[string](),
			Model:         mo.None[string](),
			ResponseModel: mo.None[string](),
			ResponseID:    mo.None[string](),
			Usage:         mo.None[ModelUsage](),
			Diagnostics:   nil,
			Content:       nil,
		}),
		OperationID:     "",
		RunID:           "",
		ModelContent:    mo.None[ModelContent](),
		ToolCallPreview: mo.None[ToolCallPreview](),
		FinalToolCall:   mo.None[FinalToolCall](),
		ToolExecution:   mo.None[ToolExecution](),
		ToolProgress:    mo.None[ToolProgress](),
		ToolResult:      mo.None[ToolResult](),
		Turn:            mo.None[TurnSummary](),
		Agent:           mo.None[AgentSummary](),
	}
}

// TestMapConfiguredModelsRejectsUnknownInputModality proves unknown domain modalities do not reach protobuf.
func TestMapConfiguredModelsRejectsUnknownInputModality(t *testing.T) {
	t.Parallel()

	// Arrange a descriptor with a modality outside the closed domain set.
	descriptor := model.Descriptor{
		Provider:      "provider",
		Model:         "model",
		Input:         []model.InputModality{"audio"},
		ContextWindow: 131072,
		MaxTokens:     16384,
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: false,
			Choices:   []model.ReasoningChoice{model.ReasoningChoiceOff},
			Default:   model.ReasoningChoiceOff,
		},
		ToolCapabilities: model.ToolCapabilities{},
		Pricing:          mo.None[model.Pricing](),
	}

	// Act by mapping the invalid descriptor.
	mapped, err := mapConfiguredModels([]model.Descriptor{descriptor})

	// Assert the mapper rejects the value without emitting an unspecified enum.
	require.Error(t, err)
	assert.Nil(t, mapped)
}

// TestMappingRejectsInvalidValues verifies closed unions and malformed JSON values.
func TestMappingRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	// Arrange malformed response and event variants for every mapping boundary.
	tests := map[string]func() error{
		"response": func() error {
			_, err := mapResponse(Response{
				SessionEntries:    nil,
				SessionStatistics: mo.None[session.Statistics](),
				Kind:              ResponseUnspecified,
				OperationID:       "",
				State:             mo.None[RunStateResult](),
				Messages:          nil,
				Models:            mo.None[ModelsResult](),
				Selection:         mo.None[model.Selection](),
				Rejection:         mo.None[Rejection](),
				SessionInfo:       mo.None[session.Info](),
				Sessions:          nil,
				SessionTree:       mo.None[SessionTree](),
				TreeNavigation:    mo.None[TreeNavigationResult](),
				Replacement:       mo.None[SessionReplacement](),
			})
			return err
		},
		"history": func() error {
			_, err := mapResponse(Response{
				SessionEntries:    nil,
				SessionStatistics: mo.None[session.Statistics](),
				Kind:              ResponseMessages,
				Messages: []HistoryEntry{{
					Kind: HistoryEntryUnspecified, User: mo.None[model.Message](),
					Model:      mo.None[ModelResponse](),
					ToolResult: mo.None[ToolResult](),
				}},
				OperationID:    "",
				State:          mo.None[RunStateResult](),
				Models:         mo.None[ModelsResult](),
				Selection:      mo.None[model.Selection](),
				Rejection:      mo.None[Rejection](),
				SessionInfo:    mo.None[session.Info](),
				Sessions:       nil,
				SessionTree:    mo.None[SessionTree](),
				TreeNavigation: mo.None[TreeNavigationResult](),
				Replacement:    mo.None[SessionReplacement](),
			})
			return err
		},
		"event": func() error {
			_, err := mapEvent(AgentEvent{
				Type:            AgentEventUnspecified,
				OperationID:     "",
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			})
			return err
		},
		"content": func() error {
			_, err := mapEvent(AgentEvent{
				Type: AgentEventModelContentStart,
				ModelContent: mo.Some(ModelContent{
					Kind:     ModelContentUnspecified,
					Position: 0,
					Text:     mo.None[string](),
				}),
				OperationID:     "",
				RunID:           "",
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			})
			return err
		},
		"preview kind": func() error {
			_, err := mapEvent(AgentEvent{
				Type: AgentEventToolCallStart,
				ToolCallPreview: mo.Some(ToolCallPreview{
					Fields: []ToolCallPreviewField{{
						Kind:   ToolCallPreviewFieldUnspecified,
						Name:   "",
						Value:  mo.None[any](),
						Prefix: mo.None[string](),
					}},
					CallID:      "",
					Name:        "",
					Position:    0,
					Provisional: false,
				}),
				OperationID:   "",
				RunID:         "",
				ModelContent:  mo.None[ModelContent](),
				FinalToolCall: mo.None[FinalToolCall](),
				ToolExecution: mo.None[ToolExecution](),
				ToolProgress:  mo.None[ToolProgress](),
				ToolResult:    mo.None[ToolResult](),
				ModelResponse: mo.None[ModelResponse](),
				Turn:          mo.None[TurnSummary](),
				Agent:         mo.None[AgentSummary](),
			})
			return err
		},
		"preview JSON": func() error {
			_, err := mapEvent(AgentEvent{
				Type: AgentEventToolCallStart,
				ToolCallPreview: mo.Some(ToolCallPreview{
					Fields: []ToolCallPreviewField{{
						Kind:   ToolCallPreviewFieldComplete,
						Value:  mo.Some[any](make(chan int)),
						Name:   "",
						Prefix: mo.None[string](),
					}},
					CallID:      "",
					Name:        "",
					Position:    0,
					Provisional: false,
				}),
				OperationID:   "",
				RunID:         "",
				ModelContent:  mo.None[ModelContent](),
				FinalToolCall: mo.None[FinalToolCall](),
				ToolExecution: mo.None[ToolExecution](),
				ToolProgress:  mo.None[ToolProgress](),
				ToolResult:    mo.None[ToolResult](),
				ModelResponse: mo.None[ModelResponse](),
				Turn:          mo.None[TurnSummary](),
				Agent:         mo.None[AgentSummary](),
			})
			return err
		},
		"progress": func() error {
			_, err := mapEvent(AgentEvent{
				Type: AgentEventToolExecutionUpdate,
				ToolProgress: mo.Some(ToolProgress{
					Channel: ProgressChannelUnspecified,
					Content: "",
				}),
				OperationID:     "",
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			})
			return err
		},
		"tool result": func() error {
			_, err := mapEvent(AgentEvent{
				Type: AgentEventToolResult,
				ToolResult: mo.Some(ToolResult{
					Contents: []ToolResultContent{{
						Kind:  ToolResultContentUnspecified,
						Text:  mo.None[string](),
						Image: mo.None[ToolResultImage](),
					}},
					CallID:   "",
					ToolName: "",
					IsError:  false,
				}),
				OperationID:     "",
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			})
			return err
		},
		"model outcome": func() error {
			_, err := mapEvent(invalidModelOutcomeEvent())
			return err
		},
		"model content": func() error {
			_, err := mapEvent(AgentEvent{
				Type: AgentEventMessageEnd,
				ModelResponse: mo.Some(ModelResponse{
					Outcome: mo.Some(ModelOutcomeStop),
					Content: []ModelResponseContent{{
						Kind:     ModelResponseContentUnspecified,
						Text:     mo.None[string](),
						ToolCall: mo.None[FinalToolCall](),
					}},
					Text:          "",
					ErrorMessage:  mo.None[string](),
					Provider:      mo.None[string](),
					Model:         mo.None[string](),
					ResponseModel: mo.None[string](),
					ResponseID:    mo.None[string](),
					Usage:         mo.None[ModelUsage](),
					Diagnostics:   nil,
				}),
				OperationID:     "",
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			})
			return err
		},
		"run outcome": func() error {
			_, err := mapEvent(AgentEvent{
				Type: AgentEventAgentEnd,
				Agent: mo.Some(AgentSummary{
					Outcome:      RunOutcomeUnspecified,
					ErrorMessage: mo.None[string](),
				}),
				OperationID:     "",
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
			})
			return err
		},
	}

	// Act by mapping each malformed value in an independent subtest.
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Assert the mapper rejects the malformed variant.
			assert.Error(t, test())
		})
	}
}

// TestMapEventRejectsMissingSelectedPayload verifies malformed selected events fail before serialization.
func TestMapEventRejectsMissingSelectedPayload(t *testing.T) {
	t.Parallel()

	_, err := mapEvent(AgentEvent{
		OperationID:     "operation",
		Type:            AgentEventToolExecutionStart,
		RunID:           "run",
		ModelContent:    mo.None[ModelContent](),
		ToolCallPreview: mo.None[ToolCallPreview](),
		FinalToolCall:   mo.None[FinalToolCall](),
		ToolExecution:   mo.None[ToolExecution](),
		ToolProgress:    mo.None[ToolProgress](),
		ToolResult:      mo.None[ToolResult](),
		ModelResponse:   mo.None[ModelResponse](),
		Turn:            mo.None[TurnSummary](),
		Agent:           mo.None[AgentSummary](),
	})

	assert.Error(t, err)
}

// TestMappingRejectsMissingNestedAlternatives verifies nested discriminators require their selected payloads.
func TestMappingRejectsMissingNestedAlternatives(t *testing.T) {
	t.Parallel()

	tests := map[string]func() error{
		"model text": func() error {
			_, err := mapModelContent(ModelContent{
				Kind: ModelContentText, Position: 0, Text: mo.None[string](),
			}, true)
			return err
		},
		"preview value": func() error {
			_, err := mapToolCallPreview(ToolCallPreview{
				CallID: "call", Name: "tool", Position: 0, Provisional: true,
				Fields: []ToolCallPreviewField{{
					Name: "field", Kind: ToolCallPreviewFieldComplete,
					Value: mo.None[any](), Prefix: mo.None[string](),
				}},
			})
			return err
		},
		"tool result text": func() error {
			_, err := mapToolResult(ToolResult{
				CallID: "call", ToolName: "tool", IsError: false,
				Contents: []ToolResultContent{{
					Kind: ToolResultContentText, Text: mo.None[string](), Image: mo.None[ToolResultImage](),
				}},
			})
			return err
		},
		"model response tool call": func() error {
			_, err := mapModelResponse(ModelResponse{
				Text: "", Outcome: mo.Some(ModelOutcomeToolUse), ErrorMessage: mo.None[string](),
				Provider: mo.None[string](), Model: mo.None[string](), ResponseModel: mo.None[string](),
				ResponseID: mo.None[string](), Usage: mo.None[ModelUsage](), Diagnostics: nil,
				Content: []ModelResponseContent{{
					Kind: ModelResponseContentToolCall, Text: mo.None[string](),
					ToolCall: mo.None[FinalToolCall](),
				}},
			})
			return err
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Error(t, test())
		})
	}
}

// TestMapModelContentPreservesPresentZeroValues verifies zero position and empty text remain present values.
func TestMapModelContentPreservesPresentZeroValues(t *testing.T) {
	t.Parallel()

	mapped, err := mapModelContent(ModelContent{
		Kind: ModelContentText, Position: 0, Text: mo.Some(""),
	}, true)
	require.NoError(t, err)
	assert.Equal(t, int64(0), mapped.GetPosition())
	assert.Empty(t, mapped.GetText())
}

// TestApprovedEnumValuesMapExactly verifies all approved nonzero enum values.
func TestApprovedEnumValuesMapExactly(t *testing.T) {
	t.Parallel()

	for index, value := range []RunState{RunStateIdle, RunStateRunning} {
		mapped, err := mapRunState(value)
		require.NoError(t, err)
		assert.Equal(t, programmaticv1.RunState(index+1), mapped)
	}
	for index, value := range []ModelContentKind{ModelContentText, ModelContentReasoning, ModelContentRefusal} {
		mapped, err := mapModelContentKind(value)
		require.NoError(t, err)
		assert.Equal(t, programmaticv1.ModelContentKind(index+1), mapped)
	}
	for index, value := range []ProgressChannel{ProgressChannelStatus, ProgressChannelStdout, ProgressChannelStderr} {
		mapped, err := mapProgressChannel(value)
		require.NoError(t, err)
		assert.Equal(t, programmaticv1.ProgressChannel(index+1), mapped)
	}
	for index, value := range []ModelOutcome{
		ModelOutcomeStop,
		ModelOutcomeToolUse,
		ModelOutcomeLength,
		ModelOutcomeAborted,
		ModelOutcomeFailed,
	} {
		mapped, err := mapModelOutcome(value)
		require.NoError(t, err)
		assert.Equal(t, programmaticv1.ModelOutcome(index+1), mapped)
	}
	for index, value := range []RunOutcome{RunOutcomeCompleted, RunOutcomeAborted, RunOutcomeFailed} {
		mapped, err := mapRunOutcome(value)
		require.NoError(t, err)
		assert.Equal(t, programmaticv1.RunOutcome(index+1), mapped)
	}
}
