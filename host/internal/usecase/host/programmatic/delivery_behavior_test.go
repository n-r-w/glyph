package programmatic

import (
	"context"
	"testing"

	"github.com/samber/mo"

	"github.com/stretchr/testify/require"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestDeliveryStopsCleanlyWhenOwnerStreamEnds verifies owner teardown is not a delivery failure.
func TestDeliveryStopsCleanlyWhenOwnerStreamEnds(t *testing.T) {
	t.Parallel()

	delivery := NewDelivery()
	active := newTestActiveRun(t.Context(), delivery, "correlation", "run")
	defer active.cancel()
	close(active.streamDone)

	require.NoError(t, delivery.emit(t.Context(), active, controller.AgentEvent{}))
}

// TestDeliveryReturnsIndependentContextCancellation verifies delivery context ownership remains unchanged.
func TestDeliveryReturnsIndependentContextCancellation(t *testing.T) {
	t.Parallel()

	delivery := NewDelivery()
	active := newTestActiveRun(t.Context(), delivery, "correlation", "run")
	defer active.cancel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, delivery.emit(ctx, active, controller.AgentEvent{}), context.Canceled)
}

// TestDeliveryRejectsMismatchedRun verifies events cannot cross active correlations.
func TestDeliveryRejectsMismatchedRun(t *testing.T) {
	t.Parallel()

	delivery := NewDelivery()
	active := newTestActiveRun(t.Context(), delivery, "correlation", "active")
	require.True(t, delivery.reserve(active))

	err := delivery.DeliverAgent(
		t.Context(),
		run.Event{
			Position:   mo.None[int](),
			Content:    mo.None[model.Content](),
			Message:    mo.None[model.Response](),
			Preview:    mo.None[model.ToolCallPreview](),
			ToolCall:   mo.None[model.ToolCall](),
			Progress:   mo.None[tool.Progress](),
			ToolResult: mo.None[agent.ToolResult](),
			Turn:       mo.None[run.TurnSummary](),
			Agent:      mo.None[run.AgentSummary](),
			Type:       run.EventAgentStart,
			RunID:      "other",
		},
	)

	require.Error(t, err)
	delivery.finish(active, nil)
}

// TestDeliveryRejectsMissingSelectedPayload verifies malformed variants do not reach Programmatic Control.
func TestDeliveryRejectsMissingSelectedPayload(t *testing.T) {
	t.Parallel()

	delivery := NewDelivery()
	active := newTestActiveRun(t.Context(), delivery, "correlation", "run")
	require.True(t, delivery.reserve(active))

	err := delivery.DeliverAgent(t.Context(), run.Event{
		Type:       run.EventMessageEnd,
		RunID:      "run",
		Position:   mo.None[int](),
		Content:    mo.None[model.Content](),
		Message:    mo.None[model.Response](),
		Preview:    mo.None[model.ToolCallPreview](),
		ToolCall:   mo.None[model.ToolCall](),
		Progress:   mo.None[tool.Progress](),
		ToolResult: mo.None[agent.ToolResult](),
		Turn:       mo.None[run.TurnSummary](),
		Agent:      mo.None[run.AgentSummary](),
	})

	require.ErrorContains(t, err, "requires model response")
	delivery.finish(active, nil)
}

// TestMapProgrammaticModelEventRejectsMalformedResponseContent verifies projection errors are returned.
func TestMapProgrammaticModelEventRejectsMalformedResponseContent(t *testing.T) {
	t.Parallel()

	event := run.Event{
		Type:       run.EventMessageEnd,
		RunID:      "run",
		Position:   mo.None[int](),
		Content:    mo.None[model.Content](),
		Preview:    mo.None[model.ToolCallPreview](),
		ToolCall:   mo.None[model.ToolCall](),
		Progress:   mo.None[tool.Progress](),
		ToolResult: mo.None[agent.ToolResult](),
		Message: mo.Some(model.Response{
			Content: []model.Content{{
				Kind:            model.ContentText,
				Text:            mo.None[string](),
				Final:           true,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
			}},
			Outcome: mo.None[model.Outcome](), ErrorMessage: mo.None[string](),
			Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](),
			ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
			Usage: mo.None[model.Usage](), Diagnostics: nil,
		}),
		Turn:  mo.None[run.TurnSummary](),
		Agent: mo.None[run.AgentSummary](),
	}

	err := mapProgrammaticModelEvent(event, &controller.AgentEvent{})

	require.Error(t, err)
}
