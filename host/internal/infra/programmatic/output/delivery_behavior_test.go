//go:build !integration

package output

import (
	"context"
	"testing"

	"github.com/n-r-w/glyph/internal/operation"

	"github.com/samber/mo"

	"github.com/stretchr/testify/require"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// TestDeliveryStopsCleanlyWhenOwnerStreamEnds verifies owner teardown is not a delivery failure.
func TestDeliveryStopsCleanlyWhenOwnerStreamEnds(t *testing.T) {
	t.Parallel()

	delivery := New()
	require.True(t, delivery.Reserve("operation", "run"))
	delivery.active.failed = true

	require.NoError(t, delivery.DeliverAgent(t.Context(), testEmptyRunEvent(agent.EventAgentStart, "run")))
}

// TestDeliveryReturnsIndependentContextCancellation verifies delivery context ownership remains unchanged.
func TestDeliveryReturnsIndependentContextCancellation(t *testing.T) {
	t.Parallel()

	delivery := New()
	require.True(t, delivery.Reserve("operation", "run"))
	delivery.BindProgress("run", operation.Reporter[controller.OperationProgress]{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, delivery.DeliverAgent(ctx, testEmptyRunEvent(agent.EventAgentStart, "run")), context.Canceled)
}

// TestDeliveryRejectsMismatchedRun verifies events cannot cross active operations.
func TestDeliveryRejectsMismatchedRun(t *testing.T) {
	t.Parallel()

	delivery := New()
	require.True(t, delivery.Reserve("operation", "active"))

	err := delivery.DeliverAgent(
		t.Context(),
		agent.Event{
			Position:   mo.None[int](),
			Content:    mo.None[model.Content](),
			Message:    mo.None[model.Response](),
			Preview:    mo.None[model.ToolCallPreview](),
			ToolCall:   mo.None[model.ToolCall](),
			Progress:   mo.None[tool.Progress](),
			ToolResult: mo.None[agent.ToolResult](),
			Turn:       mo.None[agent.TurnSummary](),
			Agent:      mo.None[agent.RunSummary](),
			Type:       agent.EventAgentStart,
			RunID:      "other",
		},
	)

	require.Error(t, err)
}

// TestDeliveryRejectsMissingSelectedPayload verifies malformed variants do not reach Programmatic Control.
func TestDeliveryRejectsMissingSelectedPayload(t *testing.T) {
	t.Parallel()

	delivery := New()
	require.True(t, delivery.Reserve("operation", "run"))
	delivery.BindProgress("run", operation.Reporter[controller.OperationProgress]{})

	err := delivery.DeliverAgent(t.Context(), testEmptyRunEvent(agent.EventMessageEnd, "run"))

	require.ErrorContains(t, err, "requires model response")
}

// TestMapProgrammaticModelEventRejectsMalformedResponseContent verifies projection errors are returned.
func TestMapProgrammaticModelEventRejectsMalformedResponseContent(t *testing.T) {
	t.Parallel()

	event := agent.Event{
		Type:       agent.EventMessageEnd,
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
		Turn:  mo.None[agent.TurnSummary](),
		Agent: mo.None[agent.RunSummary](),
	}

	err := mapProgrammaticModelEvent(event, &controller.AgentEvent{})

	require.Error(t, err)
}
