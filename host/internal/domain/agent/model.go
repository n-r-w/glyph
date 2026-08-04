// Package agent defines provider-neutral Agent Core history and run values.
package agent

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
	Kind       HistoryEntryKind
	User       UserMessage
	Model      ModelResponse
	ToolResult ToolResult
}

// UserMessage is one user-authored text request.
type UserMessage struct {
	Text string
}

// ModelOutcome identifies why one model response ended.
type ModelOutcome uint8

const (
	// ModelOutcomeStop is a final response without automatic work.
	ModelOutcomeStop ModelOutcome = iota + 1
	// ModelOutcomeToolUse requests finalized tool calls.
	ModelOutcomeToolUse
	// ModelOutcomeLength reached the provider response limit.
	ModelOutcomeLength
	// ModelOutcomeAborted records provider cancellation.
	ModelOutcomeAborted
	// ModelOutcomeFailed records provider failure.
	ModelOutcomeFailed
)

// ModelItemKind identifies one ordered model-response content item.
type ModelItemKind uint8

const (
	// ModelItemText contains finalized model text.
	ModelItemText ModelItemKind = iota + 1
	// ModelItemProviderContext contains opaque provider-owned bytes.
	ModelItemProviderContext
	// ModelItemToolCall contains one provider-neutral tool request.
	ModelItemToolCall
)

// ModelItem is one ordered content item in a model response.
type ModelItem struct {
	Kind            ModelItemKind
	Text            string
	ProviderContext ProviderContext
	ToolCall        ToolCall
}

// ProviderContext preserves provider-owned bytes without interpretation.
type ProviderContext struct {
	ProviderID string
	Payload    []byte
}

// ToolCall is one provider-neutral model-requested tool invocation.
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

// ModelResponse is one finalized ordered model response.
type ModelResponse struct {
	Items        []ModelItem
	Outcome      ModelOutcome
	ErrorMessage string
}

// ToolResult is one model-visible terminal tool result.
type ToolResult struct {
	CallID   string
	ToolName string
	Content  string
	IsError  bool
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
