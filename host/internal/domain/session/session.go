// Package session defines provider-neutral session identity and lifecycle values.
package session

import (
	"errors"
	"time"

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

// EstimatedCost contains calculated USD cost for disjoint token buckets.
type EstimatedCost struct {
	Input      float64
	Output     float64
	CacheRead  float64
	CacheWrite float64
	Total      float64
}

// ProviderModelCost groups persisted cost by configured provider and requested model.
type ProviderModelCost struct {
	Provider      model.ProviderID
	Model         model.ID
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
	Info       Info
	Statistics Statistics
}
