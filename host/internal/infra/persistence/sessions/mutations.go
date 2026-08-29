package sessions

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	hostsessions "github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
)

// replayState owns the all-or-nothing aggregate built from validated mutation records.
type replayState struct {
	// tree contains entries, labels, and the active leaf validated so far.
	tree session.Tree
	// information contains the latest session-information mutation.
	information mo.Option[session.Information]
	// informationUpdatedAt contains the timestamp of the latest information mutation.
	informationUpdatedAt mo.Option[time.Time]
}

// newReplayState creates an empty valid aggregate.
func newReplayState() replayState {
	tree, err := session.NewTree(nil, mo.None[string](), nil)
	if err != nil {
		panic(err)
	}
	return replayState{
		tree: tree, information: mo.None[session.Information](), informationUpdatedAt: mo.None[time.Time](),
	}
}

// decodeMutations replays complete records into a private aggregate or returns no partial result.
func decodeMutations(payload []byte) (replayState, error) {
	state := newReplayState()
	recordNumber := 0
	for len(payload) > 0 {
		lineEnd := bytes.IndexByte(payload, '\n')
		if lineEnd < 0 {
			return replayState{}, errors.New("completed session mutation is missing a newline")
		}
		recordNumber++
		if err := applyMutation(&state, payload[:lineEnd]); err != nil {
			return replayState{}, fmt.Errorf("decode session mutation record %d: %w", recordNumber, err)
		}
		payload = payload[lineEnd+1:]
	}
	return state, nil
}

// applyMutation decodes and applies one strict version 2 record.
func applyMutation(state *replayState, data []byte) error {
	var record mutationRecord
	if err := decodeRecord(data, &record); err != nil {
		return err
	}
	payloads := countPresent(record.Entry != nil, record.Navigation != nil, record.Label != nil, record.SessionInfo != nil)
	if payloads != 1 {
		return errors.New("session mutation must contain exactly one payload")
	}
	switch record.Type {
	case "entry":
		return applyEntryMutation(state, record.Entry)
	case "navigation":
		return applyNavigationMutation(state, record.Navigation)
	case "label":
		if record.Label == nil || record.Label.TargetID == "" {
			return errors.New("invalid label mutation")
		}
		return state.tree.SetLabel(record.Label.TargetID, record.Label.Label)
	case "session_info":
		return applyInformationMutation(state, record.SessionInfo)
	default:
		return errors.New("unknown session mutation type")
	}
}

// applyEntryMutation validates and appends one entry payload.
func applyEntryMutation(state *replayState, raw *json.RawMessage) error {
	if raw == nil {
		return errors.New("entry mutation payload is missing")
	}
	entry, err := decodeEntry(*raw)
	if err != nil {
		return err
	}
	if entry.Information.IsSome() {
		return errors.New("invalid entry mutation payload")
	}
	if summary, present := entry.BranchSummary.Get(); present {
		boundaryErr := validateSummaryBoundary(state.tree, summary)
		if boundaryErr != nil {
			return boundaryErr
		}
	}
	return state.tree.Add(entry)
}

// applyNavigationMutation validates a destination and its optional summary as one state change.
func applyNavigationMutation(state *replayState, record *navigationRecord) error {
	if record == nil {
		return errors.New("navigation mutation payload is missing")
	}
	destination := pointerStringOption(record.DestinationID)
	if err := state.tree.SetActiveLeaf(destination); err != nil {
		return err
	}
	if record.BranchSummary == nil {
		return nil
	}
	summaryEntry, err := decodeEntry(*record.BranchSummary)
	if err != nil {
		return err
	}
	if summaryEntry.BranchSummary.IsNone() || summaryEntry.ParentID != destination {
		return errors.New("invalid navigation branch summary")
	}
	boundaryErr := validateSummaryBoundary(state.tree, summaryEntry.BranchSummary.OrEmpty())
	if boundaryErr != nil {
		return boundaryErr
	}
	return state.tree.Add(summaryEntry)
}

// applyInformationMutation validates and publishes the latest session metadata.
func applyInformationMutation(state *replayState, record *sessionInfoRecord) error {
	if record == nil || record.Name == "" {
		return errors.New("invalid session information mutation")
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, record.CreatedAt)
	if err != nil {
		return fmt.Errorf("parse session information timestamp: %w", err)
	}
	state.information = mo.Some(session.Information{Name: record.Name})
	state.informationUpdatedAt = mo.Some(updatedAt)
	return nil
}

// encodeMutation validates one use-case mutation and returns one complete JSONL line.
func encodeMutation(mutation hostsessions.Mutation) ([]byte, error) {
	if countPresent(
		mutation.Entry.IsSome(), mutation.Navigation.IsSome(),
		mutation.Label.IsSome(), mutation.SessionInformation.IsSome(),
	) != 1 {
		return nil, errors.New("session mutation must contain exactly one payload")
	}
	if mutation.Entry.IsSome() {
		return encodeEntryMutation(mutation.Entry.OrEmpty())
	}
	if mutation.Navigation.IsSome() {
		return encodeNavigationMutation(mutation.Navigation.OrEmpty())
	}
	if mutation.Label.IsSome() {
		return encodeLabelMutation(mutation.Label.OrEmpty())
	}
	return encodeInformationMutation(mutation.SessionInformation.OrEmpty())
}

// encodeEntryMutation frames one encoded tree entry.
func encodeEntryMutation(entry session.Entry) ([]byte, error) {
	encoded, err := encodeEntry(entry)
	if err != nil {
		return nil, err
	}
	raw := json.RawMessage(bytes.TrimSuffix(encoded, []byte{'\n'}))
	return encodeLine(mutationRecord{Type: "entry", Entry: &raw, Navigation: nil, Label: nil, SessionInfo: nil})
}

// encodeNavigationMutation frames one destination and optional embedded summary.
func encodeNavigationMutation(navigation hostsessions.NavigationMutation) ([]byte, error) {
	record := mutationRecord{
		Type: "navigation", Entry: nil,
		Navigation: &navigationRecord{DestinationID: optionStringPointer(navigation.DestinationID), BranchSummary: nil},
		Label:      nil, SessionInfo: nil,
	}
	if summary, present := navigation.BranchSummary.Get(); present {
		encoded, err := encodeEntry(summary)
		if err != nil {
			return nil, err
		}
		raw := json.RawMessage(bytes.TrimSuffix(encoded, []byte{'\n'}))
		record.Navigation.BranchSummary = &raw
	}
	return encodeLine(record)
}

// encodeLabelMutation validates and frames one label change.
func encodeLabelMutation(label hostsessions.LabelMutation) ([]byte, error) {
	if label.TargetID == "" {
		return nil, errors.New("label target is required")
	}
	return encodeLine(mutationRecord{
		Type: "label", Entry: nil, Navigation: nil,
		Label: &labelRecord{TargetID: label.TargetID, Label: label.Label}, SessionInfo: nil,
	})
}

// encodeInformationMutation validates and frames one metadata change.
func encodeInformationMutation(information hostsessions.SessionInformationMutation) ([]byte, error) {
	if information.Name == "" || information.CreatedAt.IsZero() {
		return nil, errors.New("session information is invalid")
	}
	return encodeLine(mutationRecord{
		Type: "session_info", Entry: nil, Navigation: nil, Label: nil,
		SessionInfo: &sessionInfoRecord{Name: information.Name, CreatedAt: information.CreatedAt.Format(time.RFC3339Nano)},
	})
}

// countPresent counts selected alternatives without coupling their concrete types.
func countPresent(values ...bool) int {
	count := 0
	for _, present := range values {
		if present {
			count++
		}
	}
	return count
}

// validateSummaryBoundary checks connected local provenance while allowing unresolved copied provenance.
func validateSummaryBoundary(tree session.Tree, summary session.BranchSummaryEntry) error {
	entries := tree.Entries()
	byID := make(map[string]session.Entry, len(entries))
	for index := range entries {
		byID[entries[index].ID] = entries[index]
	}
	first, firstExists := byID[summary.FirstEntryID]
	_, lastExists := byID[summary.LastEntryID]
	if !firstExists || !lastExists {
		return nil
	}
	current := summary.LastEntryID
	for {
		if current == first.ID {
			return nil
		}
		entry := byID[current]
		parent, present := entry.ParentID.Get()
		if !present {
			return errors.New("branch summary boundary is disconnected")
		}
		current = parent
	}
}

// validReasoningChoice accepts the complete closed provider-neutral choice set.
func validReasoningChoice(choice model.ReasoningChoice) bool {
	switch choice {
	case model.ReasoningChoiceOff, model.ReasoningChoiceOn, model.ReasoningChoiceMinimal,
		model.ReasoningChoiceLow, model.ReasoningChoiceMedium, model.ReasoningChoiceHigh,
		model.ReasoningChoiceXHigh, model.ReasoningChoiceMax:
		return true
	default:
		return false
	}
}

// validTokenUsage rejects negative, overlapping, and inconsistent normalized usage.
func validTokenUsage(usage session.TokenUsage) bool {
	if usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.CacheReadTokens < 0 ||
		usage.CacheWriteTokens < 0 || usage.ReasoningTokens < 0 || usage.TotalTokens < 0 {
		return false
	}
	return usage.ReasoningTokens <= usage.OutputTokens &&
		usage.TotalTokens == usage.InputTokens+usage.OutputTokens+usage.CacheReadTokens+usage.CacheWriteTokens
}

// validEstimatedCost rejects non-finite, negative, and inconsistent persisted cost.
func validEstimatedCost(cost session.EstimatedCost) bool {
	values := []float64{cost.Input, cost.Output, cost.CacheRead, cost.CacheWrite, cost.Total}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return false
		}
	}
	return cost.Total == cost.Input+cost.Output+cost.CacheRead+cost.CacheWrite
}
