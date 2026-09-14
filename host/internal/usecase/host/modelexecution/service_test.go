//go:build !integration

package modelexecution

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestServiceStreamsOneLogicalRequest verifies Agent Core semantic events pass through one raw attempt.
func TestServiceStreamsOneLogicalRequest(t *testing.T) {
	t.Parallel()

	// Arrange one exact binding, immutable history, tools, and one semantic event.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh}
	descriptor := modelDescriptor(selection)
	catalog.EXPECT().ResolveBinding(selection).Return(CatalogBinding{
		Model: descriptor, ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	history := []agent.HistoryEntry{{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("question")),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}}
	tools := []tool.Descriptor{{
		Name: "search", Description: "Search files", InputSchemaJSON: []byte(`{"type":"object"}`),
		ConstrainedSampling: mo.None[tool.ConstrainedSampling](),
	}}
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ProviderRequest, handle StreamHandler) error {
			assert.Equal(t, descriptor, request.Model)
			assert.Equal(t, history, request.History)
			assert.Equal(t, tools, request.Tools)
			request.History[0].User.MustGet().Content[0].Text = mo.Some("changed")
			return handle(StreamEvent{
				Kind: StreamEventTextDelta, Position: mo.Some(0),
				Content: mo.Some(model.Content{
					Kind: model.ContentText, Text: mo.Some("delta"), Final: false,
					ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
				}),
				Delta: mo.Some("delta"), Preview: mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](), Response: mo.None[model.Response](),
			})
		},
	)
	service := New(catalog)
	var received agentrun.StreamEvent

	// Act through the Agent Core logical provider contract.
	err := service.Stream(t.Context(), agentrun.ModelRequest{
		Instructions: "instructions", Model: descriptor, ReasoningChoice: selection.ReasoningChoice,
		History: history, Tools: tools,
	}, func(event agentrun.StreamEvent) error {
		received = event
		return nil
	})

	// Assert the semantic event passed through and caller history stayed unchanged.
	require.NoError(t, err)
	assert.Equal(t, agentrun.StreamEventTextDelta, received.Kind)
	assert.Equal(t, "delta", received.Delta.MustGet())
	assert.Equal(t, "question", history[0].User.MustGet().Content[0].Text.MustGet())
}

// TestLogicalStreamEventMapsKindsExplicitly verifies raw events use Agent Core kinds without numeric coupling.
func TestLogicalStreamEventMapsKindsExplicitly(t *testing.T) {
	t.Parallel()

	// Arrange every supported raw event kind and one unsupported kind.
	tests := []struct {
		// name identifies the raw event transition.
		name string
		// rawKind contains the Host-owned provider event kind.
		rawKind StreamEventKind
		// logicalKind contains the Agent Core consumer event kind.
		logicalKind agentrun.StreamEventKind
	}{
		{name: "content start", rawKind: StreamEventContentStart, logicalKind: agentrun.StreamEventContentStart},
		{name: "text delta", rawKind: StreamEventTextDelta, logicalKind: agentrun.StreamEventTextDelta},
		{name: "content end", rawKind: StreamEventContentEnd, logicalKind: agentrun.StreamEventContentEnd},
		{name: "tool call start", rawKind: StreamEventToolCallStart, logicalKind: agentrun.StreamEventToolCallStart},
		{name: "tool call delta", rawKind: StreamEventToolCallDelta, logicalKind: agentrun.StreamEventToolCallDelta},
		{name: "tool call end", rawKind: StreamEventToolCallEnd, logicalKind: agentrun.StreamEventToolCallEnd},
		{name: "done", rawKind: StreamEventDone, logicalKind: agentrun.StreamEventDone},
		{name: "error", rawKind: StreamEventError, logicalKind: agentrun.StreamEventError},
		{name: "unsupported", rawKind: StreamEventKind(255), logicalKind: agentrun.StreamEventKind(0)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Act by mapping one raw event at the Host-to-Core boundary.
			mapped := logicalStreamEvent(StreamEvent{
				Kind: test.rawKind, Position: mo.None[int](), Content: mo.None[model.Content](),
				Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](), Response: mo.None[model.Response](),
			})

			// Assert the consumer receives the corresponding kind or its invalid zero value.
			assert.Equal(t, test.logicalKind, mapped.Kind)
		})
	}
}

// TestServiceConfiguredRequestReturnsDetachedTerminal verifies exact configured dispatch and terminal reduction.
func TestServiceConfiguredRequestReturnsDetachedTerminal(t *testing.T) {
	t.Parallel()

	// Arrange one credential-preflight resolution and terminal raw provider response.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow}
	descriptor := modelDescriptor(selection)
	catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), selection).Return(CatalogBinding{
		Model: descriptor, ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	history := []agent.HistoryEntry{{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("question")),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}}
	terminal := model.Response{
		Content: []model.Content{{
			Kind: model.ContentText, Text: mo.Some("answer"), Final: true,
			ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
		}},
		Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
		Provider: mo.Some(selection.Provider), Model: mo.Some(selection.Model),
		ResponseModel: mo.Some(selection.Model), ResponseID: mo.Some("response"),
		Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ProviderRequest, handle StreamHandler) error {
			assert.Equal(t, "instructions", request.Instructions)
			assert.Equal(t, history, request.History)
			assert.Empty(t, request.Tools)
			if err := handle(StreamEvent{
				Kind: StreamEventDone, Position: mo.None[int](), Content: mo.None[model.Content](),
				Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](), Response: mo.Some(terminal),
			}); err != nil {
				return err
			}
			terminal.Content[0].Text = mo.Some("mutated")
			return nil
		},
	)
	service := New(catalog)

	// Act through the configured-request contract.
	response, err := service.Request(t.Context(), selection, "instructions", history)

	// Assert the response is detached and configured requests expose no tools.
	require.NoError(t, err)
	assert.Equal(t, "answer", response.Content[0].Text.MustGet())
}

// TestServiceConfiguredRequestRejectsMissingTerminal verifies both incomplete terminal shapes return zero responses.
func TestServiceConfiguredRequestRejectsMissingTerminal(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		// name identifies the incomplete provider stream.
		name string
		// execute runs the raw attempt behavior.
		execute func(StreamHandler) error
		// errorText identifies the preserved reduction failure.
		errorText string
	}{
		{
			name: "terminal without response",
			execute: func(handle StreamHandler) error {
				return handle(StreamEvent{
					Kind: StreamEventDone, Position: mo.None[int](), Content: mo.None[model.Content](),
					Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](),
					ToolCall: mo.None[model.ToolCall](), Response: mo.None[model.Response](),
				})
			},
			errorText: "model request terminal event has no response",
		},
		{
			name:      "completion without terminal",
			execute:   func(StreamHandler) error { return nil },
			errorText: "model request ended without a terminal response",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one resolved configured binding with incomplete raw completion.
			controller := gomock.NewController(t)
			catalog := NewMockCatalogResolver(controller)
			provider := NewMockProviderAttempt(controller)
			selection := model.Selection{
				Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff,
			}
			catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), selection).Return(CatalogBinding{
				Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
			}, nil)
			provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ ProviderRequest, handle StreamHandler) error {
					return test.execute(handle)
				},
			)
			service := New(catalog)

			// Act through terminal reduction.
			response, err := service.Request(t.Context(), selection, "instructions", nil)

			// Assert the incomplete stream is rejected with a zero response.
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.errorText)
			assert.Equal(t, model.Response{}, response)
		})
	}
}

// TestServiceConfiguredRequestPreservesProviderFailure verifies one raw failure is returned with a zero response.
func TestServiceConfiguredRequestPreservesProviderFailure(t *testing.T) {
	t.Parallel()

	// Arrange one resolved binding whose single raw attempt fails.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), selection).Return(CatalogBinding{
		Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	providerErr := errors.New("complete provider failure")
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(providerErr)
	service := New(catalog)

	// Act through one configured request.
	response, err := service.Request(t.Context(), selection, "instructions", nil)

	// Assert complete provider semantics and the zero response are preserved.
	require.ErrorIs(t, err, providerErr)
	assert.Contains(t, err.Error(), providerErr.Error())
	assert.Equal(t, model.Response{}, response)
}

// modelDescriptor returns one complete descriptor for service tests.
func modelDescriptor(selection model.Selection) model.Descriptor {
	return model.Descriptor{
		Provider: selection.Provider, Model: selection.Model, Input: []model.InputModality{model.InputModalityText},
		ContextWindow: 128000, MaxTokens: 16000,
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: true, Choices: []model.ReasoningChoice{selection.ReasoningChoice},
			Default: selection.ReasoningChoice,
		},
		ToolCapabilities: model.ToolCapabilities{}, Pricing: mo.None[model.Pricing](),
	}
}
