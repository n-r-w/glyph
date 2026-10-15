// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"github.com/samber/mo"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// Participant identifies one captured compaction capability.
type Participant struct {
	// ExtensionID identifies the participant's extension.
	ExtensionID string
	// RuntimeID identifies the captured runtime generation.
	RuntimeID string
	// HandlerID identifies the registered capability.
	HandlerID string
}

// HandlerSet contains one operation's captured capability membership.
type HandlerSet struct {
	// Requests contains request participants in registration order.
	Requests []Participant
	// Generators contains registered replacement implementations.
	Generators []Participant
	// Results contains result participants in registration order.
	Results []Participant
	// Successes contains committed-outcome observers in registration order.
	Successes []Participant
	// Failures contains failure-outcome observers in registration order.
	Failures []Participant
}

// Trigger identifies the compaction intent supplied by the policy.
type Trigger uint8

const (
	// TriggerManual identifies a user- or extension-requested compaction.
	TriggerManual Trigger = iota + 1
	// TriggerThreshold identifies preparation above the estimated input budget.
	TriggerThreshold
	// TriggerOverflow identifies recovery after a provider context rejection.
	TriggerOverflow
)

// InputEntry contains one projected branch entry and its extension-supplied estimate.
type InputEntry struct {
	// Entry is the embedded provider-neutral branch projection.
	session.Entry
	// EstimatedTokens is the extension-supplied token estimate for this entry.
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
	// RetryIntent contains the policy's overflow-retry intent.
	RetryIntent bool
	// Instructions contains optional manual guidance.
	Instructions mo.Option[string]
	// Model contains the descriptor supplied for summary composition.
	Model model.Descriptor
	// ReasoningChoice contains the selection supplied for summary composition.
	ReasoningChoice model.ReasoningChoice
	// Prefix contains the proposed summarized entries with extension-supplied estimates.
	Prefix []InputEntry
	// Suffix contains the proposed retained entries with extension-supplied estimates.
	Suffix []InputEntry
	// Previous contains the preceding summary marker when present.
	Previous mo.Option[session.CompactionEntry]
	// ContextTokens is the extension-supplied context estimate.
	ContextTokens int64
	// ContextTokensEstimated is required and identifies ContextTokens as an estimate.
	ContextTokensEstimated bool
	// ContextWindow contains the composition model's context capacity.
	ContextWindow int64
	// ResponseBudget contains the composition output reserve.
	ResponseBudget int64
	// RetainedBudget is the extension-supplied recent-context target.
	RetainedBudget int64
}

// clone detaches the composition request's mutable values.
func (value Request) clone() Request {
	value.Model = value.Model.Clone()
	value.Prefix = cloneInputEntries(value.Prefix)
	value.Suffix = cloneInputEntries(value.Suffix)
	value.Previous = value.Previous.MapValue(session.CompactionEntry.Clone)
	return value
}

// cloneInputEntries detaches projected entries and retains supplied estimates.
func cloneInputEntries(entries []InputEntry) []InputEntry {
	if entries == nil {
		return nil
	}
	cloned := make([]InputEntry, len(entries))
	for index := range entries {
		cloned[index] = InputEntry{
			Entry: entries[index].Clone(), EstimatedTokens: entries[index].EstimatedTokens,
		}
	}
	return cloned
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

// clone detaches optional extension details.
func (result Result) clone() Result {
	result.Details = result.Details.MapValue(cloneResultDetails)
	return result
}

// cloneResultDetails detaches opaque composition details.
func cloneResultDetails(value []byte) []byte { return append([]byte(nil), value...) }

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
	// Cancel indicates exclusive compaction cancellation without a commit.
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
	// Original is the immutable policy-supplied request.
	Original Request
	// Current is the request left by preceding handlers.
	Current Request
	// CurrentResult is the result left by preceding handlers when present.
	CurrentResult mo.Option[Result]
}
