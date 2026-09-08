//go:build !integration

package presentation

import (
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
	tuiinput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"
)

// TestHostConfirmationReleasesEditorBeforeLocalResult preserves editing and newer dispatches after Host confirmation.
func TestHostConfirmationReleasesEditorBeforeLocalResult(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"/new", "/name renamed", "/session"} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			for _, outcome := range []string{"completed", "failed"} {
				t.Run(outcome, func(t *testing.T) {
					t.Parallel()
					for _, lateResult := range []string{"success", "failure"} {
						t.Run(lateResult, func(t *testing.T) {
							t.Parallel()
							checkSessionConfirmation(t, text, outcome, lateResult)
						})
					}
				})
			}
		})
	}
}

// checkSessionConfirmation exercises actual editor input across independent Host and local result delivery.
func checkSessionConfirmation(t *testing.T, text, outcome, lateResult string) {
	t.Helper()
	// Arrange a real session command whose local result is held outside the event loop.
	service := newTestModel(t, AvailabilityIdle, nil)
	service.Key(tuiinput.Key{Code: 0, Text: text, Mod: 0})
	work := service.Key(testKey(tuiinput.KeyEnter))
	require.NotNil(t, work)
	result := work.Execute()
	require.NoError(t, result.Err)
	kind := plugininput.SessionInformation
	if text == "/new" {
		kind = plugininput.SessionChanged
	}
	notification := sessionConfirmation(result.ID, kind)
	if outcome == "failed" {
		notification.Kind = plugininput.NotificationFailed
		notification.FailureCode = "INTERNAL"
		notification.Failure = errors.New("Host operation failed after delivery")
		notification.Payload = mo.None[plugininput.Payload]()
	}

	// Act by delivering the Host outcome, then typing before the local dispatch result arrives.
	require.NoError(t, service.Notify(notification))
	before := string(service.model.input)
	if outcome == "completed" {
		require.Empty(t, before)
		require.Equal(t, "confirmed-session", service.model.state.SessionInfo.MustGet().ID)
	} else {
		require.Equal(t, text, before)
		require.Contains(t, service.model.state.Transcript[0].Text.OrEmpty(), notification.Failure.Error())
	}
	service.Key(tuiinput.Key{Code: 0, Text: " new draft", Mod: 0})

	// Assert delivery releases real editing without claiming that a failed operation succeeded.
	require.Equal(t, before+" new draft", string(service.model.input))
	require.Contains(t, service.pending, result.ID)
	for range len(service.model.input) {
		service.Key(testKey(tuiinput.KeyBackspace))
	}
	service.Key(tuiinput.Key{Code: 0, Text: "/session", Mod: 0})
	newer := service.Key(testKey(tuiinput.KeyEnter))
	require.NotNil(t, newer)
	transcript := service.model.state.Clone().Transcript
	if lateResult == "failure" {
		result.Err = errors.New("late local dispatch failure with diagnostic suffix")
	}
	require.False(t, service.Complete(result))
	require.NotContains(t, service.pending, result.ID)
	service.Key(tuiinput.Key{Code: 0, Text: " must remain blocked", Mod: 0})
	require.Equal(t, "/session", string(service.model.input))
	require.Equal(t, len("/session"), service.model.cursor)
	if result.Err == nil {
		require.Equal(t, transcript, service.model.state.Transcript)
	} else {
		require.Len(t, service.model.state.Transcript, len(transcript)+1)
		require.Equal(t, transcript, service.model.state.Transcript[:len(transcript)])
		require.Contains(t, service.model.state.Transcript[len(transcript)].Text.OrEmpty(), result.Err.Error())
	}
	newerResult := newer.Execute()
	require.NoError(t, newerResult.Err)
	require.False(t, service.Complete(newerResult))
	require.NoError(t, service.Notify(sessionConfirmation(newerResult.ID, plugininput.SessionInformation)))
	service.Key(tuiinput.Key{Code: 0, Text: "accepted after newer confirmation", Mod: 0})
	require.Equal(t, "accepted after newer confirmation", string(service.model.input))
}

// TestDispatchFirstConfirmationPreservesNewerInputBlock keeps an acknowledged command from releasing a newer command.
func TestDispatchFirstConfirmationPreservesNewerInputBlock(t *testing.T) {
	t.Parallel()
	// Arrange an acknowledged information request followed by another in-flight request.
	service := newTestModel(t, AvailabilityIdle, nil)
	service.Key(tuiinput.Key{Code: 0, Text: "/session", Mod: 0})
	first := service.Key(testKey(tuiinput.KeyEnter))
	firstResult := first.Execute()
	require.NoError(t, firstResult.Err)
	require.False(t, service.Complete(firstResult))
	newer := service.Key(testKey(tuiinput.KeyEnter))
	require.NotNil(t, newer)

	// Act by confirming the first request while the newer local result is still pending.
	require.NoError(t, service.Notify(sessionConfirmation(firstResult.ID, plugininput.SessionInformation)))
	service.Key(tuiinput.Key{Code: 0, Text: "must remain blocked", Mod: 0})

	// Assert the newer command still controls input, then accepts editing after its own result.
	require.Empty(t, service.model.input)
	newerResult := newer.Execute()
	require.NoError(t, newerResult.Err)
	require.False(t, service.Complete(newerResult))
	require.NoError(t, service.Notify(sessionConfirmation(newerResult.ID, plugininput.SessionInformation)))
	service.Key(tuiinput.Key{Code: 0, Text: "new draft", Mod: 0})
	require.Equal(t, "new draft", string(service.model.input))
}

// TestEarlySessionListPermitsResumeAndPreservesRejection keeps selector delivery separate from resume success.
func TestEarlySessionListPermitsResumeAndPreservesRejection(t *testing.T) {
	t.Parallel()
	// Arrange a session list whose local result is delayed until after a selected resume starts.
	service := newTestModel(t, AvailabilityIdle, nil)
	service.Key(tuiinput.Key{Code: 0, Text: "/resume", Mod: 0})
	list := service.Key(testKey(tuiinput.KeyEnter))
	listResult := list.Execute()
	require.NoError(t, listResult.Err)
	confirmation := sessionConfirmation(listResult.ID, plugininput.SessionListed)
	info := confirmation.Payload.MustGet().Session.Info.MustGet()
	confirmation.Payload = mo.Some(plugininput.SessionPayload(plugininput.SessionUpdate{
		Kind: plugininput.SessionListed,
		Info: mo.None[plugininput.SessionInfo](),
		Sessions: []plugininput.SessionSummary{
			{Info: info, FirstUserText: "stored request", TextPresent: true, TotalMessages: 1},
		},
		Transcript: nil,
		Statistics: mo.None[plugininput.SessionStatistics](),
	}))

	// Act by selecting the returned session before the local list result reaches the event loop.
	require.NoError(t, service.Notify(confirmation))
	resume := service.Key(testKey(tuiinput.KeyEnter))

	// Assert actual selection emits a resume, but its rejection retains the selector and draft.
	require.NotNil(t, resume)
	resumeResult := resume.Execute()
	require.NoError(t, resumeResult.Err)
	require.False(t, service.Complete(listResult))
	require.NoError(t, service.Notify(plugininput.Notification{
		Kind: plugininput.NotificationFailed, OperationID: resumeResult.ID,
		FailureCode: "SESSION_UNAVAILABLE", Failure: errors.New("stored session cannot be resumed"),
		Payload: mo.None[plugininput.Payload](),
	}))
	require.True(t, service.model.selectorOpen)
	require.Equal(t, "stored session cannot be resumed", service.model.resumeStatus)
	require.Equal(t, "/resume", string(service.model.input))
	service.Key(testKey(tuiinput.KeyEscape))
	before := string(service.model.input)
	service.Key(tuiinput.Key{Code: 0, Text: " edited", Mod: 0})
	require.Equal(t, before+" edited", string(service.model.input))
	resumeResult.Err = errors.New("late resume dispatch diagnostic")
	require.False(t, service.Complete(resumeResult))
	require.Equal(t, before+" edited", string(service.model.input))
	require.Contains(t, service.model.state.Transcript[len(service.model.state.Transcript)-1].Text.OrEmpty(),
		resumeResult.Err.Error())
}

// TestNavigationDeliveryProofSurvivesLateResult retains foreground and terminal correlation after early progress.
func TestNavigationDeliveryProofSurvivesLateResult(t *testing.T) {
	t.Parallel()
	for _, outcome := range []string{"success", "failure"} {
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()
			// Arrange navigation with its tree panel closed and an unchanged editor draft.
			service := newTestModel(t, AvailabilityIdle, nil)
			service.Key(tuiinput.Key{Code: 0, Text: "draft", Mod: 0})
			var intent *commandIntent
			service.model, intent = service.model.emitCommand(treeCommand(CommandNavigateSessionTree, TreeCommand{
				TargetEntryID: mo.Some("root"), SummaryMode: SummaryModeNoSummary,
				CustomFocus: mo.None[string](), Label: mo.None[string](),
			}))
			work := service.prepareCommand(intent.command)
			result := work.Execute()
			require.NoError(t, result.Err)
			progress := navigationConfirmation(result.ID)

			// Act by applying committed navigation progress before local acknowledgement.
			require.NoError(t, service.Notify(progress))
			service.Key(tuiinput.Key{Code: 0, Text: " edited", Mod: 0})

			// Assert progress permits editing without claiming terminal completion.
			require.Equal(t, "draft edited", string(service.model.input))
			require.Equal(t, mo.Some("root prompt"), service.model.state.Transcript[0].Text)
			if outcome == "failure" {
				result.Err = errors.New("late navigation dispatch diagnostic")
			}
			require.False(t, service.Complete(result))
			require.Equal(t, result.ID, service.foreground)
			require.Contains(t, service.pending, result.ID)
			require.Equal(t, "draft edited", string(service.model.input))
			if result.Err != nil {
				require.Contains(
					t,
					service.model.state.Transcript[len(service.model.state.Transcript)-1].Text.OrEmpty(),
					result.Err.Error(),
				)
			}
			terminal := progress
			terminal.Kind = plugininput.NotificationCompleted
			terminal.Payload = mo.Some(plugininput.TreePayload(plugininput.TreeUpdate{
				Kind:             plugininput.TreeNavigationCompleted,
				Tree:             mo.None[plugininput.SessionTree](),
				NavigationStatus: plugininput.TreeNavigationCommitted,
				SessionInfo:      mo.None[plugininput.SessionInfo](),
				Transcript:       nil,
				NextInput:        mo.Some(" exact next input "),
				Issues:           nil,
				AddedEntry:       mo.None[plugininput.TreeEntry](),
			}))
			require.NoError(t, service.Notify(terminal))
			require.Empty(t, service.foreground)
			require.NotContains(t, service.pending, result.ID)
			require.Equal(t, " exact next input ", string(service.model.input))
		})
	}
}

// navigationConfirmation supplies committed tree and transcript progress for a tracked navigation command.
func navigationConfirmation(identifier string) plugininput.Notification {
	return plugininput.Notification{
		Kind: plugininput.NotificationProgress, OperationID: identifier, FailureCode: "", Failure: nil,
		Payload: mo.Some(plugininput.TreePayload(plugininput.TreeUpdate{
			Kind: plugininput.TreeNavigationProgress,
			Tree: mo.Some(plugininput.SessionTree{
				Entries: []plugininput.TreeEntry{
					{
						ID:               "root",
						ParentID:         mo.None[string](),
						CreatedAt:        time.Unix(1, 0),
						Label:            "",
						Kind:             plugininput.TreeEntryUser,
						ExtensionMessage: mo.None[plugininput.ExtensionMessage](),
						Text:             "root prompt",
					},
				},
				ActiveLeafID: mo.Some("root"),
			}),
			NavigationStatus: plugininput.TreeNavigationUnspecified, SessionInfo: mo.None[plugininput.SessionInfo](),
			Transcript: []plugininput.Transcript{{
				Kind: plugininput.TranscriptUser, ToolName: mo.None[string](), Status: mo.None[string](),
				Text: mo.Some("root prompt"), Contents: mo.None[[]plugininput.Content](),
			}},
			NextInput: mo.None[string](), Issues: nil, AddedEntry: mo.None[plugininput.TreeEntry](),
		})),
	}
}

// sessionConfirmation supplies the committed metadata required by session replacement and information contracts.
func sessionConfirmation(identifier string, kind plugininput.SessionKind) plugininput.Notification {
	// Information results require statistics; replacement results carry no statistics.
	statistics := mo.None[plugininput.SessionStatistics]()
	if kind == plugininput.SessionInformation {
		statistics = mo.Some(plugininput.SessionStatistics{
			UserMessages: 0, ModelResponses: 0, ToolCalls: 0, ToolResults: 0, TotalMessages: 0,
			TokenUsage: mo.None[plugininput.TokenUsage](), EstimatedCost: mo.None[plugininput.EstimatedCost](),
			CostBreakdown: nil,
		})
	}
	return plugininput.Notification{
		FailureCode: "", Kind: plugininput.NotificationCompleted, OperationID: identifier, Failure: nil,
		Payload: mo.Some(plugininput.SessionPayload(plugininput.SessionUpdate{
			Kind: kind,
			Info: mo.Some(plugininput.SessionInfo{
				ID: "confirmed-session", Name: "renamed", NamePresent: true, WorkingDirectory: "/project",
				StoragePath: "", StoragePresent: false, CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(2, 0),
			}),
			Sessions: nil, Transcript: nil, Statistics: statistics,
		})),
	}
}
