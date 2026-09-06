package ui

import (
	"errors"
	"strings"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// SubmittedText validates a present submit payload at its admission boundary.
func (command Command) SubmittedText() (string, error) {
	text, present := command.Text.Get()
	if !present || text == "" {
		return "", errors.New("UI submit text is required")
	}
	return text, nil
}

// ValidateSession checks required request fields without domain work.
//
//nolint:gocyclo // The closed request union has distinct required fields.
func (command Command) ValidateSession() error {
	switch command.Kind {
	case CommandCreateSession, CommandListSessions, CommandGetSessionInfo,
		CommandGetSessionTree, CommandCloneSession:
		return nil
	case CommandResumeSession:
		if command.SessionID.IsNone() || command.SessionID.OrEmpty() == "" {
			return errors.New("session identifier is required")
		}
	case CommandSetSessionName:
		if command.SessionName.IsNone() || strings.TrimSpace(command.SessionName.OrEmpty()) == "" {
			return errors.New("session name is required")
		}
	case CommandNavigateSessionTree:
		target, present := command.TargetEntryID.Get()
		mode := command.SummaryMode
		validMode := mode == SummaryModeNoSummary || mode == SummaryModeSummarize ||
			mode == SummaryModeSummarizeWithCustomPrompt
		focus := strings.TrimSpace(command.CustomFocus.OrEmpty())
		invalidFocus := mode == SummaryModeSummarizeWithCustomPrompt && focus == "" ||
			mode != SummaryModeSummarizeWithCustomPrompt && focus != ""
		if !present || target == "" || !validMode || invalidFocus {
			return errors.New("tree navigation request is invalid")
		}
	case CommandForkSession:
		if command.TargetEntryID.IsNone() || command.TargetEntryID.OrEmpty() == "" {
			return errors.New("fork target is required")
		}
	case CommandSetEntryLabel:
		if command.TargetEntryID.IsNone() || command.TargetEntryID.OrEmpty() == "" || command.EntryLabel.IsNone() {
			return errors.New("entry label request is incomplete")
		}
	case CommandSubmit,
		CommandRetryAuthentication,
		CommandSelectModel,
		CommandSelectReasoningChoice:
		return errors.New("UI operation kind is invalid")
	default:
		return errors.New("UI operation kind is unknown")
	}
	return nil
}

// SelectedReasoningChoice validates a requested choice before catalog-specific admission.
func (command Command) SelectedReasoningChoice() (model.ReasoningChoice, error) {
	choice, present := command.ReasoningChoice.Get()
	if !present {
		return "", errors.New("reasoning choice is required")
	}
	switch choice {
	case model.ReasoningChoiceOff, model.ReasoningChoiceOn, model.ReasoningChoiceMinimal, model.ReasoningChoiceLow,
		model.ReasoningChoiceMedium, model.ReasoningChoiceHigh, model.ReasoningChoiceXHigh, model.ReasoningChoiceMax:
		return choice, nil
	default:
		return "", errors.New("reasoning choice is invalid")
	}
}
