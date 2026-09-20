// Package extension maps extension-initiated requests to session-bound Host operations.
package extension

import (
	"context"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock_test.go -package=extension

// ConfiguredRetryProgress reports one accepted configured-model replacement attempt.
type ConfiguredRetryProgress struct {
	// CompletedAttempts is the number of completed provider attempts.
	CompletedAttempts int64
	// AttemptLimit is the effective total attempt limit.
	AttemptLimit int64
	// Delay is the pending delay before replacement.
	Delay time.Duration
	// Error contains the complete failed-attempt text.
	Error string
}

// ModelOperations supplies extension-facing catalog reads and configured model requests.
type ModelOperations interface {
	// ReadModels returns a defensive provider-neutral catalog after binding revalidation.
	ReadModels(
		ctx context.Context,
		extensionID, runtimeID string,
		reference extensiondomain.ContextRef,
	) (ModelCatalog, error)
	// ReadProviders returns provider identifiers and ordered model identifiers after revalidation.
	ReadProviders(
		ctx context.Context,
		extensionID, runtimeID string,
		reference extensiondomain.ContextRef,
	) ([]Provider, error)
	// Request executes one explicit configured selection after binding revalidation.
	Request(
		ctx context.Context,
		extensionID, runtimeID string,
		reference extensiondomain.ContextRef,
		selection model.Selection,
		instructions string,
		history []agent.HistoryEntry,
		progress func(ConfiguredRetryProgress) error,
	) (model.Response, error)
}

// CompactionResult retains durable state across terminal observer or publication failures.
type CompactionResult struct {
	// Committed contains the durable compaction marker when persistence succeeded.
	Committed mo.Option[session.Entry]
	// Canceled reports explicit extension cancellation before commit.
	Canceled bool
}

// CompactionGate owns admission against active agent and session operations.
type CompactionGate interface {
	// TryAcquire reserves manual compaction without waiting.
	TryAcquire() (release func(), acquired bool)
}

// CompactionOperations executes session-bound manual compaction.
type CompactionOperations interface {
	// CompactExtension validates the binding and runs the shared chain with optional instructions.
	CompactExtension(
		context.Context,
		string,
		string,
		extensiondomain.ContextRef,
		mo.Option[string],
	) (CompactionResult, error)
}

// ContextOperations supplies binding and session operations without transport policy.
type ContextOperations interface {
	// ValidateContext rejects a reference not issued to the connected runtime or no longer active.
	ValidateContext(extensionID, runtimeID string, reference extensiondomain.ContextRef) error
	// AppendExtension persists one hidden entry under the bound session incarnation.
	AppendExtension(
		ctx context.Context,
		extensionID, runtimeID string,
		reference extensiondomain.ContextRef,
		entryType string,
		data []byte,
	) (session.Entry, error)
	// AppendExtensionMessage persists one model-visible message and reports post-commit delivery issues.
	AppendExtensionMessage(
		ctx context.Context,
		extensionID, runtimeID string,
		reference extensiondomain.ContextRef,
		entryType, text string,
		visibility session.ClientVisibility,
	) (AppendMessageResult, error)
	// ReadSessionState returns one coherent caller-filtered active-branch snapshot.
	ReadSessionState(
		ctx context.Context,
		extensionID, runtimeID string,
		reference extensiondomain.ContextRef,
	) (SessionState, error)
}

// AppendMessageResult contains one committed entry and nonterminal post-commit issues.
type AppendMessageResult struct {
	// Entry is the committed message entry.
	Entry session.Entry
	// Issues contains ordered post-commit issues.
	Issues []OperationIssue
}

// OperationIssue contains one stable code and complete diagnostic text.
type OperationIssue struct {
	// ExtensionID identifies the extension that owns the failed append.
	ExtensionID string
	// Code identifies the nonterminal outcome.
	Code string
	// Message contains complete issue text including the original cause.
	Message string
}

// SessionState contains one coherent recovery result at the controller consumer boundary.
type SessionState struct {
	// SessionID identifies the bound durable session.
	SessionID session.ID
	// ActiveLeafID identifies the snapshot leaf when present.
	ActiveLeafID mo.Option[string]
	// Entries contains the caller extension's root-first active-branch entries.
	Entries []session.Entry
}

// ModelCatalog contains descriptors and active selection for one completed read.
type ModelCatalog struct {
	// Models contains complete provider-neutral descriptors in configured order.
	Models []model.Descriptor
	// Selection contains the active provider, model, and reasoning choice.
	Selection model.Selection
}

// Provider contains the public projection of one configured provider.
type Provider struct {
	// ID identifies the configured provider.
	ID model.ProviderID
	// ModelIDs contains model identifiers in configured order.
	ModelIDs []model.ID
}

// SelectionCommandKind identifies one extension-initiated selection request shape.
type SelectionCommandKind uint8

const (
	// SelectionCommandModel identifies a provider and model request.
	SelectionCommandModel SelectionCommandKind = iota + 1
	// SelectionCommandReasoning identifies a reasoning-choice request.
	SelectionCommandReasoning
)

// SelectionCommand carries one validated selection request and its issued binding.
type SelectionCommand struct {
	// Kind identifies the request shape.
	Kind SelectionCommandKind
	// ExtensionID identifies the extension that owns the request.
	ExtensionID string
	// RuntimeID identifies the exact connected runtime incarnation.
	RuntimeID string
	// Context identifies the issued runtime-to-session binding.
	Context extensiondomain.ContextRef
	// Provider identifies the requested provider for a model request.
	Provider model.ProviderID
	// Model identifies the requested model for a model request.
	Model model.ID
	// ReasoningChoice identifies the requested reasoning choice for a reasoning request.
	ReasoningChoice model.ReasoningChoice
}

// SelectionIssue contains one ordered nonterminal diagnostic.
type SelectionIssue struct {
	// ExtensionID identifies the extension that produced the issue.
	ExtensionID string
	// HandlerID identifies the handler that produced the issue.
	HandlerID string
	// Code identifies the stable issue category.
	Code string
	// Message contains complete diagnostic text.
	Message string
}

// SelectionResult contains the committed selection and ordered diagnostics.
type SelectionResult struct {
	// Selection is the committed full selection when Committed is true.
	Selection model.Selection
	// Committed reports whether authoritative selection state changed or was confirmed.
	Committed bool
	// Issues contains ordered nonterminal diagnostics.
	Issues []SelectionIssue
	// Source preserves complete diagnostic or terminal failure causes.
	Source error
}

// PreparedSelection owns one accepted extension selection operation.
type PreparedSelection interface {
	// Run executes handler composition, validation, protected commit, and publication.
	Run(context.Context) SelectionResult
	// Release releases shared selection admission exactly once.
	Release()
}

// ModelSelection prepares extension selections through the shared selection owner.
type ModelSelection interface {
	// PrepareExtensionSelection validates the starting target and reserves shared admission.
	PrepareExtensionSelection(SelectionCommand) (PreparedSelection, error)
}

// SelectionFailure exposes the stable selection failure category.
type SelectionFailure interface {
	error
	// ModelSelectionCode returns the category without replacing complete error text.
	ModelSelectionCode() string
}

// ModelFailure exposes the closed extension model-operation failure category.
type ModelFailure interface {
	error
	// ModelCode returns the category without replacing the complete error text.
	ModelCode() string
}

// ContextFailure exposes the closed context-operation failure category.
type ContextFailure interface {
	error
	// ContextCode returns the category without replacing the complete error text.
	ContextCode() string
}

// RuntimeOperations accounts for extension-initiated work against the connected process instance.
type RuntimeOperations interface {
	// BeginContextOperation reserves one active operation and returns its release function.
	BeginContextOperation(ctx context.Context, extensionID, runtimeID string) (func(), error)
}
