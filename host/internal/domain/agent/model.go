// Package agent defines provider-neutral Agent Core history and run values.
package agent

import (
	"bytes"
	"errors"
	"fmt"
	"slices"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// HistoryEntryKind identifies one linear history entry.
type HistoryEntryKind uint8

const (
	// HistoryEntryUser is one user-authored message.
	HistoryEntryUser HistoryEntryKind = iota + 1
	// HistoryEntryModel is one finalized model response.
	HistoryEntryModel
	// HistoryEntryToolResult is one completed tool result.
	HistoryEntryToolResult
)

// HistoryEntry is one ordered session-history item.
type HistoryEntry struct {
	// Kind identifies the entry payload.
	Kind HistoryEntryKind
	// User contains a user message when Kind is HistoryEntryUser.
	User mo.Option[model.Message]
	// Model contains a model response when Kind is HistoryEntryModel.
	Model mo.Option[model.Response]
	// ToolResult contains a tool result when Kind is HistoryEntryToolResult.
	ToolResult mo.Option[ToolResult]
}

// Clone returns a deep copy of the history entry.
func (entry HistoryEntry) Clone() HistoryEntry {
	entry.User = entry.User.MapValue(model.Message.Clone)
	entry.Model = entry.Model.MapValue(model.Response.Clone)
	entry.ToolResult = entry.ToolResult.MapValue(ToolResult.Clone)
	return entry
}

// ValidatedClone returns a deep copy after validating the selected payload.
func (entry HistoryEntry) ValidatedClone() (HistoryEntry, error) {
	switch entry.Kind {
	case HistoryEntryUser:
		message, present := entry.User.Get()
		if !present {
			return HistoryEntry{}, errors.New("user history payload is missing")
		}
		return HistoryEntry{
			Kind: entry.Kind, User: mo.Some(message.Clone()), Model: mo.None[model.Response](),
			ToolResult: mo.None[ToolResult](),
		}, nil
	case HistoryEntryModel:
		response, present := entry.Model.Get()
		if !present {
			return HistoryEntry{}, errors.New("model history payload is missing")
		}
		return HistoryEntry{
			Kind: entry.Kind, User: mo.None[model.Message](), Model: mo.Some(response.Clone()),
			ToolResult: mo.None[ToolResult](),
		}, nil
	case HistoryEntryToolResult:
		result, present := entry.ToolResult.Get()
		if !present {
			return HistoryEntry{}, errors.New("tool-result history payload is missing")
		}
		return HistoryEntry{
			Kind: entry.Kind, User: mo.None[model.Message](), Model: mo.None[model.Response](),
			ToolResult: mo.Some(result.Clone()),
		}, nil
	default:
		return HistoryEntry{}, fmt.Errorf("unsupported history entry kind %d", entry.Kind)
	}
}

// ToolResult is one model-visible terminal tool result.
type ToolResult struct {
	// CallID identifies the model-requested tool call.
	CallID string
	// ToolName identifies the executed tool.
	ToolName string
	// Contents contains ordered model-visible result blocks.
	Contents []tool.ResultContent
	// IsError reports whether tool execution failed.
	IsError bool
}

// Clone returns a deep copy of the tool result.
func (result ToolResult) Clone() ToolResult {
	result.Contents = slices.Clone(result.Contents)
	for index := range result.Contents {
		result.Contents[index].Image = result.Contents[index].Image.MapValue(func(image tool.ResultImage) tool.ResultImage {
			image.Data = bytes.Clone(image.Data)
			return image
		})
	}
	return result
}

// RunOutcome identifies the terminal Agent Core run state.
type RunOutcome uint8

const (
	// RunOutcomeCompleted ended through a final model outcome.
	RunOutcomeCompleted RunOutcome = iota + 1
	// RunOutcomeAborted ended through cancellation.
	RunOutcomeAborted
	// RunOutcomeFailed ended through provider, tool, or delivery failure.
	RunOutcomeFailed
)
