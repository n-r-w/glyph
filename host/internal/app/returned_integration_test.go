//go:build integration

package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// waitForRunEnd returns the agent summary and the terminal event of a run, and checks that the summary arrives
// before the terminal event.
func waitForRunEnd(
	t *testing.T, fixture *programmaticFixture, operationID string,
) (*programmaticv1.AgentSummary, *programmaticv1.HostEvent) {
	t.Helper()
	var summary *programmaticv1.AgentSummary
	for {
		_, event := receiveOperationEvent(t, fixture, operationID)
		if value := event.GetProgress().GetAgentEvent().GetAgent(); value != nil {
			summary = value
		}
		if event.HasCompleted() || event.HasFailed() || event.HasCanceled() || event.HasRejected() {
			require.NotNil(t, summary, "the terminal event arrived before the agent summary")
			return summary, event
		}
	}
}

// returnedText is a message text with the delivery kind that the summary returns it with.
type returnedText struct {
	// text is the text of the returned message.
	text string
	// delivery is the delivery kind of the returned message.
	delivery programmaticv1.MessageDelivery
}

// requireReturnedTexts checks that the summary returns the given texts with their delivery kinds in order.
func requireReturnedTexts(t *testing.T, summary *programmaticv1.AgentSummary, expected ...returnedText) {
	t.Helper()
	returned := summary.GetReturnedMessages()
	require.Len(t, returned, len(expected))
	for index, want := range expected {
		require.Len(t, returned[index].GetContent(), 1)
		require.Equal(t, want.text, returned[index].GetContent()[0].GetText())
		require.Equal(t, want.delivery, returned[index].GetDelivery())
	}
}

// requireClientMessagesAbsent checks that no provider request and no persisted user message holds the texts.
func requireClientMessagesAbsent(
	t *testing.T, provider *queueProvider, fixture *programmaticFixture, texts ...string,
) {
	t.Helper()
	for _, body := range provider.snapshot() {
		for _, text := range texts {
			require.NotContains(t, body, text)
		}
	}
	for _, user := range userMessages(t, fixture, "returned-history") {
		for _, text := range texts {
			require.NotContains(t, user.String(), text)
		}
	}
}

// TestPublicPendingMessagesReturnedAfterProviderFailure checks that a steer message and a followUp message pending
// when the provider fails the agent run come back to the client with their text in the agent summary, before the
// terminal event, and that neither reaches the model or the session.
func TestPublicPendingMessagesReturnedAfterProviderFailure(t *testing.T) {
	t.Parallel()
	// Arrange a provider that holds the first request and then rejects it without a retry.
	provider := startQueueProvider(t, 1, queueRejectedRequest)
	fixture := startQueueFixture(t, provider, t.TempDir())
	defer fixture.closeOwner(t)
	require.NoError(t, fixture.stream.Send(userRequest(queueRunID, "first request")))
	<-provider.held[0]

	// Act by queuing a followUp message and then a steer message, then releasing the failing response.
	followUp := sendFollowUp(t, fixture, "returned-follow-up", followUpText)
	steer := sendSteer(t, fixture, "returned-steer", steerFirstText)
	close(provider.release[0])
	summary, terminal := waitForRunEnd(t, fixture, queueRunID)

	// Assert the client gets both messages back in arrival order, and the model and the session never had them.
	requireQueued(t, followUp)
	requireQueued(t, steer)
	require.True(t, terminal.HasFailed(), "agent run did not fail: %v", terminal)
	require.Equal(t, programmaticv1.RunOutcome_RUN_OUTCOME_FAILED, summary.GetOutcome())
	requireReturnedTexts(t, summary,
		returnedText{followUpText, programmaticv1.MessageDelivery_MESSAGE_DELIVERY_FOLLOW_UP},
		returnedText{steerFirstText, programmaticv1.MessageDelivery_MESSAGE_DELIVERY_STEER},
	)
	require.Len(t, provider.snapshot(), 1)
	requireClientMessagesAbsent(t, provider, fixture, followUpText, steerFirstText)
}

// TestPublicPendingSteerReturnedAfterAbort checks that a steer message pending when the client cancels the
// agent run comes back to the client and never reaches the model.
func TestPublicPendingSteerReturnedAfterAbort(t *testing.T) {
	t.Parallel()
	// Arrange a provider that holds the first request until the client cancels the run.
	provider := startQueueProvider(t, 1, finalResponseSSE)
	fixture := startQueueFixture(t, provider, t.TempDir())
	defer fixture.closeOwner(t)
	require.NoError(t, fixture.stream.Send(userRequest(queueRunID, "first request")))
	<-provider.held[0]

	// Act by queuing a steer message and then canceling the user request.
	steer := sendSteer(t, fixture, "returned-steer", steerFirstText)
	require.NoError(t, fixture.stream.Send(cancelRequest("returned-cancel", queueRunID)))
	summary, terminal := waitForRunEnd(t, fixture, queueRunID)

	// Assert the run was aborted, the client has the steer message, and the model never had it.
	requireQueued(t, steer)
	require.True(t, terminal.HasCanceled(), "agent run was not canceled: %v", terminal)
	require.Equal(t, programmaticv1.RunOutcome_RUN_OUTCOME_ABORTED, summary.GetOutcome())
	requireReturnedTexts(t, summary,
		returnedText{steerFirstText, programmaticv1.MessageDelivery_MESSAGE_DELIVERY_STEER},
	)
	requireClientMessagesAbsent(t, provider, fixture, steerFirstText)
}
