//go:build integration

package runtime

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
)

// newSettlementLifecycleInvocation supplies an agent-start event for observer settlement.
func newSettlementLifecycleInvocation() extensionruntime.LifecycleInvocation {
	return extensionruntime.LifecycleInvocation{
		Context:            runtimeTestContext(),
		Type:               agent.EventAgentStart,
		Settled:            false,
		RunID:              "settled-run",
		Position:           mo.None[int](),
		Content:            mo.None[extensionruntime.Content](),
		ToolCall:           mo.None[model.ToolCall](),
		Preview:            mo.None[model.ToolCallPreview](),
		Progress:           mo.None[tool.Progress](),
		ToolResult:         mo.None[agent.ToolResult](),
		Response:           mo.None[extensionruntime.Response](),
		TurnResults:        nil,
		Outcome:            mo.None[agent.RunOutcome](),
		ErrorMessage:       mo.None[string](),
		SelectionEvent:     false,
		ReasoningSelection: false,
		PrecedingSelection: model.Selection{},
		CommittedSelection: model.Selection{},
	}
}
