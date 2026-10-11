// Package extensionruntime owns extension process lifetimes and routes scoped calls.
package extensionruntime

import (
	"context"

	"github.com/samber/mo/option"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
)

// InvokeCompactionRequest invokes the participant's compaction request handler in the captured runtime.
func (s *Service) InvokeCompactionRequest(
	ctx context.Context,
	participant extensioncontext.RequestParticipant,
	binding extension.Context,
	invocation extensioncontext.RequestInvocation,
) (extensioncontext.RequestAction, error) {
	handler := contextcompaction.Handler{
		ExtensionID: participant.ExtensionID,
		RuntimeID:   participant.RuntimeID,
		HandlerID:   participant.HandlerID,
	}
	owner, err := s.compactionOwner(handler)
	if err != nil {
		return extensioncontext.RequestAction{}, err
	}
	action, invokeErr := owner.state.runtime.HandleCompactionRequest(
		ctx, participant.HandlerID, binding, mapPolicyRequestInvocation(invocation),
	)
	s.finishAndReport(ctx, owner, invokeErr)
	return mapPolicyRequestAction(action), invokeErr
}

// mapPolicyRequestInvocation supplies the runtime dispatch representation.
func mapPolicyRequestInvocation(value extensioncontext.RequestInvocation) contextcompaction.RequestInvocation {
	return contextcompaction.RequestInvocation{
		ParentCompactionID: value.ParentCompactionID,
		Original:           mapPolicyRequestValue(value.Original),
		Current:            mapPolicyRequestValue(value.Current),
		CurrentResult:      option.Map(mapPolicyRequestResult)(value.CurrentResult),
	}
}

// mapPolicyRequestValue supplies one request to the runtime dispatcher.
func mapPolicyRequestValue(value extensioncontext.Request) contextcompaction.Request {
	return contextcompaction.Request{
		Trigger:                contextcompaction.Trigger(value.Trigger),
		RetryIntent:            value.RetryIntent,
		Instructions:           value.Instructions,
		Model:                  value.Model,
		ReasoningChoice:        value.ReasoningChoice,
		Prefix:                 mapPolicyRequestEntries(value.Prefix),
		Suffix:                 mapPolicyRequestEntries(value.Suffix),
		Previous:               value.Previous,
		ContextTokens:          value.ContextTokens,
		ContextTokensEstimated: value.ContextTokensEstimated,
		ContextWindow:          value.ContextWindow,
		ResponseBudget:         value.ResponseBudget,
		RetainedBudget:         value.RetainedBudget,
	}
}

// mapPolicyRequestEntry supplies one branch entry to the runtime dispatcher.
func mapPolicyRequestEntry(value extensioncontext.InputEntry) contextcompaction.InputEntry {
	return contextcompaction.InputEntry{Entry: value.Entry, EstimatedTokens: value.EstimatedTokens}
}

// mapPolicyRequestEntries supplies ordered branch entries to runtime dispatch.
func mapPolicyRequestEntries(values []extensioncontext.InputEntry) []contextcompaction.InputEntry {
	entries := make([]contextcompaction.InputEntry, len(values))
	for index := range values {
		entries[index] = mapPolicyRequestEntry(values[index])
	}
	return entries
}

// mapPolicyRequestResult converts a compaction result to the runtime representation.
func mapPolicyRequestResult(value extensioncontext.Result) contextcompaction.Result {
	return contextcompaction.Result{
		Summary:          value.Summary,
		FirstKeptEntryID: value.FirstKeptEntryID,
		Source:           value.Source,
		Details:          value.Details,
	}
}

// mapPolicyRequestAction supplies the participant's independent changes to its policy owner.
func mapPolicyRequestAction(value contextcompaction.RequestAction) extensioncontext.RequestAction {
	return extensioncontext.RequestAction{
		Cancel:        value.Cancel,
		RequestAction: extensioncontext.RequestActionKind(value.RequestAction),
		Request:       option.Map(mapPolicyResponseRequest)(value.Request),
		ResultAction:  extensioncontext.ResultActionKind(value.ResultAction),
		Result:        option.Map(mapPolicyResponseResult)(value.Result),
	}
}

// mapPolicyResponseRequest supplies a complete returned request to the policy owner.
func mapPolicyResponseRequest(value contextcompaction.Request) extensioncontext.Request {
	return extensioncontext.Request{
		Trigger:                extensioncontext.Trigger(value.Trigger),
		RetryIntent:            value.RetryIntent,
		Instructions:           value.Instructions,
		Model:                  value.Model,
		ReasoningChoice:        value.ReasoningChoice,
		Prefix:                 mapPolicyResponseEntries(value.Prefix),
		Suffix:                 mapPolicyResponseEntries(value.Suffix),
		Previous:               value.Previous,
		ContextTokens:          value.ContextTokens,
		ContextTokensEstimated: value.ContextTokensEstimated,
		ContextWindow:          value.ContextWindow,
		ResponseBudget:         value.ResponseBudget,
		RetainedBudget:         value.RetainedBudget,
	}
}

// mapPolicyResponseEntry supplies one returned branch entry to the policy owner.
func mapPolicyResponseEntry(value contextcompaction.InputEntry) extensioncontext.InputEntry {
	return extensioncontext.InputEntry{Entry: value.Entry, EstimatedTokens: value.EstimatedTokens}
}

// mapPolicyResponseEntries supplies ordered returned entries to the policy owner.
func mapPolicyResponseEntries(values []contextcompaction.InputEntry) []extensioncontext.InputEntry {
	entries := make([]extensioncontext.InputEntry, len(values))
	for index := range values {
		entries[index] = mapPolicyResponseEntry(values[index])
	}
	return entries
}

// mapPolicyResponseResult supplies the returned summary state to the policy owner.
func mapPolicyResponseResult(value contextcompaction.Result) extensioncontext.Result {
	return extensioncontext.Result{
		Summary:          value.Summary,
		FirstKeptEntryID: value.FirstKeptEntryID,
		Source:           value.Source,
		Details:          value.Details,
	}
}
