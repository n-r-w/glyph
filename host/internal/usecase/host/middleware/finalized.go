package middleware

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// FinalizeModelResponse runs the finalized-message handlers for one terminal model response of Agent Core.
func (s *Service) FinalizeModelResponse(ctx context.Context, response model.Response) (model.Response, error) {
	final, err := s.finalizeMessage(ctx, keepBeforeAdmission, agent.HistoryEntry{
		Kind: agent.HistoryEntryModel, User: mo.None[model.Message](), Model: mo.Some(response),
		ToolResult: mo.None[agent.ToolResult](),
	})
	return final.Model.OrEmpty(), err
}

// FinalizeToolResult runs the finalized-message handlers for one completed tool result of Agent Core.
func (s *Service) FinalizeToolResult(ctx context.Context, result agent.ToolResult) (agent.ToolResult, error) {
	final, err := s.finalizeMessage(ctx, keepBeforeAdmission, agent.HistoryEntry{
		Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](), Model: mo.None[model.Response](),
		ToolResult: mo.Some(result),
	})
	return final.ToolResult.OrEmpty(), err
}

// FinalizeConfiguredResponse runs the finalized-message handlers for one terminal configured-model response.
func (s *Service) FinalizeConfiguredResponse(ctx context.Context, response model.Response) (model.Response, error) {
	final, err := s.finalizeMessage(ctx, failOnCancellation, agent.HistoryEntry{
		Kind: agent.HistoryEntryModel, User: mo.None[model.Message](), Model: mo.Some(response),
		ToolResult: mo.None[agent.ToolResult](),
	})
	return final.Model.OrEmpty(), err
}

// finalizeMessage runs the finalized-message handlers in registration order and returns the final message, which
// keeps the role of the original and, for a model response, its Host-owned outcome, identity and accounting.
func (s *Service) finalizeMessage(
	ctx context.Context,
	boundary cancellationBoundary,
	message agent.HistoryEntry,
) (agent.HistoryEntry, error) {
	original := message.Clone()
	chain := transformer[agent.HistoryEntry]{
		kind:  FinalizedMessage,
		clone: agent.HistoryEntry.Clone,
		accept: func(replacement agent.HistoryEntry) (agent.HistoryEntry, error) {
			return acceptFinalizedReplacement(original, replacement)
		},
		handles:      false,
		cancellation: boundary,
		handle:       s.runtime.HandleFinalizedMessage,
	}
	result, err := compose(ctx, s, s.snapshotHandlers(), chain, original)
	if err != nil {
		return agent.HistoryEntry{}, categorizeChainFailure(ctx, err)
	}
	return result.value, nil
}

// acceptFinalizedReplacement validates a replacement against its finalized original and returns a detached final
// message that takes only content and replay from the replacement.
func acceptFinalizedReplacement(original, replacement agent.HistoryEntry) (agent.HistoryEntry, error) {
	if replacement.Kind != original.Kind {
		return agent.HistoryEntry{}, fmt.Errorf(
			"message role %s differs from original role %s", replacement.Kind, original.Kind,
		)
	}
	final, cloneErr := replacement.ValidatedClone()
	if cloneErr != nil {
		return agent.HistoryEntry{}, cloneErr
	}
	switch final.Kind {
	case agent.HistoryEntryUser:
		if err := final.User.OrEmpty().ValidateShape(); err != nil {
			return agent.HistoryEntry{}, err
		}
	case agent.HistoryEntryModel:
		response := original.Model.OrEmpty().Clone()
		response.Content = final.Model.OrEmpty().Content
		if err := response.ValidateTerminalContent(); err != nil {
			return agent.HistoryEntry{}, err
		}
		// Agent Core executes the calls of the final response under the outcome kept from the original.
		originalCalls, replacementCalls := original.Model.OrEmpty().ToolCalls(), response.ToolCalls()
		if !slices.EqualFunc(originalCalls, replacementCalls, sameToolCallIdentity) {
			return agent.HistoryEntry{}, fmt.Errorf(
				"tool calls %s differ from finalized tool calls %s",
				formatToolCalls(replacementCalls), formatToolCalls(originalCalls),
			)
		}
		final.Model = mo.Some(response)
	case agent.HistoryEntryToolResult:
		originalResult, replacementResult := original.ToolResult.OrEmpty(), final.ToolResult.OrEmpty()
		if replacementResult.CallID != originalResult.CallID {
			return agent.HistoryEntry{}, fmt.Errorf(
				"tool call %q differs from finalized call %q", replacementResult.CallID, originalResult.CallID,
			)
		}
		if replacementResult.ToolName != originalResult.ToolName {
			return agent.HistoryEntry{}, fmt.Errorf(
				"tool %q differs from finalized tool %q", replacementResult.ToolName, originalResult.ToolName,
			)
		}
		if err := replacementResult.ValidateImages(); err != nil {
			return agent.HistoryEntry{}, err
		}
	}
	return final, nil
}

// sameToolCallIdentity reports whether two tool calls have the same identifier and name.
func sameToolCallIdentity(original, replacement model.ToolCall) bool {
	return original.ID == replacement.ID && original.Name == replacement.Name
}

// formatToolCalls lists the identifier and name of each tool call for failure text.
func formatToolCalls(calls []model.ToolCall) string {
	identities := make([]string, len(calls))
	for index, call := range calls {
		identities[index] = fmt.Sprintf("id=%q name=%q", call.ID, call.Name)
	}
	return "[" + strings.Join(identities, ", ") + "]"
}
