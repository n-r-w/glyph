//go:build integration

package presentation

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
)

// TestSelectionCompletionDoesNotSettleActiveAgent verifies real decoding and presentation remain selection-neutral.
func TestSelectionCompletionDoesNotSettleActiveAgent(t *testing.T) {
	t.Parallel()
	// Arrange an active presentation, one pending selection, a newer event, and an older completion.
	display := NewMockDisplay(gomock.NewController(t))
	display.EXPECT().Publish(gomock.Any()).Times(2)
	service := New(nil, display, nil)
	service.model.state.Availability = mo.Some(AvailabilityRunning)
	service.model.state.Settled = mo.Some(false)
	service.model.state.ActiveModel = map[int]ActiveModelContent{0: {
		Kind: mo.Some(ModelContentText), Text: mo.Some("streaming"),
	}}
	service.pending["selection"] = pendingCommand{
		command: emptyCommand(CommandSelectModel), dispatchPending: false,
		terminal: false, acknowledgementApplied: true,
	}
	newer := new(uiv1.HostConnectionEvent)
	newer.SetModelSelectionChanged(uiv1.ModelSelectionChanged_builder{Selection: uiv1.ModelSelection_builder{
		ProviderId: new("provider"), ModelId: new("new"),
		ReasoningChoice: new(uiv1.ReasoningChoice_REASONING_CHOICE_HIGH),
	}.Build(), Issues: nil}.Build())
	older := new(uiv1.HostCompleted)
	older.SetModelSelection(uiv1.ModelSelectionChanged_builder{Selection: uiv1.ModelSelection_builder{
		ProviderId: new("provider"), ModelId: new("old"),
		ReasoningChoice: new(uiv1.ReasoningChoice_REASONING_CHOICE_LOW),
	}.Build(), Issues: nil}.Build())
	eventPayload, err := plugininput.DecodeConnectionEvent(newer)
	require.NoError(t, err)
	completionPayload, present, err := plugininput.DecodeCompleted(older)
	require.NoError(t, err)

	// Act by applying the newer state event before the delayed selection completion.
	require.NoError(t, service.Notify(plugininput.Notification{
		Kind: plugininput.NotificationConnection, OperationID: "", Payload: mo.Some(eventPayload),
		Failure: nil, FailureCode: "",
	}))
	require.NoError(t, service.Notify(plugininput.Notification{
		Kind: plugininput.NotificationCompleted, OperationID: "selection",
		Payload: mo.TupleToOption(completionPayload, present), Failure: nil, FailureCode: "",
	}))

	// Assert the newer selection remains and unrelated active agent presentation is unchanged.
	assert.Equal(t, "new", service.model.state.ModelSelection.MustGet().ModelID)
	assert.Equal(t, mo.Some(false), service.model.state.Settled)
	assert.Equal(t, mo.Some("streaming"), service.model.state.ActiveModel[0].Text)
}
