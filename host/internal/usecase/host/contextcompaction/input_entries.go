package contextcompaction

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// newEstimatedInputEntries clones branch entries and derives their Host fallback estimates.
func (s *Service) newEstimatedInputEntries(entries []session.Entry) ([]InputEntry, error) {
	if entries == nil {
		return nil, nil
	}
	projected := make([]InputEntry, len(entries))
	for index := range entries {
		estimate, err := s.estimateInputEntry(entries[index])
		if err != nil {
			return nil, fmt.Errorf("estimate compaction input entry %q: %w", entries[index].ID, err)
		}
		projected[index] = InputEntry{Entry: entries[index].Clone(), EstimatedTokens: estimate}
	}
	return projected, nil
}

// estimateInputEntry applies the approved record policy to one extension-visible branch entry.
func (s *Service) estimateInputEntry(entry session.Entry) (int64, error) {
	if entry.Compaction.IsSome() || entry.Extension.IsSome() || entry.Information.IsSome() {
		return 0, nil
	}
	if message, present := entry.User.Get(); present {
		return estimateUserMessage(message), nil
	}
	if response, present := entry.Model.Get(); present {
		if outcome, terminal := response.Outcome.Get(); terminal &&
			(outcome == model.OutcomeAborted || outcome == model.OutcomeFailed) {
			return 0, nil
		}
		return estimateModelResponse(response)
	}
	if result, present := entry.ToolResult.Get(); present {
		return estimateToolResult(result), nil
	}
	if message, present := entry.ExtensionMessage.Get(); present {
		return estimateUserMessage(model.TextMessage(message.Text)), nil
	}
	owned := entry.Clone()
	owned.ParentID = mo.None[string]()
	history, err := s.sessions.ProjectSuffix([]session.Entry{owned}, owned.ID)
	if err != nil {
		return 0, err
	}
	return estimateHistory(history)
}

// deriveReplacementEstimates ignores extension-supplied estimates and derives trusted current values.
func (s *Service) deriveReplacementEstimates(original, current, candidate Request) (Request, error) {
	originalByID := inputEntriesByID(original)
	currentByID := inputEntriesByID(current)
	derive := func(values []InputEntry) ([]InputEntry, error) {
		result := cloneInputEntries(values)
		for index := range result {
			if prior, exists := currentByID[result[index].ID]; exists &&
				publicEntryPayloadEqual(prior.Entry, result[index].Entry) {
				result[index].EstimatedTokens = prior.EstimatedTokens
				continue
			}
			if prior, exists := originalByID[result[index].ID]; exists &&
				publicEntryPayloadEqual(prior.Entry, result[index].Entry) {
				result[index].EstimatedTokens = prior.EstimatedTokens
				continue
			}
			estimate, err := s.estimateInputEntry(result[index].Entry)
			if err != nil {
				return nil, fmt.Errorf("estimate replacement compaction entry %q: %w", result[index].ID, err)
			}
			result[index].EstimatedTokens = estimate
		}
		return result, nil
	}
	var err error
	candidate.Prefix, err = derive(candidate.Prefix)
	if err != nil {
		return Request{}, err
	}
	candidate.Suffix, err = derive(candidate.Suffix)
	if err != nil {
		return Request{}, err
	}
	return candidate, nil
}

// inputEntriesByID indexes one valid request state without sharing mutable entry values.
func inputEntriesByID(request Request) map[string]InputEntry {
	values := append(cloneInputEntries(request.Prefix), request.Suffix...)
	indexed := make(map[string]InputEntry, len(values))
	for index := range values {
		indexed[values[index].ID] = values[index]
	}
	return indexed
}

// inputEntryKind identifies the single public payload carried by an input entry.
type inputEntryKind uint8

const (
	// inputEntryInvalid identifies a missing or ambiguous payload.
	inputEntryInvalid inputEntryKind = iota
	// inputEntryUser identifies a user message.
	inputEntryUser
	// inputEntryModel identifies a model response.
	inputEntryModel
	// inputEntryToolResult identifies a tool result.
	inputEntryToolResult
	// inputEntryBranchSummary identifies a branch summary.
	inputEntryBranchSummary
	// inputEntryCompaction identifies a compaction marker.
	inputEntryCompaction
	// inputEntryExtension identifies a model-hidden extension entry.
	inputEntryExtension
	// inputEntryExtensionMessage identifies a model-visible extension message.
	inputEntryExtensionMessage
)

// publicEntryPayloadEqual compares only values exposed through the compaction input contract.
func publicEntryPayloadEqual(left, right session.Entry) bool {
	leftKind := publicInputEntryKind(left)
	if left.ID != right.ID || leftKind == inputEntryInvalid || leftKind != publicInputEntryKind(right) {
		return false
	}
	switch leftKind {
	case inputEntryUser:
		return reflect.DeepEqual(left.User, right.User)
	case inputEntryModel:
		return publicModelResponseEqual(left.Model, right.Model)
	case inputEntryToolResult:
		return reflect.DeepEqual(left.ToolResult, right.ToolResult)
	case inputEntryBranchSummary:
		return left.BranchSummary.OrEmpty().Summary == right.BranchSummary.OrEmpty().Summary
	case inputEntryCompaction:
		return reflect.DeepEqual(left.Compaction, right.Compaction)
	case inputEntryExtension:
		leftValue := left.Extension.OrEmpty()
		rightValue := right.Extension.OrEmpty()
		return leftValue.ExtensionID == rightValue.ExtensionID && leftValue.EntryType == rightValue.EntryType
	case inputEntryExtensionMessage:
		return reflect.DeepEqual(left.ExtensionMessage, right.ExtensionMessage)
	case inputEntryInvalid:
		return false
	default:
		return false
	}
}

// publicInputEntryKind returns the unique public payload kind or invalid for malformed entries.
func publicInputEntryKind(entry session.Entry) inputEntryKind {
	kinds := make([]inputEntryKind, 0, 1)
	for kind, present := range map[inputEntryKind]bool{
		inputEntryUser: entry.User.IsSome(), inputEntryModel: entry.Model.IsSome(),
		inputEntryToolResult: entry.ToolResult.IsSome(), inputEntryBranchSummary: entry.BranchSummary.IsSome(),
		inputEntryCompaction: entry.Compaction.IsSome(), inputEntryExtension: entry.Extension.IsSome(),
		inputEntryExtensionMessage: entry.ExtensionMessage.IsSome(),
	} {
		if present {
			kinds = append(kinds, kind)
		}
	}
	if len(kinds) != 1 {
		return inputEntryInvalid
	}
	return kinds[0]
}

// publicModelResponseEqual compares finalized model content while excluding opaque provider replay.
func publicModelResponseEqual(left, right mo.Option[model.Response]) bool {
	leftValue, leftPresent := left.Get()
	rightValue, rightPresent := right.Get()
	if !leftPresent || !rightPresent || len(leftValue.Content) != len(rightValue.Content) {
		return false
	}
	for index := range leftValue.Content {
		leftContent := leftValue.Content[index]
		rightContent := rightValue.Content[index]
		if leftContent.Kind != rightContent.Kind || !reflect.DeepEqual(leftContent.Text, rightContent.Text) ||
			!reflect.DeepEqual(leftContent.ToolCall, rightContent.ToolCall) {
			return false
		}
	}
	return true
}

// validateReplacementEntryPayload rejects malformed nested public entry values before composition.
func validateReplacementEntryPayload(entry session.Entry) error {
	payloads := 0
	for _, present := range []bool{
		entry.User.IsSome(), entry.Model.IsSome(), entry.ToolResult.IsSome(), entry.Extension.IsSome(),
		entry.ExtensionMessage.IsSome(), entry.BranchSummary.IsSome(), entry.Compaction.IsSome(),
	} {
		if present {
			payloads++
		}
	}
	if entry.ID == "" || payloads != 1 {
		return errors.New("compaction input entry must have an ID and exactly one payload")
	}
	if response, present := entry.Model.Get(); present {
		if err := response.ValidateTerminalContent(); err != nil {
			return err
		}
	}
	if summary, present := entry.BranchSummary.Get(); present && strings.TrimSpace(summary.Summary) == "" {
		return errors.New("compaction input branch summary is empty")
	}
	if compaction, present := entry.Compaction.Get(); present {
		if strings.TrimSpace(compaction.Summary) == "" {
			return errors.New("compaction input summary is empty")
		}
		if strings.TrimSpace(compaction.FirstKeptEntryID) == "" {
			return errors.New("compaction input boundary is empty")
		}
		if err := compaction.ValidateAccounting(); err != nil {
			return fmt.Errorf("validate compaction input accounting: %w", err)
		}
	}
	return nil
}
