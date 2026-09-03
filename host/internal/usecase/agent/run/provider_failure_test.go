//go:build !integration

package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
)

// TestServiceRunProviderFailurePreservesStreamedText keeps partial text when the terminal error has no content.
func TestServiceRunProviderFailurePreservesStreamedText(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	tools.EXPECT().Tools().Return(nil)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ ModelRequest, handle StreamHandler) error {
			require.NoError(t, handle(testTextStreamEvent(
				StreamEventContentStart, 0, model.ContentText, "", mo.None[string](),
			)))
			require.NoError(t, handle(StreamEvent{
				Kind:     StreamEventTextDelta,
				Position: mo.Some(0),
				Content: mo.Some(
					model.Content{
						Kind:            model.ContentText,
						Text:            mo.Some("partial"),
						Final:           false,
						ProviderContext: mo.None[model.ProviderContext](),
						ToolCall:        mo.None[model.ToolCall](),
					},
				),
				Delta: mo.Some(
					"partial",
				),
				Preview:  mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](),
				Response: mo.None[model.Response](),
			}))
			return errors.New("provider transport failed")
		},
	)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service := newTestService(
		t,
		testInstructions,
		testModelDescriptor,
		model.ReasoningChoiceHigh,
		provider,
		tools,
		events,
	)

	_, err := service.Run(t.Context(), Request{RunID: "run-failed-partial", UserText: "hi"})

	require.Error(t, err)
	history := service.History()
	require.Len(t, history, 2)
	require.Len(t, history[1].Model.OrEmpty().Content, 1)
	assert.Equal(t, "partial", history[1].Model.OrEmpty().Content[0].Text.OrEmpty())
	assert.True(t, history[1].Model.OrEmpty().Content[0].Final)
}

// TestServiceRunProviderFailureRejectsMalformedRetainedContent verifies invalid gaps do not enter history or terminal
// frames.
func TestServiceRunProviderFailureRejectsMalformedRetainedContent(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	tools.EXPECT().Tools().Return(nil)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ ModelRequest, handle StreamHandler) error {
			require.NoError(t, handle(testTextStreamEvent(
				StreamEventContentStart, 1, model.ContentText, "", mo.None[string](),
			)))
			return errors.New("provider transport failed")
		},
	)
	delivered := make([]Event, 0)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, event Event) error {
			delivered = append(delivered, event)
			return nil
		},
	).AnyTimes()
	service := newTestService(
		t,
		testInstructions,
		testModelDescriptor,
		model.ReasoningChoiceHigh,
		provider,
		tools,
		events,
	)

	_, err := service.Run(t.Context(), Request{RunID: "run-malformed-partial", UserText: "hi"})

	require.ErrorContains(t, err, "provider transport failed")
	require.ErrorContains(t, err, "unknown kind")
	require.Len(t, service.History(), 1)
	assert.True(t, service.State().PartialResponse.IsNone())
	assert.NotContains(t, eventTypes(delivered), EventMessageEnd)
	assert.NotContains(t, eventTypes(delivered), EventTurnEnd)
}

// TestServiceRunProviderFailurePreservesSafeMessage keeps provider-approved detail in every terminal payload.
func TestServiceRunProviderFailurePreservesSafeMessage(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	tools.EXPECT().Tools().Return(nil)
	safeMessage := "Provider rate limit reached."
	actualModel := model.ID("gpt-actual")
	response := model.Response{
		Content: []model.Content{
			testTextItem("partial"),
			{
				Kind:            model.ContentToolCall,
				ToolCall:        mo.Some(model.ToolCall{ID: "unsafe", Name: "read", Arguments: map[string]any{}}),
				Text:            mo.None[string](),
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
			},
		},
		Outcome:       mo.Some(model.OutcomeStop),
		ErrorMessage:  mo.Some(safeMessage),
		Provider:      mo.Some(model.ProviderID("openai-codex")),
		Model:         mo.Some(model.ID("gpt-test")),
		ResponseModel: mo.Some(actualModel),
		ResponseID:    mo.Some("resp-failed"),
		Usage: mo.Some(
			model.Usage{
				InputTokens:       3,
				OutputTokens:      2,
				TotalTokens:       5,
				CachedInputTokens: 0,
				CacheWriteTokens:  0,
				ReasoningTokens:   0,
			},
		),
		Diagnostics: []model.Diagnostic{{Code: "provider_error", Message: safeMessage}},
	}
	provider.EXPECT().
		Stream(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(streamResult(response, errors.New("provider transport failed")))
	delivered := make([]Event, 0)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, event Event) error {
			delivered = append(delivered, event)
			return nil
		},
	).AnyTimes()
	service := newTestService(
		t,
		testInstructions,
		testModelDescriptor,
		model.ReasoningChoiceHigh,
		provider,
		tools,
		events,
	)

	result, err := service.Run(t.Context(), Request{RunID: "run-safe-error", UserText: "go"})

	require.Error(t, err)
	assert.Equal(t, safeMessage, result.ErrorMessage.OrEmpty())
	history := service.History()
	require.Len(t, history, 2)
	assert.Equal(t, safeMessage, history[1].Model.OrEmpty().ErrorMessage.OrEmpty())
	assert.Equal(t, model.OutcomeFailed, history[1].Model.OrEmpty().Outcome.OrEmpty())
	assert.Equal(t, "resp-failed", history[1].Model.OrEmpty().ResponseID.OrEmpty())
	assert.Equal(t, model.ID("gpt-actual"), history[1].Model.OrEmpty().ResponseModel.OrEmpty())
	mutatedResponse := history[1].Model.OrEmpty()
	mutatedResponse.ResponseModel = mo.Some(model.ID("mutated"))
	history[1].Model = mo.Some(mutatedResponse)
	preservedHistory := service.History()
	assert.Equal(t, model.ID("gpt-actual"), preservedHistory[1].Model.OrEmpty().ResponseModel.OrEmpty())
	assert.Equal(t, int64(5), history[1].Model.OrEmpty().Usage.OrEmpty().TotalTokens)
	assert.Equal(
		t,
		[]model.Diagnostic{{Code: "provider_error", Message: safeMessage}},
		history[1].Model.OrEmpty().Diagnostics,
	)
	assert.Len(t, history[1].Model.OrEmpty().Content, 2)
	assert.Len(t, service.ProjectHistory(), 1)
	var messageEnd Event
	var agentEnd Event
	for _, event := range delivered {
		if event.Type == EventMessageEnd {
			messageEnd = event
		}
		if event.Type == EventAgentEnd {
			agentEnd = event
		}
	}
	assert.Equal(t, safeMessage, messageEnd.Message.OrEmpty().ErrorMessage.OrEmpty())
	assert.Equal(t, safeMessage, agentEnd.Agent.OrEmpty().ErrorMessage.OrEmpty())
}

// TestServiceRunProviderAndContentEndFailurePreservesBothCauses verifies combined stream failures reach Agent
// boundaries once.
func TestServiceRunProviderAndContentEndFailurePreservesBothCauses(t *testing.T) {
	t.Parallel()

	// Arrange partial content, failed ContentEnd delivery, and one independent provider sibling.
	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	providerErr := errors.New("unique provider stream sibling")
	deliveryErr := errors.New("unique Agent ContentEnd delivery failure")
	tools.EXPECT().Tools().Return(nil)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ ModelRequest, handle StreamHandler) error {
			content := model.Content{
				Kind: model.ContentText, Text: mo.Some(""), Final: false,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
			}
			require.NoError(t, handle(StreamEvent{
				Kind: StreamEventContentStart, Position: mo.Some(0), Content: mo.Some(content),
				Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](), Response: mo.None[model.Response](),
			}))
			require.NoError(t, handle(StreamEvent{
				Kind: StreamEventTextDelta, Position: mo.Some(0), Content: mo.Some(content),
				Delta: mo.Some("partial"), Preview: mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](), Response: mo.None[model.Response](),
			}))
			endErr := handle(StreamEvent{
				Kind: StreamEventContentEnd, Position: mo.Some(0), Content: mo.Some(content),
				Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](), Response: mo.None[model.Response](),
			})
			require.ErrorIs(t, endErr, deliveryErr)
			return errors.Join(providerErr, endErr)
		},
	)
	var agentEnd AgentSummary
	observed := make([]EventType, 0)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event Event) error {
		observed = append(observed, event.Type)
		if event.Type == EventAgentEnd {
			agentEnd = event.Agent.OrEmpty()
		}
		if event.Type == EventContentEnd {
			return deliveryErr
		}
		return nil
	}).AnyTimes()
	service := newTestService(
		t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh,
		provider, tools, events,
	)

	// Act by running through the joined provider and recorded delivery failure.
	result, err := service.Run(t.Context(), Request{RunID: "provider-content-end-failure", UserText: "hello"})

	// Assert both causes occur once at each surviving local boundary.
	require.ErrorIs(t, err, providerErr)
	require.ErrorIs(t, err, deliveryErr)
	for _, text := range []string{err.Error(), result.ErrorMessage.OrEmpty(), agentEnd.ErrorMessage.OrEmpty()} {
		assert.Equal(t, 1, strings.Count(text, providerErr.Error()), text)
		assert.Equal(t, 1, strings.Count(text, deliveryErr.Error()), text)
	}
	assert.NotContains(t, observed, EventMessageEnd)
	assert.NotContains(t, observed, EventTurnEnd)
}

// TestServiceRunProviderAndPersistenceFailurePreservesBothCauses verifies combined failures reach every terminal
// boundary.
func TestServiceRunProviderAndPersistenceFailurePreservesBothCauses(t *testing.T) {
	t.Parallel()

	// Arrange a provider failure followed by failed-response persistence failure.
	controller := gomock.NewController(t)
	runtime := NewMockModelRuntime(controller)
	provider := NewMockModelProvider(controller)
	tools := NewMockToolRuntime(controller)
	events := NewMockEventSink(controller)
	store := NewMockHistoryStore(controller)
	providerErr := errors.New("unique provider failure")
	persistenceErr := fmt.Errorf("%w: unique failed-response persistence failure", ErrPersistenceUnavailable)
	history := make([]agent.HistoryEntry, 0, 1)
	store.EXPECT().Snapshot().DoAndReturn(func() []agent.HistoryEntry { return cloneHistory(history) }).AnyTimes()
	store.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, entry agent.HistoryEntry) error {
			history = append(history, entry.Clone())
			return nil
		},
	)
	store.EXPECT().Append(gomock.Any(), gomock.Any()).Return(persistenceErr)
	runtime.EXPECT().Snapshot().Return(RequestSnapshot{
		Model: testModelDescriptor, ReasoningChoice: model.ReasoningChoiceHigh, Provider: provider,
	})
	tools.EXPECT().Tools().Return(nil)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ ModelRequest, handle StreamHandler) error {
			return emitStream(handle, emptyModelResponse(model.OutcomeFailed), providerErr)
		},
	)
	var agentEnd AgentSummary
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event Event) error {
		if event.Type == EventAgentEnd {
			agentEnd = event.Agent.OrEmpty()
		}
		return nil
	}).AnyTimes()
	service := New(testInstructions, runtime, tools, events, store)

	// Act by running the combined provider and persistence failure.
	result, err := service.Run(t.Context(), Request{RunID: "combined-failure", UserText: "hello"})

	// Assert the returned error, result, and terminal Agent event retain both causes.
	require.Error(t, err)
	require.ErrorIs(t, err, providerErr)
	require.ErrorIs(t, err, persistenceErr)
	for _, text := range []string{result.ErrorMessage.OrEmpty(), agentEnd.ErrorMessage.OrEmpty()} {
		assert.True(t, strings.HasPrefix(text, ErrPersistenceUnavailable.Error()), text)
		assert.Equal(t, 1, strings.Count(text, providerErr.Error()), text)
		assert.Equal(t, 1, strings.Count(text, persistenceErr.Error()), text)
	}
}

// TestServiceRunProviderFailure exposes partial state, stores the provider cause, and excludes it from projection.
func TestServiceRunProviderFailure(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		provider := NewMockModelProvider(gomock.NewController(t))
		tools := NewMockToolRuntime(gomock.NewController(t))
		events := NewMockEventSink(gomock.NewController(t))
		streamed := make(chan struct{})
		release := make(chan struct{})
		tools.EXPECT().Tools().Return(nil)
		partial := model.Response{
			Content: []model.Content{testTextItem("partial")},
			Outcome: mo.Some(
				model.OutcomeStop,
			),
			ErrorMessage:  mo.None[string](),
			Provider:      mo.None[model.ProviderID](),
			Model:         mo.None[model.ID](),
			ResponseModel: mo.None[model.ID](),
			ResponseID:    mo.None[string](),
			Usage:         mo.None[model.Usage](),
			Diagnostics:   nil,
		}
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ ModelRequest, update StreamHandler) error {
				require.NoError(t, emitText(update, 0, "partial"))
				close(streamed)
				<-release
				return emitStream(update, partial, errors.New("provider secret"))
			},
		)
		events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		service := newTestService(
			t,
			testInstructions,
			testModelDescriptor,
			model.ReasoningChoiceHigh,
			provider,
			tools,
			events,
		)
		outcome := make(chan error, 1)
		go func() {
			_, err := service.Run(t.Context(), Request{RunID: "run-failed", UserText: "hi"})
			outcome <- err
		}()
		select {
		case earlyErr := <-outcome:
			require.NoError(t, earlyErr)
		case <-streamed:
			state := service.State()
			assert.Equal(t, StatusRunning, state.Status)
			require.Len(t, state.PartialResponse.OrEmpty().Content, 1)
			assert.Equal(t, "partial", state.PartialResponse.OrEmpty().Content[0].Text.OrEmpty())
			historyBefore := service.History()
			_, secondErr := service.Run(t.Context(), Request{RunID: "blocked", UserText: "no"})
			require.ErrorIs(t, secondErr, ErrRunActive)
			assert.Equal(t, historyBefore, service.History())
			close(release)
			synctest.Wait()
			require.Error(t, <-outcome)
		}
		assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
		assert.Empty(t, service.State().PartialResponse.OrEmpty().Content)
		history := service.History()
		require.Len(t, history, 2)
		assert.Equal(t, model.OutcomeFailed, history[1].Model.OrEmpty().Outcome.OrEmpty())
		assert.Contains(t, history[1].Model.OrEmpty().ErrorMessage.OrEmpty(), "provider secret")
		assert.Len(t, service.ProjectHistory(), 1)
	})
}

// TestServiceRunRejectsUnknownTerminalOutcome verifies rejection before malformed history mutation.
func TestServiceRunRejectsUnknownTerminalOutcome(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	tools.EXPECT().Tools().Return(nil)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(
		model.Response{
			Content: nil, Outcome: mo.Some(model.Outcome(99)), ErrorMessage: mo.None[string](),
			Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
			ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
		},
		nil,
	))
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service := newTestService(
		t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh,
		provider, tools, events,
	)

	_, err := service.Run(t.Context(), Request{RunID: "run-unknown-outcome", UserText: "hi"})

	require.ErrorContains(t, err, "unsupported terminal model outcome 99")
	assert.True(t, service.State().PartialResponse.IsNone())
	history := service.History()
	require.Len(t, history, 2)
	assert.Equal(t, model.OutcomeFailed, history[1].Model.OrEmpty().Outcome.OrEmpty())
	for _, message := range history {
		assert.NotEqual(t, model.Outcome(99), message.Model.OrEmpty().Outcome.OrEmpty())
	}
}
