//go:build !integration

package programmatic

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestMapAgentSummaryReturnsClientMessages verifies that the public agent summary carries the returned messages
// with their delivery kinds, in order, with their text and image content.
func TestMapAgentSummaryReturnsClientMessages(t *testing.T) {
	t.Parallel()
	// Arrange a failed run summary with a steer message that holds text and an image, and a followUp message.
	summary := AgentSummary{
		Outcome:      RunOutcomeFailed,
		ErrorMessage: mo.Some("failed"),
		ReturnedMessages: []agent.ReturnedMessage{
			{
				Delivery: agent.MessageDeliverySteer,
				Message: model.Message{Content: []model.InputContent{
					model.TextContent("steer text"), model.ImageContent("image/png", []byte{1, 2, 3}),
				}},
			},
			{Delivery: agent.MessageDeliveryFollowUp, Message: model.TextMessage("follow-up text")},
		},
	}

	// Act by mapping the summary to the public contract.
	wire, err := mapAgentSummary(summary)

	// Assert the public summary has both messages in order with their delivery kinds and content.
	require.NoError(t, err)
	returned := wire.GetReturnedMessages()
	require.Len(t, returned, 2)
	require.Equal(t, programmaticv1.MessageDelivery_MESSAGE_DELIVERY_STEER, returned[0].GetDelivery())
	require.Len(t, returned[0].GetContent(), 2)
	require.Equal(t, "steer text", returned[0].GetContent()[0].GetText())
	require.Equal(t, "image/png", returned[0].GetContent()[1].GetImage().GetMediaType())
	require.Equal(t, []byte{1, 2, 3}, returned[0].GetContent()[1].GetImage().GetData())
	require.Equal(t, programmaticv1.MessageDelivery_MESSAGE_DELIVERY_FOLLOW_UP, returned[1].GetDelivery())
	require.Equal(t, "follow-up text", returned[1].GetContent()[0].GetText())
}

// TestMapAgentSummaryRejectsUnknownDelivery verifies that a returned message with an unknown delivery kind fails
// the mapping instead of reaching the client with a wrong kind.
func TestMapAgentSummaryRejectsUnknownDelivery(t *testing.T) {
	t.Parallel()
	// Arrange a summary whose returned message has no delivery kind.
	summary := AgentSummary{
		Outcome:      RunOutcomeFailed,
		ErrorMessage: mo.None[string](),
		ReturnedMessages: []agent.ReturnedMessage{
			{Delivery: agent.MessageDelivery(0), Message: model.TextMessage("text")},
		},
	}

	// Act by mapping the summary.
	_, err := mapAgentSummary(summary)

	// Assert the mapping fails.
	require.Error(t, err)
}
