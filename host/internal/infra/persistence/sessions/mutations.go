package sessions

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"time"

	"github.com/samber/mo"

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
		if err := state.applyMutation(payload[:lineEnd]); err != nil {
			return replayState{}, fmt.Errorf("decode session mutation record %d: %w", recordNumber, err)
		}
		payload = payload[lineEnd+1:]
	}
	return state, nil
}

// applyMutation decodes and applies one aggregate mutation record.
func (state *replayState) applyMutation(data []byte) error {
	var record mutationRecord
	if err := decodeRecord(data, &record); err != nil {
		return err
	}
	payloads := countPresent(record.Entry != nil, record.Navigation != nil, record.Label != nil, record.SessionInfo != nil)
	if payloads != 1 {
		return errors.New("session mutation must contain exactly one payload")
	}
	switch record.Type {
	case mutationTypeEntry:
		return state.applyEntryMutation(record.Entry)
	case mutationTypeNavigation:
		return state.applyNavigationMutation(record.Navigation)
	case mutationTypeLabel:
		if record.Label == nil || record.Label.TargetID == "" {
			return errors.New("invalid label mutation")
		}
		return state.tree.SetLabel(record.Label.TargetID, record.Label.Label)
	case recordTypeSessionInfo:
		return state.applyInformationMutation(record.SessionInfo)
	default:
		return errors.New("unknown session mutation type")
	}
}

// applyEntryMutation validates and appends one entry payload.
func (state *replayState) applyEntryMutation(raw *jsontext.Value) error {
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
		boundaryErr := state.tree.ValidateSummaryBoundary(summary)
		if boundaryErr != nil {
			return boundaryErr
		}
	}
	return state.tree.Add(entry)
}

// applyNavigationMutation validates a destination and its optional summary as one state change.
func (state *replayState) applyNavigationMutation(record *navigationRecord) error {
	if record == nil {
		return errors.New("navigation mutation payload is missing")
	}
	destination := record.DestinationID
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
	boundaryErr := state.tree.ValidateSummaryBoundary(summaryEntry.BranchSummary.OrEmpty())
	if boundaryErr != nil {
		return boundaryErr
	}
	return state.tree.Add(summaryEntry)
}

// applyInformationMutation validates and publishes the latest session metadata.
func (state *replayState) applyInformationMutation(record *sessionInfoRecord) error {
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
	raw := jsontext.Value(bytes.TrimSuffix(encoded, []byte{'\n'}))
	return encodeLine(mutationRecord{Type: mutationTypeEntry, Entry: &raw, Navigation: nil, Label: nil, SessionInfo: nil})
}

// encodeNavigationMutation frames one destination and optional embedded summary.
func encodeNavigationMutation(navigation hostsessions.NavigationMutation) ([]byte, error) {
	record := mutationRecord{
		Type: mutationTypeNavigation, Entry: nil,
		Navigation: &navigationRecord{DestinationID: navigation.DestinationID, BranchSummary: nil},
		Label:      nil, SessionInfo: nil,
	}
	if summary, present := navigation.BranchSummary.Get(); present {
		encoded, err := encodeEntry(summary)
		if err != nil {
			return nil, err
		}
		raw := jsontext.Value(bytes.TrimSuffix(encoded, []byte{'\n'}))
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
		Type: mutationTypeLabel, Entry: nil, Navigation: nil,
		Label: &labelRecord{TargetID: label.TargetID, Label: label.Label}, SessionInfo: nil,
	})
}

// encodeInformationMutation validates and frames one metadata change.
func encodeInformationMutation(information hostsessions.SessionInformationMutation) ([]byte, error) {
	if information.Name == "" || information.CreatedAt.IsZero() {
		return nil, errors.New("session information is invalid")
	}
	return encodeLine(mutationRecord{
		Type: recordTypeSessionInfo, Entry: nil, Navigation: nil, Label: nil,
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
