package main

import (
	"context"
	"errors"
	"io"
	"net/http"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// abortMessageType identifies attachments submitted by the aborting tool.
	abortMessageType = "abort-message"
	// abortSteerText identifies the pending extension instruction.
	abortSteerText = "extension queued steer exact\ntext"
	// abortFollowUpText identifies the pending extension task.
	abortFollowUpText = "extension queued followUp exact\ntext"
	// abortNextTurnText identifies the retained next-user attachment.
	abortNextTurnText = "extension queued nextTurn exact\ntext"
	// abortAcknowledgementPath reports successful public cancellation acknowledgement.
	abortAcknowledgementPath = "/ack"
)

// queueAbortMessages accepts run-scoped messages and a next-user attachment before the test releases abort.
func (e *execution) queueAbortMessages(ctx context.Context) error {
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return err
	}
	for _, item := range []struct {
		delivery extensionpb.ExtensionMessageDelivery
		text     string
	}{
		{delivery: extensionpb.ExtensionMessageDelivery_EXTENSION_MESSAGE_DELIVERY_STEER, text: abortSteerText},
		{delivery: extensionpb.ExtensionMessageDelivery_EXTENSION_MESSAGE_DELIVERY_FOLLOW_UP, text: abortFollowUpText},
		{delivery: extensionpb.ExtensionMessageDelivery_EXTENSION_MESSAGE_DELIVERY_NEXT_TURN, text: abortNextTurnText},
	} {
		started, startErr := binding.StartAppendExtensionMessage(ctx, extensionpb.AppendExtensionMessageRequest_builder{
			Context:    nil,
			EntryType:  new(abortMessageType),
			Text:       new(item.text),
			Visibility: new(extensionpb.ClientVisibility_CLIENT_VISIBILITY_HIDDEN),
			Delivery:   new(item.delivery),
		}.Build())
		if startErr != nil {
			return startErr
		}
		if _, waitErr := started.Wait(ctx); waitErr != nil {
			return waitErr
		}
	}
	return nil
}

// abortRun acknowledges self-abort before waiting for the calling tool's ordinary cancellation.
func (e *execution) abortRun(ctx context.Context) error {
	if err := acknowledgeAbort(ctx, e.url); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}

// acknowledgeAbort signals only after the public Host operation completes successfully.
func acknowledgeAbort(ctx context.Context, url string) error {
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return err
	}
	acknowledgement := context.WithoutCancel(ctx)
	started, err := binding.StartAbortRun(acknowledgement)
	if err != nil {
		return err
	}
	if waitErr := started.Wait(acknowledgement); waitErr != nil {
		return waitErr
	}
	request, err := http.NewRequestWithContext(
		acknowledgement,
		http.MethodGet,
		url+abortAcknowledgementPath,
		http.NoBody,
	)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	_, readErr := io.Copy(io.Discard, response.Body)
	if bodyErr := errors.Join(readErr, response.Body.Close()); bodyErr != nil {
		return bodyErr
	}
	return nil
}
