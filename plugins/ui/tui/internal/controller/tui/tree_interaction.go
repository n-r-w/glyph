package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/samber/lo"
	"github.com/samber/mo"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// requestTree asks for a fresh tree before opening navigation or fork selection.
func (model Model) requestTree(purpose presentationdomain.TreePurpose) (tea.Model, tea.Cmd) {
	model.treeRequest = mo.Some(purpose)
	model.treeAwaiting = presentationdomain.CommandGetSessionTree
	model.treeStatus = ""
	return model.emitCommand(treeCommand(presentationdomain.CommandGetSessionTree, presentationdomain.TreeCommand{
		TargetEntryID: mo.None[string](), SummaryMode: presentationdomain.SummaryModeUnspecified,
		CustomFocus: mo.None[string](), Label: mo.None[string](),
	}))
}

// updateTreeSlashCommand routes tree commands before ordinary editor submission.
func (model Model) updateTreeSlashCommand(
	text string,
	availability presentationdomain.Availability,
) (tea.Model, tea.Cmd, bool) {
	if text != slashCommandTree && text != slashCommandFork && text != slashCommandClone {
		return model, nil, false
	}
	if availability != presentationdomain.AvailabilityIdle {
		return model, nil, true
	}
	switch text {
	case slashCommandTree:
		updated, command := model.requestTree(presentationdomain.TreePurposeNavigate)
		return updated, command, true
	case slashCommandFork:
		updated, command := model.requestTree(presentationdomain.TreePurposeFork)
		return updated, command, true
	case slashCommandClone:
		model.treeAwaiting = presentationdomain.CommandCloneSession
		updated, command := model.emitCommand(
			treeCommand(presentationdomain.CommandCloneSession, presentationdomain.TreeCommand{
				TargetEntryID: mo.None[string](), SummaryMode: presentationdomain.SummaryModeUnspecified,
				CustomFocus: mo.None[string](), Label: mo.None[string](),
			}),
		)
		return updated, command, true
	default:
		return model, nil, false
	}
}

// confirmTreeSelection opens summary choice or sends a fork command.
func (model Model) confirmTreeSelection(panel presentationdomain.TreePanel) (tea.Model, tea.Cmd) {
	entry, present := selectedTreeEntry(panel)
	if !present {
		return model, nil
	}
	if panel.Purpose == presentationdomain.TreePurposeFork {
		if entry.Kind != presentationdomain.TreeEntryUser {
			return model, nil
		}
		model.treeAwaiting = presentationdomain.CommandForkSession
		return model.emitCommand(treeCommand(presentationdomain.CommandForkSession, presentationdomain.TreeCommand{
			TargetEntryID: mo.Some(entry.ID), SummaryMode: presentationdomain.SummaryModeUnspecified,
			CustomFocus: mo.None[string](), Label: mo.None[string](),
		}))
	}
	model.treeMode = treeInteractionSummary
	model.treeSummaryIndex = 0
	return model, nil
}

// confirmTreeInput validates custom focus or sends one persistent label mutation.
func (model Model) confirmTreeInput() (tea.Model, tea.Cmd) {
	if model.treeMode == treeInteractionCustomFocus {
		if strings.TrimSpace(string(model.treeInput)) == "" {
			return model, nil
		}
		return model.emitNavigation(presentationdomain.SummaryModeCustomFocus, mo.Some(string(model.treeInput)))
	}
	panel, present := model.treePanel.Get()
	if !present {
		return model.closeTree(), nil
	}
	entry, selectedPresent := selectedTreeEntry(panel)
	if !selectedPresent {
		return model, nil
	}
	model.treeAwaiting = presentationdomain.CommandSetEntryLabel
	return model.emitCommand(treeCommand(presentationdomain.CommandSetEntryLabel, presentationdomain.TreeCommand{
		TargetEntryID: mo.Some(entry.ID), SummaryMode: presentationdomain.SummaryModeUnspecified,
		CustomFocus: mo.None[string](), Label: mo.Some(string(model.treeInput)),
	}))
}

// emitNavigation sends one selected target and summary mode.
func (model Model) emitNavigation(mode presentationdomain.SummaryMode, focus mo.Option[string]) (tea.Model, tea.Cmd) {
	panel, present := model.treePanel.Get()
	if !present {
		return model.closeTree(), nil
	}
	entry, selectedPresent := selectedTreeEntry(panel)
	if !selectedPresent {
		return model, nil
	}
	model.treeAwaiting = presentationdomain.CommandNavigateSessionTree
	return model.emitCommand(treeCommand(presentationdomain.CommandNavigateSessionTree, presentationdomain.TreeCommand{
		TargetEntryID: mo.Some(entry.ID), SummaryMode: mode, CustomFocus: focus, Label: mo.None[string](),
	}))
}

// applyTreeEvent applies only durable Host results to transcript, editor, session, and label state.
func (model Model) applyTreeEvent(kind presentationdomain.EventKind, event presentationdomain.TreeEvent) Model {
	switch kind {
	case presentationdomain.EventSessionTree:
		tree, present := event.Tree.Get()
		if !present {
			return model
		}
		purpose := presentationdomain.TreePurposeNavigate
		if requested, requestedPresent := model.treeRequest.Get(); requestedPresent {
			purpose = requested
		}
		panel := presentationdomain.NewTreePanel(tree, purpose)
		if purpose == presentationdomain.TreePurposeFork {
			panel.SetFilter(presentationdomain.TreeFilterUserOnly)
		}
		model.treePanel = mo.Some(panel)
		model.treeRequest = mo.None[presentationdomain.TreePurpose]()
		model.treeAwaiting = presentationdomain.CommandUnspecified
		model.treeMode = treeInteractionSelect
		model.treeStatus = ""
		model.input = nil
		model.cursor = 0
	case presentationdomain.EventEntryLabelSet:
		tree, present := event.Tree.Get()
		panel, panelPresent := model.treePanel.Get()
		if present && panelPresent {
			panel.Reconcile(tree)
			model.treePanel = mo.Some(panel)
		}
		model.treeAwaiting = presentationdomain.CommandUnspecified
		model.treeMode = treeInteractionSelect
		model.treeInput = nil
		model.treeCursor = 0
	case presentationdomain.EventTreeOperationFailed:
		model.treeAwaiting = presentationdomain.CommandUnspecified
		model.treeRequest = mo.None[presentationdomain.TreePurpose]()
		model.treeStatus = event.FailureMessage.OrElse(treeOperationFailedText)
	case presentationdomain.EventSessionTreeNavigation:
		model = model.applyTreeNavigationResult(event)
	case presentationdomain.EventSessionForked, presentationdomain.EventSessionCloned:
		model = model.applyTreeReplacement(event)
	case presentationdomain.EventUnspecified, presentationdomain.EventInitialization,
		presentationdomain.EventUserSubmitted, presentationdomain.EventAvailability,
		presentationdomain.EventTurnStarted, presentationdomain.EventModelDelta,
		presentationdomain.EventModelEnd, presentationdomain.EventToolCallPreview,
		presentationdomain.EventToolCallFinal, presentationdomain.EventToolStarted,
		presentationdomain.EventToolProgress, presentationdomain.EventToolOutput,
		presentationdomain.EventToolEnded, presentationdomain.EventToolResult,
		presentationdomain.EventTurnEnded, presentationdomain.EventAgentSettled,
		presentationdomain.EventAuthorization, presentationdomain.EventInformation,
		presentationdomain.EventError, presentationdomain.EventModelSelectionChanged,
		presentationdomain.EventSessionList, presentationdomain.EventSessionChanged,
		presentationdomain.EventSessionInformation:
	}
	return model
}

// applyTreeNavigationResult applies canceled or committed navigation state.
func (model Model) applyTreeNavigationResult(event presentationdomain.TreeEvent) Model {
	model.treeAwaiting = presentationdomain.CommandUnspecified
	model.treeStatus = formatOperationIssues(event.Issues)
	switch event.NavigationStatus {
	case presentationdomain.TreeNavigationCommitted:
		model.replaceTranscript(event.RestoredTranscript)
		model.setExactNextInput(event.NextInput)
		return model.closeTree()
	case presentationdomain.TreeNavigationCanceled:
		model.treeMode = treeInteractionSelect
	case presentationdomain.TreeNavigationUnspecified:
	}
	return model
}

// applyTreeReplacement applies one durable fork or clone result.
func (model Model) applyTreeReplacement(event presentationdomain.TreeEvent) Model {
	model.treeAwaiting = presentationdomain.CommandUnspecified
	model.replaceTranscript(event.RestoredTranscript)
	if info, present := event.SessionInfo.Get(); present {
		model.state.SessionInfo = mo.Some(info)
	}
	model.setExactNextInput(event.NextInput)
	return model.closeTree()
}

// replaceTranscript publishes one durable active branch and clears transient blocks.
func (model *Model) replaceTranscript(transcript []presentationdomain.Line) {
	model.state.Transcript = slices.Clone(transcript)
	model.state.ActiveModel = make(map[int]presentationdomain.ActiveModelContent)
	model.state.ActiveToolCalls = make(map[string]presentationdomain.ToolCallState)
	model.state.ActiveTools = make(map[string]string)
}

// setExactNextInput places exact optional text at the end of the editor without submission.
func (model *Model) setExactNextInput(nextInput mo.Option[string]) {
	text := nextInput.OrEmpty()
	model.input = []rune(text)
	model.cursor = len(model.input)
}

// closeTree removes all entry references from local interaction state.
func (model Model) closeTree() Model {
	model.treePanel = mo.None[presentationdomain.TreePanel]()
	model.treeRequest = mo.None[presentationdomain.TreePurpose]()
	model.treeMode = treeInteractionClosed
	model.treeSummaryIndex = 0
	model.treeInput = nil
	model.treeCursor = 0
	model.treeAwaiting = presentationdomain.CommandUnspecified
	return model
}

// selectedTreeEntry returns the selected complete entry.
func selectedTreeEntry(panel presentationdomain.TreePanel) (presentationdomain.TreeEntry, bool) {
	selectedID, present := panel.SelectedID.Get()
	if !present {
		return presentationdomain.TreeEntry{}, false
	}
	return lo.Find(panel.Tree.Entries, func(entry presentationdomain.TreeEntry) bool { return entry.ID == selectedID })
}

// treeCommand creates one complete presentation command.
func treeCommand(
	kind presentationdomain.CommandKind,
	payload presentationdomain.TreeCommand,
) presentationdomain.Command {
	return presentationdomain.Command{
		Kind: kind, Text: mo.None[string](), ProviderID: mo.None[string](), ModelID: mo.None[string](),
		ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](), SessionID: mo.None[string](),
		SessionName: mo.None[string](), TreeCommand: mo.Some(payload),
	}
}
