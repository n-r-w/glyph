// Package session defines provider-neutral session identity and lifecycle values.
package session

import (
	"errors"
	"math"
	"time"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// ID is the opaque public identifier of one session.
type ID string

var (
	// ErrBusy reports that agent execution or another replacement owns the operation gate.
	ErrBusy = errors.New("another operation is active")
	// ErrInvalidName reports a name that is empty after normalization.
	ErrInvalidName = errors.New("session name is required")
	// ErrUnavailable reports a stored session that cannot be validated or recovered.
	ErrUnavailable = errors.New("session is unavailable")
	// ErrPersistenceUnavailable reports an active session that cannot accept mutations.
	ErrPersistenceUnavailable = errors.New("session persistence failed")
	// ErrEntryNotFound reports an unknown session-tree entry target.
	ErrEntryNotFound = errors.New("session tree entry not found")
	// ErrInvalidForkTarget reports a fork target that is not a user message.
	ErrInvalidForkTarget = errors.New("fork target must be a user message")
)

// Header is the first record in a persisted session.
type Header struct {
	// Version selects the persisted record schema.
	Version int
	// ID identifies the session independently of its storage path.
	ID ID
	// CreatedAt fixes creation time for ordering and file naming.
	CreatedAt time.Time
	// WorkingDirectory binds the session to one canonical project path.
	WorkingDirectory string
}

// Information is a stored session name change.
type Information struct {
	// Name is the normalized user-assigned session name.
	Name string
}

// UserMessage is the provider-neutral user content stored in a session.
type UserMessage = model.Message

// ModelResponse is the provider-neutral terminal model response stored in a session.
type ModelResponse = model.Response

// ToolResult is the provider-neutral terminal tool result stored in a session.
type ToolResult = agent.ToolResult

// ExtensionEnvelope stores one extension-owned opaque JSON value.
type ExtensionEnvelope struct {
	// ExtensionID identifies the extension that owns the entry.
	ExtensionID string
	// EntryType identifies the extension-defined entry kind.
	EntryType string
	// Data contains an owned JSON value without exposing its storage representation.
	Data []byte
}

// Entry is one ordered session record.
type Entry struct {
	// ID uniquely identifies this record within the session.
	ID string
	// ParentID identifies the preceding tree entry or the implicit root when absent.
	ParentID mo.Option[string]
	// CreatedAt determines the record update time without filesystem metadata.
	CreatedAt time.Time
	// Information contains the name change carried by this lifecycle entry.
	Information mo.Option[Information]
	// User contains one terminal user message.
	User mo.Option[UserMessage]
	// Model contains one terminal model response.
	Model mo.Option[ModelResponse]
	// EstimatedCost contains the calculated cost persisted with a model response.
	EstimatedCost mo.Option[EstimatedCost]
	// ToolResult contains one terminal tool execution result.
	ToolResult mo.Option[ToolResult]
	// Extension contains one session-only extension entry.
	Extension mo.Option[ExtensionEnvelope]
	// BranchSummary contains one persisted abandoned-branch summary.
	BranchSummary mo.Option[BranchSummaryEntry]
}

// BranchSummaryEntry stores summary text, its branch boundary, and the actual result source.
type BranchSummaryEntry struct {
	// Summary contains abandoned-branch context supplied by its source.
	Summary string
	// FirstEntryID identifies the first abandoned entry.
	FirstEntryID string
	// LastEntryID identifies the last abandoned entry.
	LastEntryID string
	// Source identifies the actual producer and its model usage.
	Source BranchSummarySource
	// EstimatedCost contains persisted summary cost when calculable.
	EstimatedCost mo.Option[EstimatedCost]
}

// Replacement is one atomic active-session identity and durable transcript snapshot.
type Replacement struct {
	// Info identifies the committed active session.
	Info Info
	// Entries contains cloned durable entries from the same committed state.
	Entries []Entry
}

// Info describes one active or persisted session.
type Info struct {
	// ID is the opaque client-visible session identifier.
	ID ID
	// Name is absent until the user assigns a name.
	Name mo.Option[string]
	// WorkingDirectory is the canonical project path stored in the header.
	WorkingDirectory string
	// StoragePath is absent until the first entry creates the JSONL file.
	StoragePath mo.Option[string]
	// CreatedAt comes from the immutable session header.
	CreatedAt time.Time
	// UpdatedAt is the latest entry time, or CreatedAt for an empty session.
	UpdatedAt time.Time
}

// Summary describes one session in a client list.
type Summary struct {
	// Info contains identity and lifecycle timestamps.
	Info Info
	// FirstUserText is absent when the session contains no user content.
	FirstUserText mo.Option[string]
	// TotalMessages counts client-visible terminal messages.
	TotalMessages int
}

// TokenUsage contains disjoint normalized token buckets and their derived total.
type TokenUsage struct {
	// InputTokens excludes cache-read and cache-write tokens.
	InputTokens int64
	// OutputTokens includes the reasoning-token subset.
	OutputTokens int64
	// CacheReadTokens contains provider-reported cached input.
	CacheReadTokens int64
	// CacheWriteTokens contains provider-reported cache creation input.
	CacheWriteTokens int64
	// ReasoningTokens is a subset of OutputTokens.
	ReasoningTokens int64
	// TotalTokens is derived without adding ReasoningTokens again.
	TotalTokens int64
}

// Valid reports whether all token buckets and their derived total satisfy session invariants.
func (usage TokenUsage) Valid() bool {
	if usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.CacheReadTokens < 0 ||
		usage.CacheWriteTokens < 0 || usage.ReasoningTokens < 0 || usage.TotalTokens < 0 {
		return false
	}
	return usage.ReasoningTokens <= usage.OutputTokens &&
		usage.TotalTokens == usage.InputTokens+usage.OutputTokens+usage.CacheReadTokens+usage.CacheWriteTokens
}

// Add returns the component-wise sum of two token-usage values.
func (usage TokenUsage) Add(other TokenUsage) TokenUsage {
	return TokenUsage{
		InputTokens: usage.InputTokens + other.InputTokens, OutputTokens: usage.OutputTokens + other.OutputTokens,
		CacheReadTokens:  usage.CacheReadTokens + other.CacheReadTokens,
		CacheWriteTokens: usage.CacheWriteTokens + other.CacheWriteTokens,
		ReasoningTokens:  usage.ReasoningTokens + other.ReasoningTokens,
		TotalTokens:      usage.TotalTokens + other.TotalTokens,
	}
}

// EstimatedCost contains calculated USD cost for disjoint token buckets.
type EstimatedCost struct {
	// Input is the cost of uncached input tokens.
	Input float64
	// Output is the cost of output tokens.
	Output float64
	// CacheRead is the cost of cached input tokens.
	CacheRead float64
	// CacheWrite is the cost of cache creation input tokens.
	CacheWrite float64
	// Total is the sum of all cost buckets.
	Total float64
}

// Valid reports whether all cost buckets and their derived total satisfy session invariants.
func (cost EstimatedCost) Valid() bool {
	values := []float64{cost.Input, cost.Output, cost.CacheRead, cost.CacheWrite, cost.Total}
	// validBuckets rejects invalid provider amounts before checking the derived total.
	validBuckets := lo.EveryBy(values, func(value float64) bool {
		return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
	})
	return validBuckets && cost.Total == cost.Input+cost.Output+cost.CacheRead+cost.CacheWrite
}

// Add returns the component-wise sum of two estimated-cost values.
func (cost EstimatedCost) Add(other EstimatedCost) EstimatedCost {
	return EstimatedCost{
		Input: cost.Input + other.Input, Output: cost.Output + other.Output,
		CacheRead: cost.CacheRead + other.CacheRead, CacheWrite: cost.CacheWrite + other.CacheWrite,
		Total: cost.Total + other.Total,
	}
}

// ProviderModelCost groups persisted cost by configured provider and requested model.
type ProviderModelCost struct {
	// Provider identifies the configured model provider.
	Provider model.ProviderID
	// Model identifies the requested provider model.
	Model model.ID
	// EstimatedCost contains the complete persisted cost for this group.
	EstimatedCost mo.Option[EstimatedCost]
}

// Statistics contains counts and optional complete token and cost totals.
type Statistics struct {
	// UserMessages counts durable user entries.
	UserMessages int
	// ModelResponses counts every durable terminal model entry, including failures.
	ModelResponses int
	// ToolCalls counts finalized calls inside durable model responses.
	ToolCalls int
	// ToolResults counts durable tool-result entries.
	ToolResults int
	// TotalMessages is user messages plus model responses plus tool results.
	TotalMessages int
	// TokenUsage is available only when every durable model response has usage.
	TokenUsage mo.Option[TokenUsage]
	// EstimatedCost is available only when every durable model response has persisted cost.
	EstimatedCost mo.Option[EstimatedCost]
	// CostBreakdown groups persisted cost by configured provider and requested model.
	CostBreakdown []ProviderModelCost
}

// InformationSnapshot contains coherent active-session metadata and accounting.
type InformationSnapshot struct {
	// Info contains active-session metadata.
	Info Info
	// Statistics contains accounting from the same session state.
	Statistics Statistics
}
