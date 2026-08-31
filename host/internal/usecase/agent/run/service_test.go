//go:build !integration

package run

import (
	"context"
	"testing"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/hooks"

	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
)

const testInstructions = "resolved coding instructions"

var testModelDescriptor = model.Descriptor{
	Provider: "openai-codex", Model: "gpt-test",
	Input: nil, ContextWindow: 0, MaxTokens: 0,
	ReasoningCapabilities: model.ReasoningCapabilities{}, ToolCapabilities: model.ToolCapabilities{},
	Pricing: mo.None[model.Pricing](),
}

func newTestService(
	t *testing.T,
	instructions string,
	descriptor model.Descriptor,
	choice model.ReasoningChoice,
	provider ModelProvider,
	hookRunner hooks.ContextRunner,
	tools ToolRuntime,
	events EventSink,
) *Service {
	t.Helper()
	runtime := NewMockModelRuntime(gomock.NewController(t))
	runtime.EXPECT().Snapshot().Return(RequestSnapshot{
		Model: descriptor, ReasoningChoice: choice, Provider: provider,
	}).AnyTimes()
	return New(instructions, runtime, hookRunner, tools, events, newMockHistoryStore(t))
}

func newMockHistoryStore(t *testing.T) *MockHistoryStore {
	t.Helper()
	store := NewMockHistoryStore(gomock.NewController(t))
	history := make([]agent.HistoryEntry, 0)
	store.EXPECT().Snapshot().DoAndReturn(func() []agent.HistoryEntry {
		return cloneHistory(history)
	}).AnyTimes()
	store.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, entry agent.HistoryEntry) error {
			history = append(history, entry.Clone())
			return nil
		},
	).AnyTimes()
	return store
}

// testTextItem creates one complete text content item.
func testTextItem(text string) model.Content {
	return model.Content{
		Kind:            model.ContentText,
		Text:            mo.Some(text),
		Final:           true,
		ProviderContext: mo.None[model.ProviderContext](),
		ToolCall:        mo.None[model.ToolCall](),
	}
}

// testCallItem creates one complete tool-call content item.
func testCallItem(call model.ToolCall) model.Content {
	return model.Content{
		Kind:            model.ContentToolCall,
		ToolCall:        mo.Some(call),
		Text:            mo.None[string](),
		Final:           false,
		ProviderContext: mo.None[model.ProviderContext](),
	}
}

// eventTypes extracts observable event order for compact assertions.
func eventTypes(events []Event) []EventType {
	return lo.Map(events, func(event Event, _ int) EventType {
		return event.Type
	})
}

// streamResult returns one deterministic semantic provider stream for tests.
func streamResult(response model.Response, streamErr error) func(context.Context, ModelRequest, StreamHandler) error {
	return func(_ context.Context, _ ModelRequest, handle StreamHandler) error {
		return emitStream(handle, response, streamErr)
	}
}

// emitText emits one complete text block.
func emitText(handle StreamHandler, position int, text string) error {
	if err := handle(StreamEvent{
		Kind:     StreamEventContentStart,
		Position: mo.Some(position),
		Content: mo.Some(
			model.Content{
				Kind:            model.ContentText,
				Text:            mo.Some(""),
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
			},
		),
		Delta:    mo.None[string](),
		Preview:  mo.None[model.ToolCallPreview](),
		ToolCall: mo.None[model.ToolCall](),
		Response: mo.None[model.Response](),
	}); err != nil {
		return err
	}
	if err := handle(StreamEvent{
		Kind:     StreamEventTextDelta,
		Position: mo.Some(position),
		Content: mo.Some(
			model.Content{
				Kind:            model.ContentText,
				Text:            mo.Some(text),
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
			},
		),
		Delta:    mo.Some(text),
		Preview:  mo.None[model.ToolCallPreview](),
		ToolCall: mo.None[model.ToolCall](),
		Response: mo.None[model.Response](),
	}); err != nil {
		return err
	}
	return handle(StreamEvent{
		Kind:     StreamEventContentEnd,
		Position: mo.Some(position),
		Content: mo.Some(
			model.Content{
				Kind:            model.ContentText,
				Text:            mo.Some(""),
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
			},
		),
		Delta:    mo.None[string](),
		Preview:  mo.None[model.ToolCallPreview](),
		ToolCall: mo.None[model.ToolCall](),
		Response: mo.None[model.Response](),
	})
}

// emptyModelResponse creates a response without content or provider metadata.
func emptyModelResponse(outcome model.Outcome) model.Response {
	return model.Response{
		Content:       nil,
		Outcome:       mo.Some(outcome),
		ErrorMessage:  mo.None[string](),
		Provider:      mo.None[model.ProviderID](),
		Model:         mo.None[model.ID](),
		ResponseModel: mo.None[model.ID](),
		ResponseID:    mo.None[string](),
		Usage:         mo.None[model.Usage](),
		Diagnostics:   nil,
	}
}

// emitStream emits terminal content ends followed by one terminal event.
func emitStream(handle StreamHandler, response model.Response, streamErr error) error {
	kind := StreamEventDone
	if streamErr != nil {
		kind = StreamEventError
	}
	if err := handle(
		StreamEvent{
			Kind:     kind,
			Response: mo.Some(response),
			Position: mo.None[int](),
			Content:  mo.None[model.Content](),
			Delta:    mo.None[string](),
			Preview:  mo.None[model.ToolCallPreview](),
			ToolCall: mo.None[model.ToolCall](),
		},
	); err != nil {
		return err
	}
	return streamErr
}
