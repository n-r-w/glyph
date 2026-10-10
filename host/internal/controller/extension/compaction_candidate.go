// Package extension maps extension-initiated requests to session-bound Host operations.
package extension

import (
	"bytes"
	"context"
	"errors"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// policyCandidateKind identifies the requested candidate service.
type policyCandidateKind uint8

const (
	// policyCandidateProjection selects structural candidate projection.
	policyCandidateProjection policyCandidateKind = iota + 1
	// policyCandidateReplacement selects durable replacement.
	policyCandidateReplacement
)

// policyCandidate contains one decoded policy-authorized request.
type policyCandidate struct {
	// kind identifies projection or persistence.
	kind policyCandidateKind
	// operationID identifies the issued mutation authority.
	operationID string
	// value contains the complete replacement candidate.
	value session.CompactionEntry
}

// mapPolicyCandidate validates and decodes one scoped replacement candidate.
func mapPolicyCandidate(
	mapped contextRequest,
	raw *extensionpb.CompactionCandidateRequest,
	kind policyCandidateKind,
) (contextRequest, error) {
	if raw == nil || raw.GetOperationId() == "" || raw.GetResult() == nil {
		return contextRequest{}, errors.New("compaction candidate requires operation authority and a complete result")
	}
	// result contains the complete wire candidate.
	result := raw.GetResult()
	// source and err contain the candidate producer and its decoding failure.
	source, err := mapPolicyCandidateSource(result.GetSource())
	if err != nil {
		return contextRequest{}, err
	}
	// details contains optional detached policy metadata.
	details := mo.None[[]byte]()
	if result.HasDetails() {
		details = mo.Some(bytes.Clone(result.GetDetails()))
	}
	mapped.reference = raw.GetContext()
	// candidate contains the replacement request bound to its policy authority.
	candidate := policyCandidate{kind: kind, operationID: raw.GetOperationId(), value: session.CompactionEntry{
		Summary: result.GetSummary(), FirstKeptEntryID: result.GetFirstKeptEntryId(), Source: source,
		EstimatedCost: mo.None[session.EstimatedCost](), Details: details,
	}}
	mapped.admission = contextAdmissionCandidate
	mapped.prepare = func(o *contextOperation) (contextAction, error) {
		return contextReadAction(func(ctx context.Context) (*extensionpb.HostCompleted, error) {
			return o.runPolicyCandidate(ctx, candidate)
		}), nil
	}

	return mapped, nil
}

// mapPolicyCandidateSource decodes the result's producer and normalized accounting.
func mapPolicyCandidateSource(raw *extensionpb.BranchSummarySource) (session.CompactionSource, error) {
	if raw == nil {
		return session.CompactionSource{}, errors.New("compaction result source is required")
	}
	if raw.HasExtensionId() {
		return session.CompactionSource{
			ExtensionID: mo.Some(raw.GetExtensionId()),
			Model:       mo.None[session.BranchSummaryModelSource](),
		}, nil
	}
	// produced contains model attribution for the candidate.
	produced := raw.GetModel()
	if produced == nil || produced.GetSelection() == nil {
		return session.CompactionSource{}, errors.New("compaction model source selection is required")
	}
	// selection contains the producer model binding.
	selection := produced.GetSelection()
	// usage contains optional producer token accounting.
	usage := mo.None[session.TokenUsage]()
	// reported contains token accounting; its presence selects reported usage.
	if reported := produced.GetUsage(); reported != nil {
		usage = mo.Some(session.TokenUsage{
			InputTokens: reported.GetInputTokens(), OutputTokens: reported.GetOutputTokens(),
			CacheReadTokens: reported.GetCacheReadTokens(), CacheWriteTokens: reported.GetCacheWriteTokens(),
			ReasoningTokens: reported.GetReasoningTokens(), TotalTokens: reported.GetTotalTokens(),
		})
	}
	return session.CompactionSource{ExtensionID: mo.None[string](), Model: mo.Some(session.BranchSummaryModelSource{
		Selection: model.Selection{
			Provider: model.ProviderID(selection.GetProviderId()), Model: model.ID(selection.GetModelId()),
			ReasoningChoice: model.ReasoningChoice(selection.GetReasoningChoice()),
		}, Usage: usage,
	})}, nil
}

// runPolicyCandidate maps the scoped session service outcome to its public result.
func (o *contextOperation) runPolicyCandidate(
	ctx context.Context,
	candidate policyCandidate,
) (*extensionpb.HostCompleted, error) {
	// completed contains the public candidate operation result.
	completed := new(extensionpb.HostCompleted)
	switch candidate.kind {
	case policyCandidateProjection:
		// history and err contain the candidate model input and its projection failure.
		history, err := o.service.compactionPolicy.ProjectPolicyReplacement(
			ctx,
			o.service.extensionID,
			o.service.runtimeID,
			candidate.operationID,
			candidate.value,
		)
		if err != nil {
			return nil, extensionsdk.Fail(compactionFailureCode(err), err)
		}
		// mapped and err contain public history and its encoding failure.
		mapped, err := MapCompactionHistory(history)
		if err != nil {
			return nil, extensionsdk.Fail(internalFailureCode, err)
		}
		completed.SetProjectCompaction(extensionpb.CompactionProjection_builder{History: mapped}.Build())
	case policyCandidateReplacement:
		// entry and commitErr contain the durable receipt and replacement failure.
		entry, commitErr := o.service.compactionPolicy.CommitPolicyReplacement(
			ctx,
			o.service.extensionID,
			o.service.runtimeID,
			candidate.operationID,
			candidate.value,
		)
		// receipt contains the persisted entry when persistence supplied an entry identifier.
		receipt := mo.None[session.Entry]()
		if entry.ID != "" {
			receipt = mo.Some(entry)
		}
		// mapped and err contain the public receipt outcome and its mapping failure.
		mapped, err := mapCompactionCompletion(ctx, CompactionResult{Committed: receipt, Canceled: false}, commitErr)
		if err != nil {
			return nil, err
		}
		completed.SetReplaceCompaction(mapped)
	default:
		return nil, extensionsdk.Fail(internalFailureCode, errors.New("unknown compaction candidate operation"))
	}
	return completed, nil
}
