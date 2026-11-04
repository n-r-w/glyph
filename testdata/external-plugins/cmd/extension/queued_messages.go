package main

import (
	"context"
	"errors"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// queuedMessagesMode sends instructions from a running tool through its public context.
	queuedMessagesMode = "queue-messages"
	// queuedMessageType identifies the extension-owned queued messages.
	queuedMessageType = "queued-message"
	// queuedSteerText is the exact steer payload.
	queuedSteerText = "extension queued steer exact\ntext"
	// queuedFollowUpText is the exact followUp payload.
	queuedFollowUpText = "extension queued followUp exact\ntext"
	// queuedAcceptanceText reports acceptance before any extension entry exists.
	queuedAcceptanceText = "extension messages accepted without entries"
)

// queueMessages waits for queue acceptance while the tool invocation remains active.
func queueMessages(ctx context.Context) (*extensionv1.ToolResult, error) {
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return nil, err
	}
	for _, message := range []struct {
		delivery   extensionv1.ExtensionMessageDelivery
		text       string
		visibility extensionv1.ClientVisibility
	}{
		{
			extensionv1.ExtensionMessageDelivery_EXTENSION_MESSAGE_DELIVERY_FOLLOW_UP,
			queuedFollowUpText, extensionv1.ClientVisibility_CLIENT_VISIBILITY_HIDDEN,
		},
		{
			extensionv1.ExtensionMessageDelivery_EXTENSION_MESSAGE_DELIVERY_STEER,
			queuedSteerText, extensionv1.ClientVisibility_CLIENT_VISIBILITY_VISIBLE,
		},
	} {
		operation, startErr := binding.StartAppendExtensionMessage(
			ctx,
			extensionv1.AppendExtensionMessageRequest_builder{
				Context:    nil,
				EntryType:  new(queuedMessageType),
				Text:       new(message.text),
				Visibility: new(message.visibility),
				Delivery:   new(message.delivery),
			}.Build(),
		)
		if startErr != nil {
			return nil, startErr
		}
		result, waitErr := operation.Wait(ctx)
		if waitErr != nil {
			return nil, waitErr
		}
		if !result.HasQueued() || result.HasEntry() || len(result.GetIssues()) != 0 {
			return nil, errors.New("queued append must report acceptance without an entry or post-commit issues")
		}
	}
	recovery, err := binding.StartGetSessionState(ctx)
	if err != nil {
		return nil, err
	}
	state, err := recovery.Wait(ctx)
	if err != nil {
		return nil, err
	}
	for _, entry := range state.GetEntries() {
		if entry.GetEntryType() == queuedMessageType {
			return nil, errors.New("queued message committed before the tool ended")
		}
	}
	return catalogueTextResult([]byte(queuedAcceptanceText)), nil
}
