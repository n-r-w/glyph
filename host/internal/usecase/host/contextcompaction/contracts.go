package contextcompaction

//go:generate go tool mockgen -source=contracts.go -destination=contracts_mock.go -package=contextcompaction

import (
	"context"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// FailureCategory identifies one terminal compaction failure.
type FailureCategory string

const (
	// FailureCompactionFailed identifies Host validation or orchestration failure.
	FailureCompactionFailed FailureCategory = "COMPACTION_FAILED"
	// FailureExtensionFailed identifies handler invocation, action, or observer failure.
	FailureExtensionFailed FailureCategory = "EXTENSION_FAILED"
	// FailurePersistenceUnavailable identifies a compaction commit persistence failure.
	FailurePersistenceUnavailable FailureCategory = "PERSISTENCE_UNAVAILABLE"
	// FailureInternal identifies a post-commit publication failure outside compaction policy.
	FailureInternal FailureCategory = "INTERNAL"
)

// FailureError preserves one stable public category and the complete cause.
type FailureError struct {
	// Category identifies the public terminal outcome.
	Category FailureCategory
	// Cause preserves every contributing error.
	Cause error
}

var _ modelexecution.ContextPreparationFailure = (*FailureError)(nil)

// Error returns complete contributing cause text.
func (failure *FailureError) Error() string { return failure.Cause.Error() }

// Unwrap exposes the complete cause.
func (failure *FailureError) Unwrap() error { return failure.Cause }

// CompactionFailureCode returns the stable public category to direct and automatic consumers.
func (failure *FailureError) CompactionFailureCode() string { return string(failure.Category) }

// Trigger identifies why Host started one active-conversation compaction.
type Trigger uint8

const (
	// TriggerManual identifies a user- or extension-requested compaction.
	TriggerManual Trigger = iota + 1
	// TriggerThreshold identifies preparation above the estimated input budget.
	TriggerThreshold
	// TriggerOverflow identifies recovery after a provider context rejection.
	TriggerOverflow
)

// Snapshot is the immutable active-session state captured for one compaction.
type Snapshot struct {
	// Identity identifies the exact active-session incarnation.
	Identity session.Identity
	// ActiveLeafID identifies the captured active branch tip.
	ActiveLeafID mo.Option[string]
	// Entries contains the captured active branch in root-first order.
	Entries []session.Entry
	// Context contains the exact current model-visible projection.
	Context []agent.HistoryEntry
	// Previous contains the latest persisted compaction when present.
	Previous mo.Option[session.CompactionEntry]
}

// InputEntry contains one projected branch entry and its Host fallback estimate.
type InputEntry struct {
	// Entry is the embedded provider-neutral branch projection.
	session.Entry
	// EstimatedTokens is the Host-computed fallback estimate for this entry.
	EstimatedTokens int64
}

// Request is the complete extension-visible state for one compaction attempt.
type Request struct {
	// Trigger identifies the operation entry point.
	Trigger Trigger
	// RetryIntent reports whether success permits one overflow replacement attempt.
	RetryIntent bool
	// Instructions contains optional manual guidance.
	Instructions mo.Option[string]
	// Model contains the captured agent model descriptor.
	Model model.Descriptor
	// ReasoningChoice contains the captured agent reasoning selection.
	ReasoningChoice model.ReasoningChoice
	// Prefix contains the proposed newly summarized branch entries with fallback estimates.
	Prefix []InputEntry
	// Suffix contains the proposed preserved branch entries with fallback estimates.
	Suffix []InputEntry
	// Previous contains the preceding summary marker when present.
	Previous mo.Option[session.CompactionEntry]
	// ContextTokens is the estimate for the captured outbound request.
	ContextTokens int64
	// ContextTokensEstimated is required and identifies ContextTokens as an estimate.
	ContextTokensEstimated bool
	// ContextWindow is the captured model context capacity.
	ContextWindow int64
	// ResponseBudget is the captured generated-output reserve.
	ResponseBudget int64
	// RetainedBudget is the configured recent-context target.
	RetainedBudget int64
}

// Result is one complete compaction result supplied by an extension.
type Result struct {
	// Summary contains replacement context for the removed prefix.
	Summary string
	// FirstKeptEntryID identifies the first original entry in the preserved suffix.
	FirstKeptEntryID string
	// Source identifies the actual producer and optional model usage.
	Source session.CompactionSource
	// Details contains optional opaque extension-owned state.
	Details mo.Option[[]byte]
}

// RequestActionKind identifies how one request handler changes the current request.
type RequestActionKind uint8

const (
	// RequestActionPreserve keeps the current request.
	RequestActionPreserve RequestActionKind = iota + 1
	// RequestActionReplace replaces the complete current request.
	RequestActionReplace
)

// ResultActionKind identifies how one request handler changes optional result state.
type ResultActionKind uint8

const (
	// ResultActionPreserve keeps the current optional result.
	ResultActionPreserve ResultActionKind = iota + 1
	// ResultActionReplace sets or replaces the current result.
	ResultActionReplace
	// ResultActionClear removes the current result.
	ResultActionClear
)

// RequestAction is one validated request-handler transition.
type RequestAction struct {
	// Cancel exclusively terminates compaction without a commit.
	Cancel bool
	// RequestAction identifies request preservation or replacement.
	RequestAction RequestActionKind
	// Request contains the replacement only for RequestActionReplace.
	Request mo.Option[Request]
	// ResultAction identifies optional-result preservation, replacement, or clearing.
	ResultAction ResultActionKind
	// Result contains the replacement only for ResultActionReplace.
	Result mo.Option[Result]
}

// ResultAction is one validated result-handler transition.
type ResultAction struct {
	// Cancel exclusively terminates compaction without a commit.
	Cancel bool
	// Preserve keeps current output; false requires Result.
	Preserve bool
	// Result contains replacement output when Preserve is false.
	Result mo.Option[Result]
}

// Handler identifies one exact snapshotted runtime capability.
type Handler struct {
	// ExtensionID identifies the owning extension.
	ExtensionID string
	// RuntimeID identifies the exact accepted runtime generation.
	RuntimeID string
	// HandlerID identifies the extension-local capability.
	HandlerID string
}

// HandlerSet contains one operation's immutable capability membership and order.
type HandlerSet struct {
	// Requests contains request handlers in registration order.
	Requests []Handler
	// Generators contains registered replacement implementations.
	Generators []Handler
	// Results contains result handlers in registration order.
	Results []Handler
	// Successes contains post-commit success observers in registration order.
	Successes []Handler
	// Failures contains terminal failure observers in registration order.
	Failures []Handler
}

// RequestInvocation carries immutable original and composed current state.
type RequestInvocation struct {
	// Original is the immutable Host-produced request.
	Original Request
	// Current is the request left by preceding handlers.
	Current Request
	// CurrentResult is the result left by preceding handlers when present.
	CurrentResult mo.Option[Result]
}

// ResultInvocation carries immutable request and result states.
type ResultInvocation struct {
	// OriginalRequest is the immutable Host-produced request.
	OriginalRequest Request
	// CurrentRequest is the final request-handler state.
	CurrentRequest Request
	// OriginalResult is the immutable result entering result handling.
	OriginalResult Result
	// CurrentResult is the result left by preceding result handlers.
	CurrentResult Result
}

// OutcomeInvocation reports one terminal compaction outcome.
type OutcomeInvocation struct {
	// OriginalRequest is the immutable Host-produced request.
	OriginalRequest Request
	// CurrentRequest is the final valid request state.
	CurrentRequest Request
	// Result contains final output when one was validated.
	Result mo.Option[Result]
	// Committed contains durable state when persistence succeeded.
	Committed mo.Option[session.Entry]
	// Canceled reports explicit handler cancellation.
	Canceled bool
	// Error contains complete terminal failure text when present.
	Error mo.Option[string]
}

// Runtime snapshots and invokes public compaction capabilities.
type Runtime interface {
	// SnapshotCompactionHandlers captures membership and order for one operation.
	SnapshotCompactionHandlers() HandlerSet
	// HandleCompactionRequest invokes one request handler from the snapshot.
	HandleCompactionRequest(context.Context, Handler, extension.Context, RequestInvocation) (RequestAction, error)
	// GenerateCompaction invokes one registered generation implementation.
	GenerateCompaction(context.Context, Handler, extension.Context, RequestInvocation) (Result, error)
	// HandleCompactionResult invokes one result handler from the snapshot.
	HandleCompactionResult(context.Context, Handler, extension.Context, ResultInvocation) (ResultAction, error)
	// ObserveCompactionSuccess invokes one post-commit success observer.
	ObserveCompactionSuccess(context.Context, Handler, extension.Context, OutcomeInvocation) error
	// ObserveCompactionFailure invokes one terminal failure observer.
	ObserveCompactionFailure(context.Context, Handler, extension.Context, OutcomeInvocation) error
}

// ContextIssuer supplies session-bound extension contexts.
type ContextIssuer interface {
	// IssueContext returns one binding for the extension and current active session.
	IssueContext(extensionID string) (extension.Context, error)
	// ValidateContext rejects a reference not issued to the runtime or no longer active.
	ValidateContext(extensionID, runtimeID string, reference extension.ContextRef) error
}

// OperationResult retains durable state even when a post-commit observer fails.
type OperationResult struct {
	// Committed contains the durable compaction entry when persistence succeeded.
	Committed mo.Option[session.Entry]
	// Canceled reports explicit handler cancellation before commit.
	Canceled bool
}

// ManualModelSnapshot contains the model choices captured for one manual compaction.
type ManualModelSnapshot struct {
	// Model contains the active model descriptor.
	Model model.Descriptor
	// ReasoningChoice contains the active reasoning selection.
	ReasoningChoice model.ReasoningChoice
}

// ManualModel supplies the current active model snapshot for manual compaction.
type ManualModel interface {
	// ManualCompactionModel returns one atomic model descriptor and reasoning snapshot.
	ManualCompactionModel() ManualModelSnapshot
}

// ManualTools supplies the current available tool catalog for manual compaction.
type ManualTools interface {
	// Tools returns detached available descriptors in stable order.
	Tools() []tool.Descriptor
}
