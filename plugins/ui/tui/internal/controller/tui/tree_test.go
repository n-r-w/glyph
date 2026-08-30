package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestTreeForkAndCloneCommandsFollowDocumentedEntryFlows verifies slash-command routing.
func TestTreeForkAndCloneCommandsFollowDocumentedEntryFlows(t *testing.T) {
	t.Parallel()

	// Arrange an idle model with command capture.
	var commands []presentationdomain.Command
	model := newTestModel(t, presentationdomain.AvailabilityIdle, func(command presentationdomain.Command) error {
		commands = append(commands, command)
		return nil
	})

	// Act by entering /tree and applying the returned Host snapshot.
	model.input = []rune("/tree")
	model.cursor = len(model.input)
	model = executeCommand(t, model, tea.KeyPressMsg(testKey(tea.KeyEnter)))
	model = updateModel(t, model, treeControllerEvent(presentationdomain.EventSessionTree, presentationdomain.TreeEvent{
		Tree: mo.Some(controllerTree()), NavigationStatus: presentationdomain.TreeNavigationUnspecified,
		SessionInfo: mo.None[presentationdomain.SessionInfo](), RestoredTranscript: nil,
		NextInput: mo.None[string](), Issues: nil, FailureMessage: mo.None[string](),
	}))

	// Assert /tree requested a snapshot without submitting and opened navigation selection.
	require.Len(t, commands, 1)
	require.Equal(t, presentationdomain.CommandGetSessionTree, commands[0].Kind)
	require.Equal(t, treeInteractionSelect, model.treeMode)
	panel, present := model.treePanel.Get()
	require.True(t, present)
	require.Equal(t, presentationdomain.TreePurposeNavigate, panel.Purpose)
	require.Empty(t, model.input)

	// Act by closing the tree, entering /fork, and applying a fresh snapshot.
	model = updateModel(t, model, tea.KeyPressMsg(testKey(tea.KeyEscape)))
	model.input = []rune("/fork")
	model.cursor = len(model.input)
	model = executeCommand(t, model, tea.KeyPressMsg(testKey(tea.KeyEnter)))
	model = updateModel(t, model, treeControllerEvent(presentationdomain.EventSessionTree, presentationdomain.TreeEvent{
		Tree: mo.Some(controllerTree()), NavigationStatus: presentationdomain.TreeNavigationUnspecified,
		SessionInfo: mo.None[presentationdomain.SessionInfo](), RestoredTranscript: nil,
		NextInput: mo.None[string](), Issues: nil, FailureMessage: mo.None[string](),
	}))

	// Assert /fork also refreshes the tree and opens user-only target selection.
	require.Len(t, commands, 2)
	require.Equal(t, presentationdomain.CommandGetSessionTree, commands[1].Kind)
	panel, present = model.treePanel.Get()
	require.True(t, present)
	require.Equal(t, presentationdomain.TreePurposeFork, panel.Purpose)
	require.Equal(t, presentationdomain.TreeFilterUserOnly, panel.Filter)

	// Act by closing the selector and entering /clone.
	model = updateModel(t, model, tea.KeyPressMsg(testKey(tea.KeyEscape)))
	model.input = []rune("/clone")
	model.cursor = len(model.input)
	model = executeCommand(t, model, tea.KeyPressMsg(testKey(tea.KeyEnter)))

	// Assert clone immediately sends the typed replacement command.
	require.Len(t, commands, 3)
	require.Equal(t, presentationdomain.CommandCloneSession, commands[2].Kind)
	require.Equal(t, presentationdomain.CommandCloneSession, model.treeAwaiting)

	// Act by typing while the durable clone result is pending.
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Code: 'x', Text: "x", Mod: 0, ShiftedCode: 0, BaseCode: 0, IsRepeat: false,
	}))

	// Assert pending replacement prevents a later draft from being lost on commit.
	require.Equal(t, "/clone", string(model.input))
}

// TestTreeInteractionUsesLocalSearchFiltersFoldingAndDurableLabels verifies tree controls.
func TestTreeInteractionUsesLocalSearchFiltersFoldingAndDurableLabels(t *testing.T) {
	t.Parallel()

	// Arrange an open full-tree selector with command capture.
	var commands []presentationdomain.Command
	model := newTestModel(t, presentationdomain.AvailabilityIdle, func(command presentationdomain.Command) error {
		commands = append(commands, command)
		return nil
	})
	panel := presentationdomain.NewTreePanel(controllerTree(), presentationdomain.TreePurposeNavigate)
	panel.SetFilter(presentationdomain.TreeFilterAll)
	panel.SelectedID = mo.Some("model")
	model.treePanel = mo.Some(panel)
	model.treeMode = treeInteractionSelect

	// Act by typing a search, clearing it, selecting no-tools, and folding the branch.
	model = updateModel(
		t,
		model,
		tea.KeyPressMsg(tea.Key{Code: 'a', Text: "alternate", Mod: 0, ShiftedCode: 0, BaseCode: 0, IsRepeat: false}),
	)
	panel, _ = model.treePanel.Get()
	require.Equal(t, "alternate", panel.Query)
	require.Equal(
		t,
		[]string{"root", "model", "alternate"},
		lo.Map(panel.VisibleRows(), func(row presentationdomain.TreeRow, _ int) string { return row.Entry.ID }),
	)
	model = updateModel(t, model, tea.KeyPressMsg(testKey(tea.KeyEscape)))
	model = updateModel(
		t,
		model,
		tea.KeyPressMsg(tea.Key{Code: 't', Text: "", Mod: tea.ModCtrl, ShiftedCode: 0, BaseCode: 0, IsRepeat: false}),
	)
	panel, _ = model.treePanel.Get()
	require.Equal(t, presentationdomain.TreeFilterNoTools, panel.Filter)
	panel.SetFilter(presentationdomain.TreeFilterAll)
	panel.SelectedID = mo.Some("model")
	model.treePanel = mo.Some(panel)
	model = updateModel(
		t,
		model,
		tea.KeyPressMsg(
			tea.Key{Code: tea.KeyRight, Text: "", Mod: tea.ModCtrl, ShiftedCode: 0, BaseCode: 0, IsRepeat: false},
		),
	)
	model = updateModel(
		t,
		model,
		tea.KeyPressMsg(
			tea.Key{Code: tea.KeyLeft, Text: "", Mod: tea.ModCtrl, ShiftedCode: 0, BaseCode: 0, IsRepeat: false},
		),
	)
	panel, _ = model.treePanel.Get()
	require.Contains(t, panel.Folded, "model")

	// Act by opening label editing, changing the draft, and confirming.
	model = updateModel(
		t,
		model,
		tea.KeyPressMsg(
			tea.Key{Code: 'L', Text: "", Mod: tea.ModShift, ShiftedCode: 'L', BaseCode: 'l', IsRepeat: false},
		),
	)
	require.Equal(t, treeInteractionLabel, model.treeMode)
	model.treeInput = []rune("renamed")
	model.treeCursor = len(model.treeInput)
	model = executeCommand(t, model, tea.KeyPressMsg(testKey(tea.KeyEnter)))

	// Assert the label remains committed-only until the Host returns a tree.
	require.Len(t, commands, 1)
	require.Equal(t, presentationdomain.CommandSetEntryLabel, commands[0].Kind)
	panel, _ = model.treePanel.Get()
	selected, _ := panel.SelectedID.Get()
	selectedRow := slices.IndexFunc(
		panel.VisibleRows(),
		func(row presentationdomain.TreeRow) bool { return row.Entry.ID == selected },
	)
	require.Equal(t, "checkpoint", panel.VisibleRows()[selectedRow].Entry.Label)

	committed := controllerTree()
	committed.Entries[1].Label = "renamed"
	model = updateModel(
		t,
		model,
		treeControllerEvent(presentationdomain.EventEntryLabelSet, presentationdomain.TreeEvent{
			Tree: mo.Some(committed), NavigationStatus: presentationdomain.TreeNavigationUnspecified,
			SessionInfo: mo.None[presentationdomain.SessionInfo](), RestoredTranscript: nil,
			NextInput: mo.None[string](), Issues: nil, FailureMessage: mo.None[string](),
		}),
	)

	// Assert only the durable tree updates the visible label.
	panel, _ = model.treePanel.Get()
	require.Equal(t, "renamed", panel.Tree.Entries[1].Label)
	require.Equal(t, treeInteractionSelect, model.treeMode)
}

// TestTreeViewRendersStructureActiveStateKindsLabelsAndSummary verifies visible tree semantics.
func TestTreeViewRendersStructureActiveStateKindsLabelsAndSummary(t *testing.T) {
	t.Parallel()

	// Arrange an open tree whose active leaf is a labeled branch summary.
	model := newTestModel(t, presentationdomain.AvailabilityIdle, nil)
	model.width = 120
	tree := controllerTree()
	tree.Entries = append(tree.Entries, presentationdomain.TreeEntry{
		ID: "summary", ParentID: mo.Some("model"), CreatedAt: time.Unix(2, 0).UTC(), Label: "summary-label",
		Kind: presentationdomain.TreeEntryBranchSummary, Text: "abandoned decisions",
	})
	tree.ActiveLeafID = mo.Some("summary")
	panel := presentationdomain.NewTreePanel(tree, presentationdomain.TreePurposeNavigate)
	panel.SetFilter(presentationdomain.TreeFilterAll)
	model.treePanel = mo.Some(panel)
	model.treeMode = treeInteractionSelect

	// Act by rendering the tree selector.
	view := model.View().Content

	// Assert structure, active leaf, entry kind, label, and summary text are explicit.
	require.Contains(t, view, "└─")
	require.Contains(t, view, "* leaf")
	require.Contains(t, view, "[summary-label] branch summary: abandoned decisions")
}

// TestTreeNavigationSummaryModesAndCustomFocusValidation verifies target confirmation behavior.
func TestTreeNavigationSummaryModesAndCustomFocusValidation(t *testing.T) {
	t.Parallel()

	// Arrange an open navigation selector with one selected target.
	var commands []presentationdomain.Command
	model := newTestModel(t, presentationdomain.AvailabilityIdle, func(command presentationdomain.Command) error {
		commands = append(commands, command)
		return nil
	})
	panel := presentationdomain.NewTreePanel(controllerTree(), presentationdomain.TreePurposeNavigate)
	panel.SelectedID = mo.Some("alternate")
	model.treePanel = mo.Some(panel)
	model.treeMode = treeInteractionSelect

	// Act by confirming the target and accepting the default summary mode.
	model = updateModel(t, model, tea.KeyPressMsg(testKey(tea.KeyEnter)))
	require.Equal(t, treeInteractionSummary, model.treeMode)
	require.Zero(t, model.treeSummaryIndex)
	model = executeCommand(t, model, tea.KeyPressMsg(testKey(tea.KeyEnter)))

	// Assert No summary is the default typed navigation command.
	require.Len(t, commands, 1)
	commandPayload, present := commands[0].TreeCommand.Get()
	require.True(t, present)
	require.Equal(t, presentationdomain.SummaryModeNoSummary, commandPayload.SummaryMode)

	// Arrange custom mode and act with an empty custom focus.
	model.treeAwaiting = presentationdomain.CommandUnspecified
	model.treeMode = treeInteractionSummary
	model.treeSummaryIndex = 2
	model = updateModel(t, model, tea.KeyPressMsg(testKey(tea.KeyEnter)))
	require.Equal(t, treeInteractionCustomFocus, model.treeMode)
	next, emitted := model.Update(tea.KeyPressMsg(testKey(tea.KeyEnter)))
	model = next.(Model)

	// Assert empty focus emits nothing, while exact nonempty focus is sent.
	require.Nil(t, emitted)
	model.treeInput = []rune(" focus on errors ")
	model.treeCursor = len(model.treeInput)
	model = executeCommand(t, model, tea.KeyPressMsg(testKey(tea.KeyEnter)))
	require.Len(t, commands, 2)
	require.Equal(t, presentationdomain.CommandNavigateSessionTree, model.treeAwaiting)
	commandPayload, present = commands[1].TreeCommand.Get()
	require.True(t, present)
	require.Equal(t, presentationdomain.SummaryModeCustomFocus, commandPayload.SummaryMode)
	require.Equal(t, mo.Some(" focus on errors "), commandPayload.CustomFocus)
}

// TestForkAndCloneResultsReplaceOnlyAfterDurableFrames verifies replacement-session commits.
func TestForkAndCloneResultsReplaceOnlyAfterDurableFrames(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		kind      presentationdomain.EventKind
		nextInput mo.Option[string]
		expected  string
	}{
		{
			name: "fork", kind: presentationdomain.EventSessionForked,
			nextInput: mo.Some(" exact fork input "), expected: " exact fork input ",
		},
		{name: "clone", kind: presentationdomain.EventSessionCloned, nextInput: mo.None[string](), expected: ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// Arrange preceding local state that must survive until the durable frame.
			model := newTestModel(t, presentationdomain.AvailabilityIdle, nil)
			model.state.Transcript = []presentationdomain.Line{controllerLine("old")}
			model.input = []rune("draft")
			model.cursor = len(model.input)
			info := presentationdomain.SessionInfo{
				ID:               "replacement",
				Name:             "",
				NamePresent:      false,
				WorkingDirectory: "/project",
				StoragePath:      "",
				StoragePresent:   false,
				CreatedAt:        time.Unix(1, 0).UTC(),
				UpdatedAt:        time.Unix(2, 0).UTC(),
			}

			// Act by applying the Host-confirmed replacement.
			model = updateModel(t, model, treeControllerEvent(testCase.kind, presentationdomain.TreeEvent{
				Tree:             mo.None[presentationdomain.SessionTree](),
				NavigationStatus: presentationdomain.TreeNavigationUnspecified,
				SessionInfo: mo.Some(
					info,
				),
				RestoredTranscript: []presentationdomain.Line{controllerLine("replacement")},
				NextInput:          testCase.nextInput,
				Issues:             nil,
				FailureMessage:     mo.None[string](),
			}))

			// Assert session, transcript, and exact optional editor state come from the durable frame.
			require.Equal(t, mo.Some(info), model.state.SessionInfo)
			require.Equal(t, []presentationdomain.Line{controllerLine("replacement")}, model.state.Transcript)
			require.Equal(t, testCase.expected, string(model.input))
			require.Equal(t, len([]rune(testCase.expected)), model.cursor)
		})
	}
}

// TestTreeCommandDeliveryFailurePreservesTranscriptAndEditor verifies pre-Host failure safety.
func TestTreeCommandDeliveryFailurePreservesTranscriptAndEditor(t *testing.T) {
	t.Parallel()

	// Arrange existing local state and a failing UI stream send.
	model := newTestModel(t, presentationdomain.AvailabilityIdle, func(presentationdomain.Command) error {
		return errors.New("stream closed")
	})
	model.state.Transcript = []presentationdomain.Line{controllerLine("old")}
	model.input = []rune("/clone")
	model.cursor = len(model.input)
	oldTranscript := slices.Clone(model.state.Transcript)

	// Act by sending the clone command through the failed stream.
	model = executeCommand(t, model, tea.KeyPressMsg(testKey(tea.KeyEnter)))

	// Assert failure changes only safe operation status.
	require.Equal(t, oldTranscript, model.state.Transcript)
	require.Equal(t, "/clone", string(model.input))
	require.Contains(t, model.treeStatus, "stream closed")
}

// TestDurableTreeResultsOwnTranscriptAndExactEditorReplacement verifies commit and failure boundaries.
func TestDurableTreeResultsOwnTranscriptAndExactEditorReplacement(t *testing.T) {
	t.Parallel()

	// Arrange existing transcript, editor, and open tree state.
	model := newTestModel(t, presentationdomain.AvailabilityIdle, nil)
	model.state.Transcript = []presentationdomain.Line{controllerLine("old")}
	model.input = []rune("draft")
	model.cursor = len(model.input)
	model.treePanel = mo.Some(presentationdomain.NewTreePanel(controllerTree(), presentationdomain.TreePurposeNavigate))
	model.treeMode = treeInteractionSelect
	oldTranscript := slices.Clone(model.state.Transcript)

	// Act by applying a rejected operation and canceled navigation with one safe issue.
	model = updateModel(
		t,
		model,
		treeControllerEvent(presentationdomain.EventSessionTreeFailed, presentationdomain.TreeEvent{
			Tree:               mo.None[presentationdomain.SessionTree](),
			NavigationStatus:   presentationdomain.TreeNavigationUnspecified,
			SessionInfo:        mo.None[presentationdomain.SessionInfo](),
			RestoredTranscript: nil,
			NextInput:          mo.None[string](),
			Issues:             nil,
			FailureMessage:     mo.Some("session operation is busy"),
		}),
	)
	require.Equal(t, oldTranscript, model.state.Transcript)
	require.Equal(t, "draft", string(model.input))
	model = updateModel(
		t,
		model,
		treeControllerEvent(presentationdomain.EventSessionTreeNavigation, presentationdomain.TreeEvent{
			Tree:               mo.None[presentationdomain.SessionTree](),
			NavigationStatus:   presentationdomain.TreeNavigationCanceled,
			SessionInfo:        mo.None[presentationdomain.SessionInfo](),
			RestoredTranscript: nil,
			NextInput:          mo.None[string](),
			Issues: []presentationdomain.OperationIssue{
				{Code: "HANDLER_ERROR", ExtensionID: "ext", HandlerID: "handler", Message: "safe issue"},
			},
			FailureMessage: mo.None[string](),
		}),
	)
	require.Equal(t, oldTranscript, model.state.Transcript)
	require.Equal(t, "draft", string(model.input))
	require.Contains(t, model.View().Content, "safe issue")

	// Act by applying a committed navigation with exact optional next input.
	model = updateModel(
		t,
		model,
		treeControllerEvent(presentationdomain.EventSessionTreeNavigation, presentationdomain.TreeEvent{
			Tree:               mo.Some(controllerTree()),
			NavigationStatus:   presentationdomain.TreeNavigationCommitted,
			SessionInfo:        mo.None[presentationdomain.SessionInfo](),
			RestoredTranscript: []presentationdomain.Line{controllerLine("new")},
			NextInput:          mo.Some(" exact next input "),
			Issues:             nil,
			FailureMessage:     mo.None[string](),
		}),
	)

	// Assert durable state replaces transcript and editor without submitting the next input.
	require.Equal(t, []presentationdomain.Line{controllerLine("new")}, model.state.Transcript)
	require.Equal(t, " exact next input ", string(model.input))
	require.Equal(t, len([]rune(" exact next input ")), model.cursor)
	require.Equal(t, treeInteractionClosed, model.treeMode)
	require.True(t, model.treePanel.IsNone())
}

// TestLinearTreeRenderingOmitsAncestorVerticals verifies a chain does not imply sibling continuation.
func TestLinearTreeRenderingOmitsAncestorVerticals(t *testing.T) {
	t.Parallel()

	// Arrange a visible three-entry linear branch.
	model := newTestModel(t, presentationdomain.AvailabilityIdle, nil)
	tree := controllerTree()
	tree.Entries = tree.Entries[:3]
	panel := presentationdomain.NewTreePanel(tree, presentationdomain.TreePurposeNavigate)
	panel.SetFilter(presentationdomain.TreeFilterAll)
	model.treePanel = mo.Some(panel)
	model.treeMode = treeInteractionSelect
	model.width = 200

	// Act by rendering the complete selector.
	lines := model.treeSelectorLines()

	// Assert depth alone does not produce an ancestor continuation line.
	require.NotContains(t, strings.Join(lines, "\n"), "│")
}

// TestTreeRenderingKeepsMultilineEntryTextOnOneRow verifies content cannot break selector topology.
func TestTreeRenderingKeepsMultilineEntryTextOnOneRow(t *testing.T) {
	t.Parallel()

	// Arrange a tree entry containing multiline tool output.
	model := newTestModel(t, presentationdomain.AvailabilityIdle, nil)
	tree := controllerTree()
	tree.Entries[2].Text = "read output\nnext line"
	panel := presentationdomain.NewTreePanel(tree, presentationdomain.TreePurposeNavigate)
	panel.SetFilter(presentationdomain.TreeFilterAll)
	model.treePanel = mo.Some(panel)
	model.treeMode = treeInteractionSelect
	model.width = 200

	// Act by rendering the selector rows.
	lines := model.treeSelectorLines()

	// Assert dynamic content is normalized without changing row indentation.
	require.NotContains(t, lines[3], "\n")
	require.Contains(t, lines[3], "read output next line")
}

// TestBranchedTreeRenderingUsesVisibleSiblingTopology verifies terminal branch glyph placement.
func TestBranchedTreeRenderingUsesVisibleSiblingTopology(t *testing.T) {
	t.Parallel()

	// Arrange a first root child with one descendant and a later root child.
	model := newTestModel(t, presentationdomain.AvailabilityIdle, nil)
	tree := controllerTree()
	tree.Entries[3].ParentID = mo.Some("root")
	panel := presentationdomain.NewTreePanel(tree, presentationdomain.TreePurposeNavigate)
	panel.SetFilter(presentationdomain.TreeFilterAll)
	model.treePanel = mo.Some(panel)
	model.treeMode = treeInteractionSelect
	model.width = 200

	// Act by rendering the branched selector.
	lines := strings.Join(model.treeSelectorLines(), "\n")

	// Assert only visible following siblings produce middle and ancestor continuations.
	require.Contains(t, lines, "├─")
	require.Contains(t, lines, "│  └─")
	require.Contains(t, lines, "└─")
}

// TestTreeSelectionMovementPreservesVisibleTopology verifies navigation does not alter branch structure.
func TestTreeSelectionMovementPreservesVisibleTopology(t *testing.T) {
	t.Parallel()

	// Arrange selection on the last row of a branched visible tree.
	model := newTestModel(t, presentationdomain.AvailabilityIdle, nil)
	tree := controllerTree()
	tree.Entries[3].ParentID = mo.Some("root")
	panel := presentationdomain.NewTreePanel(tree, presentationdomain.TreePurposeNavigate)
	panel.SetFilter(presentationdomain.TreeFilterAll)
	panel.SelectedID = mo.Some("alternate")
	model.treePanel = mo.Some(panel)
	model.treeMode = treeInteractionSelect
	model.width = 200
	before := lo.Map(model.treeSelectorLines(), withoutTreeSelectionPrefix)

	// Act by moving selection to the preceding visible entry.
	model = updateModel(t, model, tea.KeyPressMsg(testKey(tea.KeyUp)))
	after := lo.Map(model.treeSelectorLines(), withoutTreeSelectionPrefix)

	// Assert selection changed while every rendered topology glyph stayed fixed.
	panel, present := model.treePanel.Get()
	require.True(t, present)
	require.Equal(t, mo.Some("tool"), panel.SelectedID)
	require.Equal(t, before, after)
}

// withoutTreeSelectionPrefix removes only the local selector marker for topology comparison.
func withoutTreeSelectionPrefix(line string, _ int) string {
	if after, ok := strings.CutPrefix(line, activeSelectorPrefix); ok {
		return after
	}
	return strings.TrimPrefix(line, inactiveSelectorPrefix)
}

// controllerTree creates a branch with every entry type needed by controller tests.
func controllerTree() presentationdomain.SessionTree {
	createdAt := time.Unix(1, 0).UTC()
	return presentationdomain.SessionTree{
		Entries: []presentationdomain.TreeEntry{
			{
				ID:        "root",
				ParentID:  mo.None[string](),
				CreatedAt: createdAt,
				Label:     "",
				Kind:      presentationdomain.TreeEntryUser,
				Text:      "root prompt",
			},
			{
				ID:        "model",
				ParentID:  mo.Some("root"),
				CreatedAt: createdAt,
				Label:     "checkpoint",
				Kind:      presentationdomain.TreeEntryModel,
				Text:      "model answer",
			},
			{
				ID:        "tool",
				ParentID:  mo.Some("model"),
				CreatedAt: createdAt,
				Label:     "",
				Kind:      presentationdomain.TreeEntryToolResult,
				Text:      "read output",
			},
			{
				ID:        "alternate",
				ParentID:  mo.Some("model"),
				CreatedAt: createdAt,
				Label:     "",
				Kind:      presentationdomain.TreeEntryUser,
				Text:      "alternate prompt",
			},
		},
		ActiveLeafID: mo.Some("tool"),
	}
}

// controllerLine creates one complete transcript line.
func controllerLine(text string) presentationdomain.Line {
	return presentationdomain.Line{
		Kind:     presentationdomain.LineModel,
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Text:     mo.Some(text),
		Contents: mo.None[[]presentationdomain.Content](),
	}
}

// treeControllerEvent creates one complete tree event fixture.
func treeControllerEvent(
	kind presentationdomain.EventKind,
	treeEvent presentationdomain.TreeEvent,
) presentationdomain.Event {
	return presentationdomain.Event{
		Kind:                 kind,
		Startup:              nil,
		RestoredTranscript:   nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Text:                 mo.None[string](),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
		TreeEvent:            mo.Some(treeEvent),
	}
}
