package programmatic

import (
	"fmt"

	"github.com/samber/mo"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// mapHistory projects user, model, and tool history into public conversation entries.
func mapHistory(history []agent.HistoryEntry) ([]controller.HistoryEntry, error) {
	result := make([]controller.HistoryEntry, 0, len(history))
	for position := range history {
		entry := &history[position]
		switch entry.Kind {
		case agent.HistoryEntryUser:
			user, present := entry.User.Get()
			if !present {
				return nil, fmt.Errorf("map history entry %d: user payload is missing", position)
			}
			result = append(result, controller.HistoryEntry{
				Kind: controller.HistoryEntryUser, User: mo.Some(user.Clone()),
				Model:      mo.None[controller.ModelResponse](),
				ToolResult: mo.None[controller.ToolResult](), ExtensionMessage: mo.None[controller.ExtensionMessage](),
			})
		case agent.HistoryEntryModel:
			response, present := entry.Model.Get()
			if !present {
				return nil, fmt.Errorf("map history entry %d: model payload is missing", position)
			}
			mapped, err := controller.MapModelResponseProjection(response)
			if err != nil {
				return nil, fmt.Errorf("map history entry %d: %w", position, err)
			}
			result = append(result, controller.HistoryEntry{
				Kind: controller.HistoryEntryModel,
				User: mo.None[model.Message](),
				Model: mo.Some(
					mapped,
				),
				ToolResult:       mo.None[controller.ToolResult](),
				ExtensionMessage: mo.None[controller.ExtensionMessage](),
			})
		case agent.HistoryEntryToolResult:
			toolResult, present := entry.ToolResult.Get()
			if !present {
				return nil, fmt.Errorf("map history entry %d: tool result payload is missing", position)
			}
			publicToolResult := controller.MapToolResult(toolResult)
			result = append(result, controller.HistoryEntry{
				Kind:             controller.HistoryEntryToolResult,
				User:             mo.None[model.Message](),
				Model:            mo.None[controller.ModelResponse](),
				ToolResult:       mo.Some(publicToolResult),
				ExtensionMessage: mo.None[controller.ExtensionMessage](),
			})
		default:
			return nil, fmt.Errorf("map history entry %d: unknown kind %d", position, entry.Kind)
		}
	}
	return result, nil
}

// mapSessionEntries projects the public conversation for a prepared session query.
func mapSessionEntries(entries []session.Entry) ([]controller.SessionEntry, error) {
	result := make([]controller.SessionEntry, 0, len(entries))
	for position := range entries {
		projected, present, err := controller.ProjectSessionEntry(entries[position], position)
		if err != nil {
			return nil, err
		}
		if present {
			result = append(result, projected)
		}
	}
	return result, nil
}
