//go:build !integration

package presentation

import (
	"errors"
	"slices"
	"testing"
	"time"

	inputcontroller "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
)

// TestSessionEntryAddedRetainsHiddenTreeStateWithoutTranscriptRendering verifies TUI connection-event projection.
func TestSessionEntryAddedRetainsHiddenTreeStateWithoutTranscriptRendering(t *testing.T) {
	t.Parallel()

	// Arrange an open tree panel and existing ordinary transcript.
	model := newTestModel(t, AvailabilityIdle, func(Command) error { return nil })
	panel := newTreePanel(controllerTree(), TreePurposeNavigate)
	model.model.treePanel = mo.Some(panel)
	model.model.state.Transcript = []Line{controllerLine("existing")}
	entry := TreeEntry{
		ID: "message", ParentID: mo.Some("root"), CreatedAt: time.Unix(10, 0).UTC(), Label: "",
		Kind: TreeEntryExtensionMessage, Text: "hidden text",
		ExtensionMessage: mo.Some(ExtensionMessage{
			ExtensionID: "example", EntryType: "note", Text: "hidden text",
			Visibility: ClientVisibilityHidden,
		}),
	}

	// Act by applying one hidden-client committed entry connection event.
	model = updateModel(t, model, treeControllerEvent(
		eventSessionEntryAdded,
		treeEvent{
			Tree:             mo.None[SessionTree](),
			NavigationStatus: TreeNavigationUnspecified,
			SessionInfo:      mo.None[SessionInfo](), RestoredTranscript: nil,
			NextInput: mo.None[string](), Issues: nil, FailureMessage: mo.None[string](), AddedEntry: mo.Some(entry),
		},
	))

	// Assert complete tree state advances while ordinary transcript remains unchanged.
	updated := model.model.treePanel.MustGet()
	require.Equal(t, mo.Some("message"), updated.Tree.ActiveLeafID)
	require.Equal(t, "hidden text", updated.Tree.Entries[len(updated.Tree.Entries)-1].ExtensionMessage.MustGet().Text)
	require.Equal(t, []Line{controllerLine("existing")}, model.model.state.Transcript)
}

// TestTreeForkAndCloneCommandsFollowDocumentedEntryFlows verifies slash-command routing.
func TestTreeForkAndCloneCommandsFollowDocumentedEntryFlows(t *testing.T) {
	t.Parallel()

	// Arrange an idle model with command capture.
	var commands []Command
	model := newTestModel(t, AvailabilityIdle, func(command Command) error {
		commands = append(commands, command)
		return nil
	})

	// Act by entering /tree and applying the returned Host snapshot.
	model.model.input = []rune("/tree")
	model.model.cursor = len(model.model.input)
	model = executeCommand(t, model, testKey(inputcontroller.KeyEnter))
	model = updateModel(t, model, treeControllerEvent(eventSessionTree, treeEvent{
		Tree:               mo.Some(controllerTree()),
		NavigationStatus:   TreeNavigationUnspecified,
		SessionInfo:        mo.None[SessionInfo](),
		RestoredTranscript: nil,
		NextInput:          mo.None[string](),
		Issues:             nil,
		FailureMessage:     mo.None[string](),
		AddedEntry:         mo.None[TreeEntry](),
	}))

	// Assert /tree requested a snapshot without submitting and opened navigation selection.
	require.Len(t, commands, 1)
	require.Equal(t, CommandGetSessionTree, commands[0].Kind)
	require.Equal(t, TreeSelect, model.model.treeMode)
	panel, present := model.model.treePanel.Get()
	require.True(t, present)
	require.Equal(t, TreePurposeNavigate, panel.Purpose)
	require.Empty(t, model.model.input)

	// Act by closing the tree, entering /fork, and applying a fresh snapshot.
	model = updateModel(t, model, testKey(inputcontroller.KeyEscape))
	model.model.input = []rune("/fork")
	model.model.cursor = len(model.model.input)
	model = executeCommand(t, model, testKey(inputcontroller.KeyEnter))
	model = updateModel(t, model, treeControllerEvent(eventSessionTree, treeEvent{
		Tree:               mo.Some(controllerTree()),
		NavigationStatus:   TreeNavigationUnspecified,
		SessionInfo:        mo.None[SessionInfo](),
		RestoredTranscript: nil,
		NextInput:          mo.None[string](),
		Issues:             nil,
		FailureMessage:     mo.None[string](),
		AddedEntry:         mo.None[TreeEntry](),
	}))

	// Assert /fork also refreshes the tree and opens user-only target selection.
	require.Len(t, commands, 2)
	require.Equal(t, CommandGetSessionTree, commands[1].Kind)
	panel, present = model.model.treePanel.Get()
	require.True(t, present)
	require.Equal(t, TreePurposeFork, panel.Purpose)
	require.Equal(t, TreeFilterUserOnly, panel.Filter)

	// Act by closing the selector and entering /clone.
	model = updateModel(t, model, testKey(inputcontroller.KeyEscape))
	model.model.input = []rune("/clone")
	model.model.cursor = len(model.model.input)
	model = executeCommand(t, model, testKey(inputcontroller.KeyEnter))

	// Assert clone immediately sends the typed replacement command.
	require.Len(t, commands, 3)
	require.Equal(t, CommandCloneSession, commands[2].Kind)
	require.Equal(t, CommandCloneSession, model.model.treeAwaiting)

	// Act by typing while the durable clone result is pending.
	model = updateModel(t, model, inputcontroller.Key{
		Code: 'x', Text: "x", Mod: 0,
	})

	// Assert pending replacement prevents a later draft from being lost on commit.
	require.Equal(t, "/clone", string(model.model.input))
}

// TestTreeInteractionUsesLocalSearchFiltersFoldingAndDurableLabels verifies tree controls.
func TestTreeInteractionUsesLocalSearchFiltersFoldingAndDurableLabels(t *testing.T) {
	t.Parallel()

	// Arrange an open full-tree selector with command capture.
	var commands []Command
	model := newTestModel(t, AvailabilityIdle, func(command Command) error {
		commands = append(commands, command)
		return nil
	})
	panel := newTreePanel(controllerTree(), TreePurposeNavigate)
	panel.SetFilter(TreeFilterAll)
	panel.SelectedID = mo.Some("model")
	model.model.treePanel = mo.Some(panel)
	model.model.treeMode = TreeSelect

	// Act by typing a search, clearing it, selecting no-tools, and folding the branch.
	model = updateModel(
		t,
		model,
		inputcontroller.Key{Code: 'a', Text: "alternate", Mod: 0},
	)
	panel, _ = model.model.treePanel.Get()
	require.Equal(t, "alternate", panel.Query)
	require.Equal(
		t,
		[]string{"root", "model", "alternate"},
		lo.Map(panel.visibleEntries(), func(row VisibleEntry, _ int) string { return row.Entry.ID }),
	)
	model = updateModel(t, model, testKey(inputcontroller.KeyEscape))
	model = updateModel(
		t,
		model,
		inputcontroller.Key{Code: 't', Text: "", Mod: inputcontroller.ModCtrl},
	)
	panel, _ = model.model.treePanel.Get()
	require.Equal(t, TreeFilterNoTools, panel.Filter)
	panel.SetFilter(TreeFilterAll)
	panel.SelectedID = mo.Some("model")
	model.model.treePanel = mo.Some(panel)
	model = updateModel(
		t,
		model,

		inputcontroller.Key{Code: inputcontroller.KeyRight, Text: "", Mod: inputcontroller.ModCtrl},
	)
	model = updateModel(
		t,
		model,

		inputcontroller.Key{Code: inputcontroller.KeyLeft, Text: "", Mod: inputcontroller.ModCtrl},
	)
	panel, _ = model.model.treePanel.Get()
	require.Contains(t, panel.Folded, "model")

	// Act by opening label editing, changing the draft, and confirming.
	model = updateModel(
		t,
		model,

		inputcontroller.Key{Code: 'L', Text: "", Mod: inputcontroller.ModShift},
	)
	require.Equal(t, TreeLabel, model.model.treeMode)
	model.model.treeInput = []rune("renamed")
	model.model.treeCursor = len(model.model.treeInput)
	model = executeCommand(t, model, testKey(inputcontroller.KeyEnter))

	// Assert the label remains committed-only until the Host returns a tree.
	require.Len(t, commands, 1)
	require.Equal(t, CommandSetEntryLabel, commands[0].Kind)
	panel, _ = model.model.treePanel.Get()
	selected, _ := panel.SelectedID.Get()
	selectedRow := slices.IndexFunc(
		panel.visibleEntries(),
		func(row VisibleEntry) bool { return row.Entry.ID == selected },
	)
	require.Equal(t, "checkpoint", panel.visibleEntries()[selectedRow].Entry.Label)

	committed := controllerTree()
	committed.Entries[1].Label = "renamed"
	model = updateModel(
		t,
		model,
		treeControllerEvent(eventEntryLabelSet, treeEvent{
			Tree:               mo.Some(committed),
			NavigationStatus:   TreeNavigationUnspecified,
			SessionInfo:        mo.None[SessionInfo](),
			RestoredTranscript: nil,
			NextInput:          mo.None[string](),
			Issues:             nil,
			FailureMessage:     mo.None[string](),
			AddedEntry:         mo.None[TreeEntry](),
		}),
	)

	// Assert only the durable tree updates the visible label.
	panel, _ = model.model.treePanel.Get()
	require.Equal(t, "renamed", panel.Tree.Entries[1].Label)
	require.Equal(t, TreeSelect, model.model.treeMode)
}

// TestTreeNavigationSummaryModesAndCustomFocusValidation verifies target confirmation behavior.
func TestTreeNavigationSummaryModesAndCustomFocusValidation(t *testing.T) {
	t.Parallel()

	// Arrange an open navigation selector with one selected target.
	var commands []Command
	model := newTestModel(t, AvailabilityIdle, func(command Command) error {
		commands = append(commands, command)
		return nil
	})
	panel := newTreePanel(controllerTree(), TreePurposeNavigate)
	panel.SelectedID = mo.Some("alternate")
	model.model.treePanel = mo.Some(panel)
	model.model.treeMode = TreeSelect

	// Act by confirming the target and accepting the default summary mode.
	model = updateModel(t, model, testKey(inputcontroller.KeyEnter))
	require.Equal(t, TreeSummary, model.model.treeMode)
	require.Zero(t, model.model.treeSummaryIndex)
	model = executeCommand(t, model, testKey(inputcontroller.KeyEnter))

	// Assert No summary is the default typed navigation command.
	require.Len(t, commands, 1)
	commandPayload, present := commands[0].TreeCommand.Get()
	require.True(t, present)
	require.Equal(t, SummaryModeNoSummary, commandPayload.SummaryMode)

	// Arrange custom mode and act with an empty custom focus.
	model.model.treeAwaiting = CommandUnspecified
	model.model.treeMode = TreeSummary
	model.model.treeSummaryIndex = 2
	model = updateModel(t, model, testKey(inputcontroller.KeyEnter))
	require.Equal(t, TreeCustomFocus, model.model.treeMode)
	next, emitted := updateApplication(model, testKey(inputcontroller.KeyEnter))
	model = next

	// Assert empty focus emits nothing, while exact nonempty focus is sent.
	require.Nil(t, emitted)
	model.model.treeInput = []rune(" focus on errors ")
	model.model.treeCursor = len(model.model.treeInput)
	model = executeCommand(t, model, testKey(inputcontroller.KeyEnter))
	require.Len(t, commands, 2)
	require.Equal(t, CommandNavigateSessionTree, model.model.treeAwaiting)
	commandPayload, present = commands[1].TreeCommand.Get()
	require.True(t, present)
	require.Equal(t, SummaryModeCustomFocus, commandPayload.SummaryMode)
	require.Equal(t, mo.Some(" focus on errors "), commandPayload.CustomFocus)
}

// TestForkAndCloneResultsReplaceOnlyAfterDurableFrames verifies replacement-session commits.
func TestForkAndCloneResultsReplaceOnlyAfterDurableFrames(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		kind      eventKind
		nextInput mo.Option[string]
		expected  string
	}{
		{
			name: "fork", kind: eventSessionForked,
			nextInput: mo.Some(" exact fork input "), expected: " exact fork input ",
		},
		{name: "clone", kind: eventSessionCloned, nextInput: mo.None[string](), expected: ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// Arrange preceding local state that must survive until the durable frame.
			model := newTestModel(t, AvailabilityIdle, nil)
			model.model.state.Transcript = []Line{controllerLine("old")}
			model.model.input = []rune("draft")
			model.model.cursor = len(model.model.input)
			info := SessionInfo{
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
			model = updateModel(t, model, treeControllerEvent(testCase.kind, treeEvent{
				Tree:             mo.None[SessionTree](),
				NavigationStatus: TreeNavigationUnspecified,
				SessionInfo: mo.Some(
					info,
				),
				RestoredTranscript: []Line{controllerLine("replacement")},
				NextInput:          testCase.nextInput,
				Issues:             nil,
				FailureMessage:     mo.None[string](), AddedEntry: mo.None[TreeEntry](),
			}))

			// Assert session, transcript, and exact optional editor state come from the durable frame.
			require.Equal(t, mo.Some(info), model.model.state.SessionInfo)
			require.Equal(t, []Line{controllerLine("replacement")}, model.model.state.Transcript)
			require.Equal(t, testCase.expected, string(model.model.input))
			require.Equal(t, len([]rune(testCase.expected)), model.model.cursor)
		})
	}
}

// TestTreeCommandDeliveryFailurePreservesTranscriptAndEditor verifies pre-Host failure safety.
func TestTreeCommandDeliveryFailurePreservesTranscriptAndEditor(t *testing.T) {
	t.Parallel()

	// Arrange existing local state and a failing UI stream send.
	model := newTestModel(t, AvailabilityIdle, func(Command) error {
		return errors.New("stream closed")
	})
	model.model.state.Transcript = []Line{controllerLine("old")}
	model.model.input = []rune("/clone")
	model.model.cursor = len(model.model.input)
	oldTranscript := slices.Clone(model.model.state.Transcript)

	// Act by sending the clone command through the failed stream.
	model = executeCommand(t, model, testKey(inputcontroller.KeyEnter))

	// Assert failure changes only safe operation status.
	require.Equal(t, oldTranscript, model.model.state.Transcript)
	require.Equal(t, "/clone", string(model.model.input))
	require.Contains(t, model.model.treeStatus, "stream closed")
}

// TestNavigationProgressOwnsTranscriptAndTerminalOwnsExactEditorReplacement verifies commit and completion boundaries.
func TestNavigationProgressOwnsTranscriptAndTerminalOwnsExactEditorReplacement(t *testing.T) {
	t.Parallel()

	// Arrange existing transcript, editor, and open tree state.
	model := newTestModel(t, AvailabilityIdle, nil)
	model.model.state.Transcript = []Line{controllerLine("old")}
	model.model.input = []rune("draft")
	model.model.cursor = len(model.model.input)
	model.model.treePanel = mo.Some(newTreePanel(controllerTree(), TreePurposeNavigate))
	model.model.treeMode = TreeSelect
	oldTranscript := slices.Clone(model.model.state.Transcript)

	// Act by applying a rejected operation and canceled navigation with one safe issue.
	model = updateModel(
		t,
		model,
		treeControllerEvent(eventTreeOperationFailed, treeEvent{
			Tree:               mo.None[SessionTree](),
			NavigationStatus:   TreeNavigationUnspecified,
			SessionInfo:        mo.None[SessionInfo](),
			RestoredTranscript: nil,
			NextInput:          mo.None[string](),
			Issues:             nil,
			FailureMessage:     mo.Some("session operation is busy"), AddedEntry: mo.None[TreeEntry](),
		}),
	)
	require.Equal(t, oldTranscript, model.model.state.Transcript)
	require.Equal(t, "draft", string(model.model.input))
	model = updateModel(
		t,
		model,
		treeControllerEvent(eventSessionTreeNavigation, treeEvent{
			Tree:               mo.None[SessionTree](),
			NavigationStatus:   TreeNavigationCanceled,
			SessionInfo:        mo.None[SessionInfo](),
			RestoredTranscript: nil,
			NextInput:          mo.None[string](),
			Issues: []OperationIssue{
				{Code: "HANDLER_ERROR", ExtensionID: "ext", HandlerID: "handler", Message: "safe issue"},
			},
			FailureMessage: mo.None[string](), AddedEntry: mo.None[TreeEntry](),
		}),
	)
	require.Equal(t, oldTranscript, model.model.state.Transcript)
	require.Equal(t, "draft", string(model.model.input))
	require.Contains(t, model.model.treeStatus, "safe issue")

	// Act by applying committed progress, then terminal metadata with exact optional next input.
	model = updateModel(
		t,
		model,
		treeControllerEvent(eventSessionTreeNavigationProgress, treeEvent{
			Tree:               mo.Some(controllerTree()),
			NavigationStatus:   TreeNavigationUnspecified,
			SessionInfo:        mo.None[SessionInfo](),
			RestoredTranscript: []Line{controllerLine("new")},
			NextInput:          mo.None[string](),
			Issues:             nil,
			FailureMessage:     mo.None[string](),
			AddedEntry:         mo.None[TreeEntry](),
		}),
	)
	model = updateModel(
		t,
		model,
		treeControllerEvent(eventSessionTreeNavigation, treeEvent{
			Tree:               mo.None[SessionTree](),
			NavigationStatus:   TreeNavigationCommitted,
			SessionInfo:        mo.None[SessionInfo](),
			RestoredTranscript: nil,
			NextInput:          mo.Some(" exact next input "),
			Issues:             nil,
			FailureMessage:     mo.None[string](), AddedEntry: mo.None[TreeEntry](),
		}),
	)

	// Assert progress replaces transcript and completion edits without submitting the next input.
	require.Equal(t, []Line{controllerLine("new")}, model.model.state.Transcript)
	require.Equal(t, " exact next input ", string(model.model.input))
	require.Equal(t, len([]rune(" exact next input ")), model.model.cursor)
	require.Equal(t, TreeClosed, model.model.treeMode)
	require.True(t, model.model.treePanel.IsNone())
}

// controllerTree creates a branch with every entry type needed by controller tests.
func controllerTree() SessionTree {
	createdAt := time.Unix(1, 0).UTC()
	return SessionTree{
		Entries: []TreeEntry{
			{
				ID:               "root",
				ParentID:         mo.None[string](),
				CreatedAt:        createdAt,
				Label:            "",
				Kind:             TreeEntryUser,
				ExtensionMessage: mo.None[ExtensionMessage](),
				Text:             "root prompt",
			},
			{
				ID:               "model",
				ParentID:         mo.Some("root"),
				CreatedAt:        createdAt,
				Label:            "checkpoint",
				Kind:             TreeEntryModel,
				ExtensionMessage: mo.None[ExtensionMessage](),
				Text:             "model answer",
			},
			{
				ID:               "tool",
				ParentID:         mo.Some("model"),
				CreatedAt:        createdAt,
				Label:            "",
				Kind:             TreeEntryToolResult,
				ExtensionMessage: mo.None[ExtensionMessage](),
				Text:             "read output",
			},
			{
				ID:               "alternate",
				ParentID:         mo.Some("model"),
				CreatedAt:        createdAt,
				Label:            "",
				Kind:             TreeEntryUser,
				ExtensionMessage: mo.None[ExtensionMessage](),
				Text:             "alternate prompt",
			},
		},
		ActiveLeafID: mo.Some("tool"),
	}
}

// controllerLine creates one complete transcript line.
func controllerLine(text string) Line {
	return Line{
		Kind:     LineModel,
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Text:     mo.Some(text),
		Contents: mo.None[[]Content](),
	}
}

// treeControllerEvent creates one complete tree event fixture.
func treeControllerEvent(
	kind eventKind,
	treeEvent treeEvent,
) event {
	return event{
		FailureCode:          "",
		Kind:                 kind,
		Startup:              nil,
		RestoredTranscript:   nil,
		Availability:         mo.None[Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[OutputStream](),
		Text:                 mo.None[string](),
		Contents:             mo.None[[]Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[ModelSelection](),
		SessionInfo:          mo.None[SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[SessionStatistics](),
		treeEvent:            mo.Some(treeEvent),
	}
}
