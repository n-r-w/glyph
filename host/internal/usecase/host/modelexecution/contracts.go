// Package modelexecution owns Host logical model execution and raw provider attempts.
package modelexecution

import (
	"context"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
)

//go:generate go tool mockgen -source=contracts.go -destination=contracts_mock.go -package=modelexecution

// ProviderRequest contains one immutable raw provider-attempt request.
type ProviderRequest struct {
	// Instructions contains the system prompt.
	Instructions string
	// Model contains the immutable configured model snapshot.
	Model model.Descriptor
	// ReasoningChoice identifies the selected reasoning behavior.
	ReasoningChoice model.ReasoningChoice
	// History contains owned provider-neutral conversation history.
	History []agent.HistoryEntry
	// Tools contains the available model-callable tool catalog.
	Tools []tool.Descriptor
}

// StreamEventKind identifies one raw provider stream transition.
type StreamEventKind uint8

const (
	// StreamEventContentStart starts one text content block.
	StreamEventContentStart StreamEventKind = iota + 1
	// StreamEventTextDelta appends text to an active content block.
	StreamEventTextDelta
	// StreamEventContentEnd finalizes one text content block.
	StreamEventContentEnd
	// StreamEventToolCallStart starts one provisional function call.
	StreamEventToolCallStart
	// StreamEventToolCallDelta replaces one provisional function-call preview.
	StreamEventToolCallDelta
	// StreamEventToolCallEnd carries exact final function-call arguments.
	StreamEventToolCallEnd
	// StreamEventDone carries one successful terminal response.
	StreamEventDone
	// StreamEventError carries one failed or aborted terminal response.
	StreamEventError
)

// StreamEvent is one ordered raw provider stream transition.
type StreamEvent struct {
	// Kind identifies the stream transition and active payload.
	Kind StreamEventKind
	// Position identifies the response content block order.
	Position mo.Option[int]
	// Content contains one typed model content block.
	Content mo.Option[model.Content]
	// Delta contains an exact model text fragment.
	Delta mo.Option[string]
	// Preview contains provisional tool call state.
	Preview mo.Option[model.ToolCallPreview]
	// ToolCall contains one finalized tool request.
	ToolCall mo.Option[model.ToolCall]
	// Response contains the authoritative terminal response.
	Response mo.Option[model.Response]
}

// StreamHandler consumes raw provider stream transitions in provider order.
type StreamHandler func(event StreamEvent) error

// RetryPolicy is one immutable logical-execution retry configuration.
type RetryPolicy struct {
	// Enabled reports whether provider failures can be repeated.
	Enabled bool
	// MaxRetries is the maximum number of repeats after the initial attempt.
	MaxRetries int64
	// Delays contains the ordered delay before each repeat attempt.
	Delays []time.Duration
	// MaxProviderDelay is the largest accepted provider-requested minimum delay.
	MaxProviderDelay time.Duration
}

// RetryDecision is the complete retry state composed for one failed attempt.
type RetryDecision struct {
	// Retryable reports whether the source can be repeated unchanged.
	Retryable bool
	// Retry reports whether another attempt is requested.
	Retry bool
	// Delay is the pending delay before another attempt.
	Delay time.Duration
	// AttemptLimit is the effective total attempt limit including the initial attempt.
	AttemptLimit int64
}

// RetryHandler identifies one snapshotted extension retry handler.
type RetryHandler struct {
	// ExtensionID identifies the extension that owns the handler.
	ExtensionID string
	// RuntimeID identifies the exact registered runtime instance.
	RuntimeID string
	// HandlerID identifies the extension-local handler.
	HandlerID string
}

// RetryInvocation contains immutable original and composed current retry decisions.
type RetryInvocation struct {
	// SourceError contains the complete failed-attempt error text.
	SourceError string
	// Classification identifies the source-owned provider failure class.
	Classification ProviderFailureClassification
	// Original is the immutable Host-produced retry decision.
	Original RetryDecision
	// Current is the retry decision left by preceding handlers.
	Current RetryDecision
	// CompletedAttempts is the number of completed provider attempts.
	CompletedAttempts int64
	// ProviderDelay contains the provider-requested minimum delay when supplied.
	ProviderDelay mo.Option[time.Duration]
}

// RetryActionKind identifies one explicit retry-handler result.
type RetryActionKind uint8

const (
	// RetryActionPreserve keeps the current retry decision.
	RetryActionPreserve RetryActionKind = iota + 1
	// RetryActionReplace replaces the complete current retry decision.
	RetryActionReplace
	// RetryActionCancel terminates retry coordination without another attempt.
	RetryActionCancel
)

// RetryAction contains one retry-handler result.
type RetryAction struct {
	// Kind identifies the active action variant.
	Kind RetryActionKind
	// Decision contains the replacement decision only for RetryActionReplace.
	Decision mo.Option[RetryDecision]
}

// RetryProgress reports one accepted retry before its replacement attempt.
type RetryProgress struct {
	// CompletedAttempts is the number of completed provider attempts.
	CompletedAttempts int64
	// AttemptLimit is the effective total attempt limit.
	AttemptLimit int64
	// Delay is the pending delay before the replacement attempt.
	Delay time.Duration
	// Error contains the complete failed-attempt text.
	Error string
}

// retryProgressHandler consumes ordered retry progress for one owning Host path.
type retryProgressHandler func(progress RetryProgress) error

// RetryOutput delivers agent-request retry progress through the active Host mode.
type RetryOutput interface {
	// DeliverRetry publishes one accepted retry with mode-owned client correlation.
	DeliverRetry(context.Context, RetryProgress) error
}

// RetryHandlers snapshots and invokes public extension retry handlers.
type RetryHandlers interface {
	// SnapshotRetryHandlers returns registration-order handler identities for one logical execution.
	SnapshotRetryHandlers() []RetryHandler
	// HandleRetry invokes one handler from the captured runtime snapshot.
	HandleRetry(ctx context.Context, handler RetryHandler, invocation RetryInvocation) (RetryAction, error)
}

// FailureCategory identifies one terminal logical model-execution outcome.
type FailureCategory string

const (
	// FailureModelFailed identifies a non-retryable provider failure.
	FailureModelFailed FailureCategory = "MODEL_FAILED"
	// FailureRetryExhausted identifies a retryable failure with no attempts left.
	FailureRetryExhausted FailureCategory = "RETRY_EXHAUSTED"
	// FailureRetryCanceled identifies explicit retry-handler cancellation.
	FailureRetryCanceled FailureCategory = "RETRY_CANCELED"
	// FailureExtensionFailed identifies retry-handler invocation or action failure.
	FailureExtensionFailed FailureCategory = "EXTENSION_FAILED"
	// FailureRetryDelayExceeded identifies a provider delay above the accepted maximum.
	FailureRetryDelayExceeded FailureCategory = "RETRY_DELAY_EXCEEDED"
	// FailureContextLimit identifies provider context overflow after recovery cannot continue.
	FailureContextLimit FailureCategory = "CONTEXT_LIMIT"
	// FailureCompactionFailed identifies active-conversation preparation or compaction failure.
	FailureCompactionFailed FailureCategory = "COMPACTION_FAILED"
	// FailurePersistenceUnavailable identifies an unavailable durable session store during compaction.
	FailurePersistenceUnavailable FailureCategory = "PERSISTENCE_UNAVAILABLE"
	// FailureInternal identifies an acquired failure without a more specific logical category.
	FailureInternal FailureCategory = "INTERNAL"
)

// LogicalFailureError preserves one terminal category and every contributing cause.
type LogicalFailureError struct {
	// Category identifies the public provider-neutral terminal outcome.
	Category FailureCategory
	// Cause preserves all contributing errors.
	Cause error
}

var _ sessiontree.ModelRequestFailure = (*LogicalFailureError)(nil)

// Error returns the complete contributing cause text.
func (failure *LogicalFailureError) Error() string { return failure.Cause.Error() }

// Unwrap exposes contributing causes without weakening the logical category.
func (failure *LogicalFailureError) Unwrap() error { return failure.Cause }

// FailureCode returns the stable public terminal category.
func (failure *LogicalFailureError) FailureCode() string { return string(failure.Category) }

// ProviderFailureClassification identifies why one provider-owned attempt failed.
type ProviderFailureClassification uint8

const (
	// ProviderFailureTransient identifies a provider failure that may succeed unchanged later.
	ProviderFailureTransient ProviderFailureClassification = iota + 1
	// ProviderFailureNonRetryable identifies a provider rejection that must not be retried unchanged.
	ProviderFailureNonRetryable
	// ProviderFailureContextOverflow identifies provider rejection because the input exceeded its context limit.
	ProviderFailureContextOverflow
)

// ProviderFailureError preserves one source-owned provider failure and its retry metadata.
type ProviderFailureError struct {
	// Classification identifies the provider's source failure class.
	Classification ProviderFailureClassification
	// RetryDelay contains the provider-requested minimum delay when supplied.
	RetryDelay mo.Option[time.Duration]
	// Cause preserves the complete source error.
	Cause error
}

// Error returns the complete source error text.
func (failure *ProviderFailureError) Error() string {
	return failure.Cause.Error()
}

// Unwrap exposes the original source error without replacing its classification.
func (failure *ProviderFailureError) Unwrap() error {
	return failure.Cause
}

// ProviderAttempt executes exactly one raw provider request.
type ProviderAttempt interface {
	// Stream executes one attempt and emits provider-neutral semantic events.
	Stream(ctx context.Context, request ProviderRequest, handle StreamHandler) error
}

// ConversationContext observes completed agent calls for later estimation.
type ConversationContext interface {
	// ObserveCompletedConversation records one delivered terminal conversation response.
	ObserveCompletedConversation(request ProviderRequest, response model.Response)
}

// ContextPreparationFailure exposes a compaction-owned category to the model-execution consumer.
type ContextPreparationFailure interface {
	error
	// CompactionFailureCode returns the stable compaction category.
	CompactionFailureCode() string
}

// ContextPreparation owns active-conversation threshold and overflow compaction.
type ContextPreparation interface {
	// PrepareContext validates and compacts one agent request before its initial provider attempt.
	PrepareContext(context.Context, ProviderRequest) (ProviderRequest, error)
	// RecoverOverflow compacts one rejected agent request before its single changed replacement attempt.
	RecoverOverflow(context.Context, ProviderRequest) (ProviderRequest, error)
}

// CatalogBinding binds one validated selection to its raw provider attempt.
type CatalogBinding struct {
	// Model contains the immutable configured model descriptor.
	Model model.Descriptor
	// ReasoningChoice identifies the validated reasoning behavior.
	ReasoningChoice model.ReasoningChoice
	// Provider executes one raw provider attempt.
	Provider ProviderAttempt
}

// CatalogResolver resolves active and explicit selections without exposing catalog storage.
type CatalogResolver interface {
	// ActiveBinding returns an atomic snapshot of the active model binding.
	ActiveBinding() CatalogBinding
	// ResolveBinding validates one exact selection without credential preflight.
	ResolveBinding(selection model.Selection) (CatalogBinding, error)
	// ResolveConfiguredBinding validates one exact selection and performs credential preflight.
	ResolveConfiguredBinding(ctx context.Context, selection model.Selection) (CatalogBinding, error)
}
