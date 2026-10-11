// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"github.com/samber/mo"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
)

// RequestParticipant identifies one captured request-handler capability.
type RequestParticipant struct {
	// ExtensionID identifies the participant's extension.
	ExtensionID string
	// RuntimeID identifies the captured runtime generation.
	RuntimeID string
	// HandlerID identifies the registered request handler.
	HandlerID string
}

// newRequestParticipants supplies captured request capabilities to their policy owner.
func newRequestParticipants(handlers []contextcompaction.Handler) []RequestParticipant {
	participants := make([]RequestParticipant, len(handlers))
	for index := range handlers {
		participants[index] = RequestParticipant{
			ExtensionID: handlers[index].ExtensionID,
			RuntimeID:   handlers[index].RuntimeID,
			HandlerID:   handlers[index].HandlerID,
		}
	}
	return participants
}

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

// InputEntry contains one projected branch entry and its Host fallback estimate.
type InputEntry struct {
	// Entry is the embedded provider-neutral branch projection.
	session.Entry
	// EstimatedTokens is the Host-computed fallback estimate for this entry.
	EstimatedTokens int64
}

// newInputEntry supplies one admitted branch value to the policy participant.
func newInputEntry(value extensioncontroller.CompactionRequestEntry) InputEntry {
	return InputEntry{Entry: value.Entry, EstimatedTokens: value.EstimatedTokens}
}

// newInputEntries preserves the ordered admitted branch entries.
func newInputEntries(values []extensioncontroller.CompactionRequestEntry) []InputEntry {
	entries := make([]InputEntry, len(values))
	for index := range values {
		entries[index] = newInputEntry(values[index])
	}
	return entries
}

// mapRequestResponseEntries supplies returned branch entries to the policy response.
func mapRequestResponseEntries(values []InputEntry) []extensioncontroller.CompactionRequestEntry {
	entries := make([]extensioncontroller.CompactionRequestEntry, len(values))
	for index := range values {
		entries[index] = values[index].mapToControllerEntry()
	}
	return entries
}

// mapToControllerEntry supplies one returned branch value to the policy response.
func (value InputEntry) mapToControllerEntry() extensioncontroller.CompactionRequestEntry {
	return extensioncontroller.CompactionRequestEntry{Entry: value.Entry, EstimatedTokens: value.EstimatedTokens}
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

// newRequest supplies admitted request values to the policy participant.
func newRequest(value extensioncontroller.CompactionRequestValue) Request {
	prefix := newInputEntries(value.Prefix)
	suffix := newInputEntries(value.Suffix)
	return Request{
		Trigger:                Trigger(value.Trigger),
		RetryIntent:            value.RetryIntent,
		Instructions:           value.Instructions,
		Model:                  value.Model,
		ReasoningChoice:        value.ReasoningChoice,
		Prefix:                 prefix,
		Suffix:                 suffix,
		Previous:               value.Previous,
		ContextTokens:          value.ContextTokens,
		ContextTokensEstimated: value.ContextTokensEstimated,
		ContextWindow:          value.ContextWindow,
		ResponseBudget:         value.ResponseBudget,
		RetainedBudget:         value.RetainedBudget,
	}
}

// mapToControllerValue supplies the request replacement to the policy response.
func (value Request) mapToControllerValue() extensioncontroller.CompactionRequestValue {
	prefix := mapRequestResponseEntries(value.Prefix)
	suffix := mapRequestResponseEntries(value.Suffix)
	return extensioncontroller.CompactionRequestValue{
		Trigger:                extensioncontroller.CompactionRequestTrigger(value.Trigger),
		RetryIntent:            value.RetryIntent,
		Instructions:           value.Instructions,
		Model:                  value.Model,
		ReasoningChoice:        value.ReasoningChoice,
		Prefix:                 prefix,
		Suffix:                 suffix,
		Previous:               value.Previous,
		ContextTokens:          value.ContextTokens,
		ContextTokensEstimated: value.ContextTokensEstimated,
		ContextWindow:          value.ContextWindow,
		ResponseBudget:         value.ResponseBudget,
		RetainedBudget:         value.RetainedBudget,
	}
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

// newResult supplies admitted summary state to the policy participant.
func newResult(value session.CompactionEntry) Result {
	return Result{
		Summary:          value.Summary,
		FirstKeptEntryID: value.FirstKeptEntryID,
		Source:           value.Source,
		Details:          value.Details,
	}
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

// RequestAction contains the independent request and result changes returned by one participant.
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

// RequestInvocation carries immutable original and composed current state.
type RequestInvocation struct {
	// ParentCompactionID correlates nested work with a non-authorizing parent lifetime.
	ParentCompactionID mo.Option[string]
	// Original is the immutable Host-produced request.
	Original Request
	// Current is the request left by preceding handlers.
	Current Request
	// CurrentResult is the result left by preceding handlers when present.
	CurrentResult mo.Option[Result]
}
