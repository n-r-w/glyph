//go:build integration

package app

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	operationpb "github.com/n-r-w/glyph/pkg/operation/v1"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestPublicExtensionAbort checks public acknowledgement, queue cleanup, sibling settlement and later-user recovery.
func TestPublicExtensionAbort(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	buildExternalCommand(t, directory, "toolbatch", "batch-extension")
	for _, concurrent := range []bool{false, true} {
		t.Run(fmt.Sprint(concurrent), func(t *testing.T) {
			t.Parallel()
			// Arrange real extension work held until client messages have been accepted.
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			started := make(chan string, 8)
			release := make(chan struct{})
			acknowledged := make(chan struct{})
			siblingCanceled := make(chan struct{})
			signals := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/abort":
					started <- "abort"
					select {
					case <-release:
					case <-ctx.Done():
					}
				case "/sibling":
					started <- "sibling"
					<-r.Context().Done()
					close(siblingCanceled)
				case "/ack":
					close(acknowledged)
				default:
					started <- r.URL.Path
				}
			}))
			defer signals.Close()
			ids := []string{"abort"}
			if concurrent {
				ids = append(ids, "sibling", "later")
			}
			var calls []string
			for _, id := range ids {
				args, err := json.Marshal(map[string]any{"url": signals.URL, "id": id, "abort": id == "abort"})
				require.NoError(t, err)
				quoted, err := json.Marshal(string(args))
				require.NoError(t, err)
				name := concurrentFixtureTool
				if id == "later" {
					name = serialFixtureTool
				}
				calls = append(calls, fmt.Sprintf(batchCallFormat, id, id, name, quoted))
			}
			provider := startQueueProvider(
				t,
				0,
				fmt.Sprintf(batchResponseFormat, strings.Join(calls, ",")),
				finalResponseSSE,
			)
			fixture := startQueueFixture(t, provider, directory)
			defer fixture.closeOwner(t)
			require.NoError(t, fixture.stream.Send(userRequest("extension-abort", "first request")))
			initial := []string{waitBatchSignal(t, ctx, started)}
			if concurrent {
				initial = append(initial, waitBatchSignal(t, ctx, started))
				require.ElementsMatch(t, []string{"abort", "sibling"}, initial)
			} else {
				require.Equal(t, []string{"abort"}, initial)
			}
			for index, delivery := range []programmaticv1.MessageDelivery{
				programmaticv1.MessageDelivery_MESSAGE_DELIVERY_FOLLOW_UP,
				programmaticv1.MessageDelivery_MESSAGE_DELIVERY_STEER,
			} {
				completeProgrammaticRequest(
					t,
					fixture,
					enqueueRequest(
						fmt.Sprintf("pending-%d", index),
						delivery,
						fmt.Sprintf("client original %d\nexact", index),
					),
				)
			}

			// Act by releasing the tool to request cancellation through its public extension context.
			close(release)
			var summary *programmaticv1.AgentSummary
			for {
				frame, err := fixture.stream.Recv()
				require.NoError(t, err)
				if frame.GetOperationId() != "extension-abort" {
					continue
				}
				event := frame.GetEvent()
				if update := event.GetProgress().GetAgentEvent(); update != nil {
					if update.GetAgent() != nil {
						summary = update.GetAgent()
					}
				}
				require.False(t, event.HasFailed() || event.HasRejected() || event.HasCompleted(), "%v", event)
				if event.HasCanceled() {
					require.Contains(t, event.GetCanceled().GetMessage(), "batch-extension")
					require.Contains(t, event.GetCanceled().GetMessage(), context.Canceled.Error())
					break
				}
			}

			// Assert the acknowledgement and all accepted work settle before idle and the next user run.
			waitBatchCompletion(t, ctx, acknowledged)
			if concurrent {
				waitBatchCompletion(t, ctx, siblingCanceled)
			}
			require.NotNil(t, summary)
			require.Equal(t, programmaticv1.RunOutcome_RUN_OUTCOME_ABORTED, summary.GetOutcome())
			returned := summary.GetReturnedMessages()
			require.Len(t, returned, 2)
			for index, message := range returned {
				require.Equal(t, fmt.Sprintf("client original %d\nexact", index), message.GetContent()[0].GetText())
			}
			require.Empty(t, started)
			state := completeProgrammaticRequest(t, fixture, runStateRequest("abort-idle")).GetRunState()
			require.Equal(t, programmaticv1.RunState_RUN_STATE_IDLE, state.GetState())
			completeProgrammaticRequest(t, fixture, userRequest("after-extension-abort", "later user request"))
			bodies := provider.snapshot()
			require.Len(t, bodies, 2)
			require.Contains(t, bodies[1], `extension queued nextTurn exact\ntext`)
			require.Less(
				t,
				strings.Index(bodies[1], "extension queued nextTurn"),
				strings.Index(bodies[1], "later user request"),
			)
			require.NotContains(t, bodies[1], "extension queued steer")
			require.NotContains(t, bodies[1], "extension queued followUp")
			require.NotContains(t, bodies[1], "client original")
		})
	}
}

// TestPublicClientCancellationBeforeExtensionObserver checks accepted abort provenance from a live turn-end observer.
func TestPublicClientCancellationBeforeExtensionObserver(t *testing.T) {
	t.Parallel()
	// Arrange a real tool that arms a one-shot observer, then blocks until its owning client cancels.
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	started := make(chan string, 1)
	toolCanceled := make(chan struct{})
	acknowledged := make(chan struct{})
	ackRelease := make(chan struct{})
	release := sync.OnceFunc(func() { close(ackRelease) })
	signals := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/held":
			started <- "held"
			<-r.Context().Done()
			close(toolCanceled)
		case "/ack":
			close(acknowledged)
			select {
			case <-ackRelease:
			case <-r.Context().Done():
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer signals.Close()
	arguments, err := json.Marshal(map[string]any{"url": signals.URL, "id": "held", "observer_abort": true})
	require.NoError(t, err)
	quoted, err := json.Marshal(string(arguments))
	require.NoError(t, err)
	call := fmt.Sprintf(batchCallFormat, "observer-call", "observer-call", concurrentFixtureTool, quoted)
	provider := startQueueProvider(t, 0, fmt.Sprintf(batchResponseFormat, call), finalResponseSSE)
	directory := t.TempDir()
	buildExternalCommand(t, directory, "toolbatch", "batch-extension")
	fixture := startQueueFixture(t, provider, directory)
	defer fixture.closeOwner(t)
	defer release()
	require.NoError(t, fixture.stream.Send(userRequest("observer-abort", "first request")))
	require.Equal(t, "held", waitBatchSignal(t, ctx, started))

	// Act by canceling the client first and observing the observer's successful remote abort acknowledgement.
	require.NoError(t, fixture.stream.Send(cancelRequest("cancel-observer-run", "observer-abort")))
	waitBatchCompletion(t, ctx, toolCanceled)
	waitBatchCompletion(t, ctx, acknowledged)
	state := completeProgrammaticRequest(t, fixture, runStateRequest("observer-still-running")).GetRunState()
	require.Equal(t, programmaticv1.RunState_RUN_STATE_RUNNING, state.GetState())
	require.Equal(t, "observer-abort", state.GetActiveOperationId())
	release()
	var summary *programmaticv1.AgentSummary
	var canceledMessage string
	targetCanceled, cancelCompleted := false, false
	for !targetCanceled || !cancelCompleted {
		frame, receiveErr := fixture.stream.Recv()
		require.NoError(t, receiveErr)
		event := frame.GetEvent()
		switch frame.GetOperationId() {
		case "observer-abort":
			require.False(t, event.HasFailed() || event.HasRejected() || event.HasCompleted(), "%v", event)
			if agent := event.GetProgress().GetAgentEvent().GetAgent(); agent != nil {
				summary = agent
			}
			if event.HasCanceled() {
				targetCanceled = true
				canceledMessage = event.GetCanceled().GetMessage()
			}
		case "cancel-observer-run":
			require.False(t, event.HasFailed() || event.HasRejected() || event.HasCanceled(), "%v", event)
			if event.HasCompleted() {
				require.True(t, targetCanceled)
				require.Equal(t, operationpb.TerminalState_TERMINAL_STATE_CANCELED,
					event.GetCompleted().GetCancel().GetTargetState())
				cancelCompleted = true
			}
		}
	}

	// Assert complete observer provenance reaches the existing canceled message and ends with idle recovery.
	require.NotNil(t, summary)
	require.Equal(t, programmaticv1.RunOutcome_RUN_OUTCOME_ABORTED, summary.GetOutcome())
	require.Contains(t, canceledMessage, "batch-extension")
	require.Contains(t, canceledMessage, context.Canceled.Error())
	require.Contains(t, summary.GetErrorMessage(), "batch-extension")
	require.Contains(t, summary.GetErrorMessage(), context.Canceled.Error())
	require.Contains(t, canceledMessage, summary.GetErrorMessage())
	state = completeProgrammaticRequest(t, fixture, runStateRequest("observer-idle")).GetRunState()
	require.Equal(t, programmaticv1.RunState_RUN_STATE_IDLE, state.GetState())
	completeProgrammaticRequest(t, fixture, userRequest("observer-recovery", "later user request"))
	require.Len(t, provider.snapshot(), 2)
}
