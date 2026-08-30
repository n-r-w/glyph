package programmatic

import (
	"github.com/samber/mo"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// testProgrammaticCommand creates a payload-free command for lifecycle tests.
func testProgrammaticCommand(correlationID string, kind controller.CommandKind) controller.Command {
	return controller.Command{
		CorrelationID:   correlationID,
		Kind:            kind,
		UserText:        mo.None[string](),
		ProviderID:      mo.None[model.ProviderID](),
		ModelID:         mo.None[model.ID](),
		ReasoningChoice: mo.None[model.ReasoningChoice](),
		SessionID:       mo.None[session.ID](),
		SessionName:     mo.None[string](), TargetEntryID: mo.None[string](), SummaryMode: controller.SummaryModeNoSummary, CustomFocus: mo.None[string](), EntryLabel: mo.None[string](),
	}
}

// testProgrammaticUserCommand creates one user request command with no unrelated payload.
func testProgrammaticUserCommand(correlationID, text string) controller.Command {
	return controller.Command{
		CorrelationID:   correlationID,
		Kind:            controller.CommandUserRequest,
		UserText:        mo.Some(text),
		ProviderID:      mo.None[model.ProviderID](),
		ModelID:         mo.None[model.ID](),
		ReasoningChoice: mo.None[model.ReasoningChoice](),
		SessionID:       mo.None[session.ID](),
		SessionName:     mo.None[string](), TargetEntryID: mo.None[string](), SummaryMode: controller.SummaryModeNoSummary, CustomFocus: mo.None[string](), EntryLabel: mo.None[string](),
	}
}
