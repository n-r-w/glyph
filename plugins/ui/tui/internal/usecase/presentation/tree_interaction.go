package presentation

import (
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/mo"
)

// requestTree asks for a fresh tree before opening navigation or fork selection.
func (model interaction) requestTree(purpose TreePurpose) (interaction, *commandIntent) {
	model.treeRequest = mo.Some(purpose)
	model.treeAwaiting = CommandGetSessionTree
	model.treeStatus = ""
	return model.emitCommand(treeCommand(CommandGetSessionTree, TreeCommand{
		TargetEntryID: mo.None[string](), SummaryMode: SummaryModeUnspecified,
		CustomFocus: mo.None[string](), Label: mo.None[string](),
	}))
}

// updateTreeSlashCommand routes tree commands before ordinary editor submission.
func (model interaction) updateTreeSlashCommand(
	text string,
	availability Availability,
) (interaction, *commandIntent, bool) {
	if text != slashCommandTree && text != slashCommandFork && text != slashCommandClone {
		return model, nil, false
	}
	if availability != AvailabilityIdle {
		return model, nil, true
	}
	switch text {
	case slashCommandTree:
		updated, command := model.requestTree(TreePurposeNavigate)
		return updated, command, true
	case slashCommandFork:
		updated, command := model.requestTree(TreePurposeFork)
		return updated, command, true
	case slashCommandClone:
		model.treeAwaiting = CommandCloneSession
		updated, command := model.emitCommand(
			treeCommand(CommandCloneSession, TreeCommand{
				TargetEntryID: mo.None[string](), SummaryMode: SummaryModeUnspecified,
				CustomFocus: mo.None[string](), Label: mo.None[string](),
			}),
		)
		return updated, command, true
	default:
		return model, nil, false
	}
}

// confirmTreeSelection opens summary choice or sends a fork command.
func (model interaction) confirmTreeSelection(panel treePanel) (interaction, *commandIntent) {
	entry, present := selectedTreeEntry(panel)
	if !present {
		return model, nil
	}
	if panel.Purpose == TreePurposeFork {
		if entry.Kind != TreeEntryUser {
			return model, nil
		}
		model.treeAwaiting = CommandForkSession
		return model.emitCommand(treeCommand(CommandForkSession, TreeCommand{
			TargetEntryID: mo.Some(entry.ID), SummaryMode: SummaryModeUnspecified,
			CustomFocus: mo.None[string](), Label: mo.None[string](),
		}))
	}
	model.treeMode = TreeSummary
	model.treeSummaryIndex = 0
	return model, nil
}

// confirmTreeInput validates custom focus or sends one persistent label mutation.
func (model interaction) confirmTreeInput() (interaction, *commandIntent) {
	if model.treeMode == TreeCustomFocus {
		if strings.TrimSpace(string(model.treeInput)) == "" {
			return model, nil
		}
		return model.emitNavigation(SummaryModeCustomFocus, mo.Some(string(model.treeInput)))
	}
	panel, present := model.treePanel.Get()
	if !present {
		return model.closeTree(), nil
	}
	entry, selectedPresent := selectedTreeEntry(panel)
	if !selectedPresent {
		return model, nil
	}
	model.treeAwaiting = CommandSetEntryLabel
	return model.emitCommand(treeCommand(CommandSetEntryLabel, TreeCommand{
		TargetEntryID: mo.Some(entry.ID), SummaryMode: SummaryModeUnspecified,
		CustomFocus: mo.None[string](), Label: mo.Some(string(model.treeInput)),
	}))
}

// emitNavigation sends one selected target and summary mode.
func (model interaction) emitNavigation(mode SummaryMode, focus mo.Option[string]) (interaction, *commandIntent) {
	panel, present := model.treePanel.Get()
	if !present {
		return model.closeTree(), nil
	}
	entry, selectedPresent := selectedTreeEntry(panel)
	if !selectedPresent {
		return model, nil
	}
	model.treeAwaiting = CommandNavigateSessionTree
	return model.emitCommand(treeCommand(CommandNavigateSessionTree, TreeCommand{
		TargetEntryID: mo.Some(entry.ID), SummaryMode: mode, CustomFocus: focus, Label: mo.None[string](),
	}))
}

// applyTreeEvent applies only durable Host results to transcript, editor, session, and label state.
//
//nolint:gocyclo // The closed tree-event union has distinct state transitions.
func (model interaction) applyTreeEvent(kind eventKind, event treeEvent) interaction {
	switch kind {
	case eventSessionTree:
		tree, present := event.Tree.Get()
		if !present {
			return model
		}
		purpose := TreePurposeNavigate
		if requested, requestedPresent := model.treeRequest.Get(); requestedPresent {
			purpose = requested
		}
		panel := newTreePanel(tree, purpose)
		if purpose == TreePurposeFork {
			panel.SetFilter(TreeFilterUserOnly)
		}
		model.treePanel = mo.Some(panel)
		model.treeRequest = mo.None[TreePurpose]()
		model.treeAwaiting = CommandUnspecified
		model.treeMode = TreeSelect
		model.treeStatus = ""
		model.input = nil
		model.cursor = 0
	case eventEntryLabelSet:
		tree, present := event.Tree.Get()
		panel, panelPresent := model.treePanel.Get()
		if present && panelPresent {
			panel.Reconcile(tree)
			model.treePanel = mo.Some(panel)
		}
		model.treeAwaiting = CommandUnspecified
		model.treeMode = TreeSelect
		model.treeInput = nil
		model.treeCursor = 0
	case eventTreeOperationFailed:
		model.treeAwaiting = CommandUnspecified
		model.treeRequest = mo.None[TreePurpose]()
		model.treeStatus = event.FailureMessage.OrElse(treeOperationFailedText)
	case eventSessionTreeNavigationProgress:
		if tree, present := event.Tree.Get(); present {
			if panel, panelPresent := model.treePanel.Get(); panelPresent {
				panel.Reconcile(tree)
				model.treePanel = mo.Some(panel)
			}
		}
		model.replaceTranscript(event.RestoredTranscript)
	case eventSessionTreeNavigation:
		model = model.applyTreeNavigationResult(event)
	case eventSessionEntryAdded:
		if entry, present := event.AddedEntry.Get(); present {
			if panel, panelPresent := model.treePanel.Get(); panelPresent {
				tree := panel.Tree
				tree.Entries = append(tree.Entries, entry)
				tree.ActiveLeafID = mo.Some(entry.ID)
				panel.Reconcile(tree)
				model.treePanel = mo.Some(panel)
			}
		}
		model.state.Transcript = append(model.state.Transcript, event.RestoredTranscript...)
		model.projectionChanged = true
	case eventSessionForked, eventSessionCloned:
		model = model.applyTreeReplacement(event)
	case eventUnspecified, eventInitialization,
		eventUserSubmitted, eventAvailability,
		eventTurnStarted, eventModelDelta,
		eventModelEnd, eventToolCallPreview,
		eventToolCallFinal, eventToolStarted,
		eventToolProgress, eventToolOutput,
		eventToolEnded, eventToolResult,
		eventTurnEnded, eventAgentSettled,
		eventAuthorization, eventInformation,
		eventError, eventModelSelectionChanged,
		eventSessionList, eventSessionChanged,
		eventSessionInformation:
	}
	return model
}

// applyTreeNavigationResult applies canceled or committed navigation state.
func (model interaction) applyTreeNavigationResult(event treeEvent) interaction {
	model.treeAwaiting = CommandUnspecified
	model.treeStatus = formatOperationIssues(event.Issues)
	switch event.NavigationStatus {
	case TreeNavigationCommitted:
		model.setExactNextInput(event.NextInput)
		return model.closeTree()
	case TreeNavigationCanceled:
		model.treeMode = TreeSelect
	case TreeNavigationUnspecified:
	}
	return model
}

// applyTreeReplacement applies one durable fork or clone result.
func (model interaction) applyTreeReplacement(event treeEvent) interaction {
	model.treeAwaiting = CommandUnspecified
	model.replaceTranscript(event.RestoredTranscript)
	if info, present := event.SessionInfo.Get(); present {
		model.state.SessionInfo = mo.Some(info)
	}
	model.setExactNextInput(event.NextInput)
	return model.closeTree()
}

// replaceTranscript publishes one durable active branch and clears transient blocks.
func (model *interaction) replaceTranscript(transcript []Line) {
	model.projectionChanged = true
	model.state.Transcript = slices.Clone(transcript)
	model.state.ActiveModel = make(map[int]ActiveModelContent)
	model.state.ActiveToolCalls = make(map[string]ToolCallState)
	model.state.ActiveTools = make(map[string]string)
}

// setExactNextInput places exact optional text at the end of the editor without submission.
func (model *interaction) setExactNextInput(nextInput mo.Option[string]) {
	text := nextInput.OrEmpty()
	model.input = []rune(text)
	model.cursor = len(model.input)
}

// closeTree removes all entry references from local interaction state.
func (model interaction) closeTree() interaction {
	model.treePanel = mo.None[treePanel]()
	model.treeRequest = mo.None[TreePurpose]()
	model.treeMode = TreeClosed
	model.treeSummaryIndex = 0
	model.treeInput = nil
	model.treeCursor = 0
	model.treeAwaiting = CommandUnspecified
	return model
}

// selectedTreeEntry returns the selected complete entry.
func selectedTreeEntry(panel treePanel) (TreeEntry, bool) {
	selectedID, present := panel.SelectedID.Get()
	if !present {
		return TreeEntry{}, false
	}
	return lo.Find(panel.Tree.Entries, func(entry TreeEntry) bool { return entry.ID == selectedID })
}

// treeCommand creates one complete presentation command.
func treeCommand(
	kind CommandKind,
	payload TreeCommand,
) Command {
	return Command{
		Kind: kind, Text: mo.None[string](), ProviderID: mo.None[string](), ModelID: mo.None[string](),
		ReasoningChoice: mo.None[ReasoningChoice](), SessionID: mo.None[string](),
		SessionName: mo.None[string](), TreeCommand: mo.Some(payload),
	}
}
