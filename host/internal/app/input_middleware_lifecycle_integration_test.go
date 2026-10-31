//go:build integration

package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// inputSignalsEnvironment names the directory that receives the blocked-invocation marker.
	inputSignalsEnvironment = "GLYPH_INPUT_SIGNALS"
	// inputBlockedSignal is the marker written when the reference starts its blocked invocation.
	inputBlockedSignal = "input-blocked"
	// inputSignalPoll is the interval between checks for the blocked-invocation marker.
	inputSignalPoll = 10 * time.Millisecond
)

// userMessages returns the user entries of the active session history.
func userMessages(t *testing.T, fixture *programmaticFixture, operationID string) []*programmaticv1.UserMessage {
	t.Helper()
	entries := sendProgrammaticOperation(t, fixture, operationID, func(request *programmaticv1.OpenRequest) {
		programmaticRequest(request).SetGetMessages(new(programmaticv1.GetMessages))
	}).GetMessages().GetEntries()
	var users []*programmaticv1.UserMessage
	for _, entry := range entries {
		if entry.HasUser() {
			users = append(users, entry.GetUser())
		}
	}
	return users
}

// waitForInputSignal waits until the reference reports its blocked invocation.
func waitForInputSignal(t *testing.T, directory string) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(directory, inputBlockedSignal))
		return err == nil
	}, 30*time.Second, inputSignalPoll)
}

// receiveOperationEvent returns the next event of one of the given operations.
func receiveOperationEvent(
	t *testing.T,
	fixture *programmaticFixture,
	operationIDs ...string,
) (string, *programmaticv1.HostEvent) {
	t.Helper()
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		for _, id := range operationIDs {
			if response.GetOperationId() == id && response.HasEvent() {
				return id, response.GetEvent()
			}
		}
	}
}

// TestPublicInputMiddlewareOriginalReplacement verifies that a handler can replace the input from the original.
//
//nolint:paralleltest // The scenario replaces process-global provider HTTP transport and environment.
func TestPublicInputMiddlewareOriginalReplacement(t *testing.T) {
	// Arrange the reference where input-text appends A and input-images replaces from the original plus B.
	trace := enableInputTrace(t)
	paths := testPaths(t, userInputSettings(false))
	writeProgrammaticCredentials(t, paths)
	bodies := userInputTransport(t)
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildInputFixture(t, "input-original"))
	defer fixture.closeOwner(t)

	// Act by submitting one text request.
	completeProgrammaticRequest(t, fixture, userRequest("original-request", userInputFirstText))

	// Assert the invocation order and that B replaced A in the provider input and the persisted entry.
	entries := readInputTrace(t, trace)
	assert.Equal(t, inputHandlers, contextTraceHandlers(entries))
	images := decodeInputInvocation(t, entries[2])
	require.Len(t, images.GetCurrent().GetContent(), 2)
	assert.Equal(t, "glyph-input-A", images.GetCurrent().GetContent()[1].GetText())
	received := bodies.snapshot()
	require.Len(t, received, 1)
	contents := providerUserContent(t, received[0])
	assert.JSONEq(
		t,
		`[{"type":"input_text","text":"`+userInputFirstText+`"},{"type":"input_text","text":"glyph-input-B"}]`,
		contents[len(contents)-1],
	)
	users := userMessages(t, fixture, "original-messages")
	require.Len(t, users, 1)
	require.Len(t, users[0].GetContent(), 2)
	assert.Equal(t, userInputFirstText, users[0].GetContent()[0].GetText())
	assert.Equal(t, "glyph-input-B", users[0].GetContent()[1].GetText())
}

// TestPublicInputMiddlewareCancellation verifies admission while a handler runs and cancellation of that handler.
func TestPublicInputMiddlewareCancellation(t *testing.T) {
	const blockedID, busyID, cancelID = "blocked-request", "busy-request", "cancel-blocked"
	// Arrange the reference whose first input-text invocation blocks until Host cancels it.
	trace := enableInputTrace(t)
	signals := t.TempDir()
	t.Setenv(inputSignalsEnvironment, signals)
	paths := testPaths(t, userInputSettings(false))
	writeProgrammaticCredentials(t, paths)
	bodies := userInputTransport(t)
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildInputFixture(t, "input-block"))
	defer fixture.closeOwner(t)

	// Act by blocking the first request, submitting a second one, and canceling the first.
	require.NoError(t, fixture.stream.Send(userRequest(blockedID, userInputFirstText)))
	waitForInputSignal(t, signals)
	require.NoError(t, fixture.stream.Send(userRequest(busyID, userInputLastText)))
	busyCode := ""
	agentEvents := false
	for busyCode == "" {
		id, event := receiveOperationEvent(t, fixture, blockedID, busyID)
		require.False(t, event.HasCompleted() || event.HasFailed() || event.HasCanceled(), "%s ended early", id)
		agentEvents = agentEvents || event.GetProgress().GetAgentEvent() != nil
		busyCode = event.GetRejected().GetCode()
	}
	require.NoError(t, fixture.stream.Send(cancelRequest(cancelID, blockedID)))
	targetCanceled, cancelCompleted := false, false
	for !targetCanceled || !cancelCompleted {
		id, event := receiveOperationEvent(t, fixture, blockedID, cancelID)
		agentEvents = agentEvents || event.GetProgress().GetAgentEvent() != nil
		require.False(t, event.HasFailed() || event.HasRejected(), "%s did not end as expected: %v", id, event)
		if id == blockedID {
			require.False(t, event.HasCompleted())
			targetCanceled = targetCanceled || event.HasCanceled()
		}
		if id == cancelID && event.HasCompleted() {
			cancelCompleted = true
			assert.Equal(t, operationv1.TerminalState_TERMINAL_STATE_CANCELED,
				event.GetCompleted().GetCancel().GetTargetState())
		}
	}
	completeProgrammaticRequest(t, fixture, userRequest("after-cancel", userInputFollowUpText))

	// Assert the rejection, no work after the blocked handler, and a successful follow-up.
	assert.Equal(t, "BUSY", busyCode)
	assert.False(t, agentEvents)
	entries := readInputTrace(t, trace)
	require.Len(t, entries, 2+len(inputHandlers))
	assert.Equal(t, inputHandlers[:2], contextTraceHandlers(entries[:2]))
	received := bodies.snapshot()
	require.Len(t, received, 1)
	users := userMessages(t, fixture, "after-cancel-messages")
	require.Len(t, users, 1)
	assert.Equal(t, userInputFollowUpText+inputTextSuffix, users[0].GetContent()[0].GetText())
}

// TestPublicInputMiddlewareRuntimeLoss verifies that loss of the extension process fails the request before Core.
//
//nolint:paralleltest // The scenario replaces process-global provider HTTP transport and environment.
func TestPublicInputMiddlewareRuntimeLoss(t *testing.T) {
	// Arrange the reference that terminates its process during the first input-text invocation.
	trace := enableInputTrace(t)
	paths := testPaths(t, userInputSettings(false))
	writeProgrammaticCredentials(t, paths)
	bodies := userInputTransport(t)
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildInputFixture(t, "input-exit"))
	defer fixture.closeOwner(t)

	// Act by submitting one request that loses the runtime and one later request.
	lost := runProgrammaticUserInputFailure(t, fixture, "lost-request")
	later := runProgrammaticUserInputFailure(t, fixture, "later-request")

	// Assert the complete extension failure, no later handler, no Core effects, and admission of the next request.
	assert.Equal(t, extensionFailedCode, lost.GetCode())
	assert.Contains(t, lost.GetMessage(), `middleware handler "input-text"`)
	assert.Equal(t, inputHandlers[:2], contextTraceHandlers(readInputTrace(t, trace)))
	assert.Equal(t, extensionFailedCode, later.GetCode())
	assert.Empty(t, bodies.snapshot())
	assert.Empty(t, userMessages(t, fixture, "lost-messages"))
}

// runProgrammaticUserInputFailure submits one text request that must be admitted and then fail without agent events.
func runProgrammaticUserInputFailure(
	t *testing.T,
	fixture *programmaticFixture,
	operationID string,
) *operationv1.Failed {
	t.Helper()
	require.NoError(t, fixture.stream.Send(userRequest(operationID, userInputFirstText)))
	for {
		_, event := receiveOperationEvent(t, fixture, operationID)
		require.False(t, event.HasRejected() || event.HasCompleted() || event.HasCanceled(), "%v", event)
		require.Nil(t, event.GetProgress().GetAgentEvent())
		if event.HasFailed() {
			return event.GetFailed().GetFailure()
		}
	}
}
