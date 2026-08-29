package sessions

import (
	"bytes"

	"errors"
	"fmt"
	"maps"

	"slices"

	"strings"

	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

func terminalContinuationEntry(history agent.HistoryEntry) (session.Entry, bool, error) {
	entry := session.Entry{
		ID: "", CreatedAt: time.Time{}, Information: mo.None[session.Information](),
		User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		EstimatedCost: mo.None[session.EstimatedCost](),
	}
	switch history.Kind {
	case agent.HistoryEntryUser:
		entry.User = mo.Some(cloneMessage(history.User.MustGet()))
	case agent.HistoryEntryModel:
		response := history.Model.MustGet()
		outcome, terminal := response.Outcome.Get()
		if !terminal || outcome < model.OutcomeStop || outcome > model.OutcomeFailed {
			return session.Entry{}, false, nil
		}
		entry.Model = mo.Some(cloneModelResponse(response))
	case agent.HistoryEntryToolResult:
		entry.ToolResult = mo.Some(cloneToolResult(history.ToolResult.MustGet()))
	default:
		return session.Entry{}, false, fmt.Errorf("unsupported history entry kind %d", history.Kind)
	}
	return entry, true, nil
}

func historyFromEntries(entries []session.Entry) []agent.HistoryEntry {
	history := make([]agent.HistoryEntry, 0, len(entries))
	for entryIndex := range entries {
		entry := &entries[entryIndex]
		if user, present := entry.User.Get(); present {
			history = append(history, agent.HistoryEntry{
				Kind: agent.HistoryEntryUser, User: mo.Some(cloneMessage(user)),
				Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
			})
		}
		if response, present := entry.Model.Get(); present {
			history = append(history, agent.HistoryEntry{
				Kind: agent.HistoryEntryModel, User: mo.None[model.Message](),
				Model: mo.Some(cloneModelResponse(response)), ToolResult: mo.None[agent.ToolResult](),
			})
		}
		if result, present := entry.ToolResult.Get(); present {
			history = append(history, agent.HistoryEntry{
				Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](),
				Model: mo.None[model.Response](), ToolResult: mo.Some(cloneToolResult(result)),
			})
		}
	}
	return history
}

func cloneHistory(history []agent.HistoryEntry) []agent.HistoryEntry {
	cloned := slices.Clone(history)
	for index := range cloned {
		cloned[index], _ = cloneValidatedHistoryEntry(cloned[index])
	}
	return cloned
}

func cloneValidatedHistoryEntry(entry agent.HistoryEntry) (agent.HistoryEntry, error) {
	switch entry.Kind {
	case agent.HistoryEntryUser:
		message, present := entry.User.Get()
		if !present {
			return agent.HistoryEntry{}, errors.New("user history payload is missing")
		}
		return agent.HistoryEntry{
			Kind: entry.Kind, User: mo.Some(cloneMessage(message)), Model: mo.None[model.Response](),
			ToolResult: mo.None[agent.ToolResult](),
		}, nil
	case agent.HistoryEntryModel:
		response, present := entry.Model.Get()
		if !present {
			return agent.HistoryEntry{}, errors.New("model history payload is missing")
		}
		return agent.HistoryEntry{
			Kind: entry.Kind, User: mo.None[model.Message](), Model: mo.Some(cloneModelResponse(response)),
			ToolResult: mo.None[agent.ToolResult](),
		}, nil
	case agent.HistoryEntryToolResult:
		result, present := entry.ToolResult.Get()
		if !present {
			return agent.HistoryEntry{}, errors.New("tool-result history payload is missing")
		}
		return agent.HistoryEntry{
			Kind: entry.Kind, User: mo.None[model.Message](), Model: mo.None[model.Response](),
			ToolResult: mo.Some(cloneToolResult(result)),
		}, nil
	default:
		return agent.HistoryEntry{}, fmt.Errorf("unsupported history entry kind %d", entry.Kind)
	}
}

func cloneMessage(message model.Message) model.Message {
	message.Content = slices.Clone(message.Content)
	for index := range message.Content {
		message.Content[index].Data = message.Content[index].Data.MapValue(bytes.Clone)
	}
	return message
}

func cloneModelResponse(response model.Response) model.Response {
	content := slices.Clone(response.Content)
	for index := range content {
		cloneContext := func(value model.ProviderContext) model.ProviderContext {
			value.Payload = bytes.Clone(value.Payload)
			return value
		}
		content[index].ProviderContext = content[index].ProviderContext.MapValue(cloneContext)
		content[index].ToolCall = content[index].ToolCall.MapValue(func(value model.ToolCall) model.ToolCall {
			value.Arguments = cloneArguments(value.Arguments)
			return value
		})
	}
	return model.Response{
		Content: content, Outcome: response.Outcome, ErrorMessage: response.ErrorMessage,
		Provider: response.Provider, Model: response.Model, ResponseModel: response.ResponseModel,
		ResponseID: response.ResponseID, Usage: response.Usage, Diagnostics: slices.Clone(response.Diagnostics),
	}
}

func cloneArguments(arguments map[string]any) map[string]any {
	if arguments == nil {
		return nil
	}
	cloned := maps.Clone(arguments)
	for name, value := range cloned {
		cloned[name] = cloneJSONValue(value)
	}
	return cloned
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneArguments(typed)
	case []any:
		cloned := slices.Clone(typed)
		for index := range cloned {
			cloned[index] = cloneJSONValue(cloned[index])
		}
		return cloned
	default:
		return value
	}
}

func cloneToolResult(result agent.ToolResult) agent.ToolResult {
	contents := slices.Clone(result.Contents)
	for index := range contents {
		if image, present := contents[index].Image.Get(); present {
			image.Data = bytes.Clone(image.Data)
			contents[index].Image = mo.Some(image)
		}
	}
	return agent.ToolResult{
		CallID: result.CallID, ToolName: result.ToolName, Contents: contents, IsError: result.IsError,
	}
}

func publicUserText(message model.Message) string {
	var text strings.Builder
	for _, content := range message.Content {
		if content.Kind == model.InputContentText {
			if value, present := content.Text.Get(); present {
				text.WriteString(value)
			}
		}
	}
	return text.String()
}
