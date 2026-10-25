//go:build integration

package presentation

import (
	"errors"
	"testing"
	"time"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
	tuiinput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// receiptCloneCommand selects the other public replacement completion group.
const receiptCloneCommand = "/clone"

// receiptSubmitText starts later work after a deferred healthy compaction has settled.
const receiptSubmitText = "continue"

// receiptRejectedCode identifies operation-gate rejection before replacement.
const receiptRejectedCode = "BUSY"

// receiptRejectedText supplies the complete rejected replacement diagnostic.
const receiptRejectedText = "replacement rejected: original admission cause\nadditional detail"

// TestCompactionReceiptSessionCrossing keeps an earlier operation's result out of a confirmed replacement.
func TestCompactionReceiptSessionCrossing(t *testing.T) {
	t.Parallel()
	for _, command := range []string{receiptNewSessionCommand, receiptCloneCommand} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			// Arrange real decoding and presentation with generated external boundaries.
			service, snapshot := newReceiptSessionService(t)
			compactID := dispatchReceiptCommand(t, service, snapshot, receiptCompactionCommand)
			replacementID := dispatchReceiptCommand(t, service, snapshot, command)

			// Act with confirmed replacement before the earlier Failed-with-receipt terminal.
			confirmReceiptSession(t, service, replacementID, command)
			notification := failReceiptSessionCompaction(t, service, compactID)

			// Assert the replacement keeps its transcript while the complete error and correlation settle.
			requireReceiptCount(t, *snapshot, 0)
			requireReceiptFailure(t, *snapshot)
			require.Error(t, service.Notify(notification))
		})
	}
}

// TestCompactionReceiptPendingReplacement applies new-session work dispatched before replacement confirmation.
func TestCompactionReceiptPendingReplacement(t *testing.T) {
	t.Parallel()
	for _, terminalFirst := range []bool{false, true} {
		name := "replacement-before-terminal"
		if terminalFirst {
			name = "terminal-before-replacement-and-local-ack"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange an enqueued replacement and a subsequent compaction with local acknowledgement withheld.
			service, snapshot := newReceiptSessionService(t)
			replacementID := dispatchReceiptCommand(t, service, snapshot, receiptNewSessionCommand)
			result := executeReceiptSessionCommand(t, service, snapshot, receiptCompactionCommand)

			// Act in both feasible terminal/replacement orders, then consume the late local acknowledgement.
			if !terminalFirst {
				confirmReceiptSession(t, service, replacementID, receiptNewSessionCommand)
			}
			notification := failReceiptSessionCompaction(t, service, result.ID)
			requireReceiptFailure(t, *snapshot)
			if terminalFirst {
				confirmReceiptSession(t, service, replacementID, receiptNewSessionCommand)
			}
			require.False(t, service.Complete(result))

			// Assert the new session receives its receipt and complete diagnostic without reopening correlation.
			requireReceiptCount(t, *snapshot, 1)
			requireReceiptFailure(t, *snapshot)
			require.Error(t, service.Notify(notification))
		})
	}
}

// TestCompactionReceiptRejectedReplacement retains the unchanged owner when replacement admission fails.
func TestCompactionReceiptRejectedReplacement(t *testing.T) {
	t.Parallel()
	for _, terminalFirst := range []bool{false, true} {
		name := "rejection-before-terminal"
		if terminalFirst {
			name = "terminal-before-rejection"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange a replacement request followed by compaction of the unchanged session.
			service, snapshot := newReceiptSessionService(t)
			replacementID := dispatchReceiptCommand(t, service, snapshot, receiptNewSessionCommand)
			compactID := dispatchReceiptCommand(t, service, snapshot, receiptCompactionCommand)
			reject := plugininput.Notification{
				Kind: plugininput.NotificationFailed, OperationID: replacementID,
				Payload: mo.None[plugininput.Payload](), Failure: errors.New(receiptRejectedText),
				FailureCode: receiptRejectedCode,
			}

			// Act with rejection on either side of the retained compaction terminal.
			if !terminalFirst {
				require.NoError(t, service.Notify(reject))
			}
			notification := failReceiptSessionCompaction(t, service, compactID)
			if terminalFirst {
				require.NoError(t, service.Notify(reject))
			}

			// Assert receipt application, complete diagnostics and settled operations through caller-visible output.
			requireReceiptCount(t, *snapshot, 1)
			requireReceiptFailure(t, *snapshot)
			rejectedDiagnostic := false
			for _, line := range snapshot.Body.Transcript {
				if line.Text.OrEmpty() == receiptRejectedText {
					rejectedDiagnostic = true
				}
			}
			require.True(t, rejectedDiagnostic, "complete rejection diagnostic must remain visible")
			require.Error(t, service.Notify(notification))
			require.Error(t, service.Notify(reject))
		})
	}
}

// TestCompactionReceiptReplayPreservesLaterRetry keeps receipt replay from settling newer work again.
func TestCompactionReceiptReplayPreservesLaterRetry(t *testing.T) {
	t.Parallel()
	// Arrange healthy compaction completed before its owning replacement confirmation.
	service, snapshot := newReceiptSessionService(t)
	replacementID := dispatchReceiptCommand(t, service, snapshot, receiptNewSessionCommand)
	compactID := dispatchReceiptCommand(t, service, snapshot, receiptCompactionCommand)
	entry, _ := compactionReceiptEntries()
	completed := new(uiv1.HostCompleted)
	completed.SetCompaction(uiv1.CompactionResult_builder{
		Committed: entry, Canceled: new(false), Error: nil, FailureCode: nil,
	}.Build())
	notification := applyReceiptCompletion(t, service, compactID, completed, false)
	submitID := dispatchReceiptCommand(t, service, snapshot, receiptSubmitText)
	require.NoError(t, service.Notify(plugininput.Notification{
		Kind: plugininput.NotificationProgress, OperationID: submitID, Failure: nil, FailureCode: "",
		Payload: mo.Some(plugininput.AgentPayload(plugininput.AgentUpdate{
			Kind: plugininput.AgentRetryProgress, Position: mo.None[int](),
			ModelContentKind: mo.None[plugininput.ModelContentKind](), ModelResponseContent: nil,
			ToolCallID: mo.None[string](), ToolName: mo.None[string](), Status: mo.None[string](),
			Stream: mo.None[plugininput.OutputStream](), Text: mo.None[string](),
			Contents: mo.None[[]plugininput.Content](), ErrorText: mo.None[string](),
			ExitCode: mo.None[int](), Failure: mo.None[bool](), ToolCall: mo.None[plugininput.ToolCallState](),
			Retry: mo.Some(plugininput.RetryProgress{
				CompletedAttempts: 1, AttemptLimit: 3, Delay: time.Second, Error: receiptFailureText,
			}),
		})),
	}))
	retry := snapshot.Body.RetryStatus
	require.NotEmpty(t, retry)

	// Act when delayed replacement confirmation resolves the healthy receipt's owner.
	confirmReceiptSession(t, service, replacementID, receiptNewSessionCommand)

	// Assert receipt projection leaves the newer operation's retry state and old correlation settled.
	requireReceiptCount(t, *snapshot, 1)
	require.Equal(t, retry, snapshot.Body.RetryStatus)
	require.Error(t, service.Notify(notification))
}

// newReceiptSessionService exposes only the production service and its published output.
func newReceiptSessionService(t *testing.T) (*Service, *Snapshot) {
	t.Helper()
	control := gomock.NewController(t)
	display := NewMockDisplay(control)
	snapshot := new(Snapshot)
	display.EXPECT().Publish(gomock.Any()).Do(func(value Snapshot) { *snapshot = value }).AnyTimes()
	host := NewMockHost(control)
	host.EXPECT().Send(gomock.Any(), gomock.Any(), "").DoAndReturn(
		func(identifier string, command Command, target string) error {
			require.NotEmpty(t, identifier)
			require.Empty(t, target)
			expected := map[string]CommandKind{
				receiptCompactionCommand: CommandCompact, receiptNewSessionCommand: CommandCreateSession,
				receiptCloneCommand: CommandCloneSession, receiptSubmitText: CommandSubmit,
			}
			require.Equal(t, expected[string(snapshot.Input)], command.Kind)
			return nil
		},
	).AnyTimes()
	service := New(host, display, nil)
	require.NoError(t, service.Notify(plugininput.Notification{
		Kind: plugininput.NotificationConnection, OperationID: "", Failure: nil, FailureCode: "",
		Payload: mo.Some(plugininput.AvailabilityPayload(plugininput.AvailabilityIdle)),
	}))
	return service, snapshot
}

// clearReceiptSessionInput replaces the visible editor draft through public editing keys.
func clearReceiptSessionInput(t *testing.T, service *Service, snapshot Snapshot) {
	t.Helper()
	require.Nil(t, service.Key(tuiinput.Key{Code: tuiinput.KeyHome, Text: "", Mod: 0}))
	for range snapshot.Input {
		require.Nil(t, service.Key(tuiinput.Key{Code: tuiinput.KeyDelete, Text: "", Mod: 0}))
	}
}

// executeReceiptSessionCommand returns the public dispatch result without applying local acknowledgement.
func executeReceiptSessionCommand(t *testing.T, service *Service, snapshot *Snapshot, command string) tuiinput.Result {
	t.Helper()
	clearReceiptSessionInput(t, service, *snapshot)
	require.Nil(t, service.Key(tuiinput.Key{Code: 0, Text: command, Mod: 0}))
	work := service.Key(tuiinput.Key{Code: tuiinput.KeyEnter, Text: "", Mod: 0})
	require.NotNil(t, work)
	result := work.Execute()
	require.NoError(t, result.Err)
	return result
}

// confirmReceiptSession delivers replacement data through the production completion decoder.
func confirmReceiptSession(t *testing.T, service *Service, id, command string) {
	t.Helper()
	info := uiv1.SessionInfo_builder{
		Id: new(receiptReplacementSessionID), Name: nil, WorkingDirectory: new(receiptWorkingDirectory),
		StoragePath: nil, CreatedTime: timestamppb.New(time.Unix(3, 0)), UpdateTime: timestamppb.New(time.Unix(3, 0)),
	}.Build()
	completed := new(uiv1.HostCompleted)
	if command == receiptCloneCommand {
		completed.SetSessionCloned(uiv1.SessionCloned_builder{
			Session: uiv1.SessionChanged_builder{Info: info, Entries: nil}.Build(),
		}.Build())
	} else {
		completed.SetSessionChanged(uiv1.SessionChanged_builder{Info: info, Entries: nil}.Build())
	}
	applyReceiptCompletion(t, service, id, completed, false)
}

// failReceiptSessionCompaction retains the actual receipt alongside the full independent failure.
func failReceiptSessionCompaction(t *testing.T, service *Service, id string) plugininput.Notification {
	t.Helper()
	entry, _ := compactionReceiptEntries()
	completed := new(uiv1.HostCompleted)
	completed.SetCompaction(uiv1.CompactionResult_builder{
		Committed: entry, Canceled: new(false), Error: new(receiptFailureText), FailureCode: new(receiptFailureCode),
	}.Build())
	return applyReceiptCompletion(t, service, id, completed, true)
}

// requireReceiptCount checks compaction projection without inspecting identity or pending state.
func requireReceiptCount(t *testing.T, snapshot Snapshot, expected int) {
	t.Helper()
	count := 0
	for _, line := range snapshot.Body.Transcript {
		if line.Kind == LineCompaction {
			count++
		}
	}
	require.Equal(t, expected, count)
}

// requireReceiptFailure checks that the caller can still read the complete multiline cause.
func requireReceiptFailure(t *testing.T, snapshot Snapshot) {
	t.Helper()
	for _, line := range snapshot.Body.Transcript {
		if line.Text.OrEmpty() == receiptFailureText {
			return
		}
	}
	t.Fatal("complete compaction failure is absent from the published transcript")
}
