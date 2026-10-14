package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// configuredCancellationReport contains the extension's local and received cancellation observations.
type configuredCancellationReport struct {
	// LocalText contains the interrupted local Wait error.
	LocalText string `json:"local_text"`
	// LocalCanceled identifies the local context cancellation.
	LocalCanceled bool `json:"local_canceled"`
	// Text contains the received configured-operation terminal error.
	Text string `json:"text"`
	// Canceled identifies the received context cancellation.
	Canceled bool `json:"canceled"`
	// Typed identifies the SDK canceled terminal.
	Typed bool `json:"typed"`
	// Rewrapped identifies preservation of the receiver-local SDK error.
	Rewrapped bool `json:"rewrapped"`
	// Progress contains the configured-operation retry causes acquired before cancellation.
	Progress []configuredRetryObservation `json:"progress"`
}

// requestConfiguredModelCancellation reports cancellation after an actual configured retry update.
func requestConfiguredModelCancellation(ctx context.Context) (*extensionv1.ToolResult, error) {
	const receiverFailureFormat = "configured receiver: %w"
	startContext, cancelStart := context.WithCancel(ctx)
	defer cancelStart()
	operation, err := startConfiguredModel(startContext, configuredInstructions, configuredDisabledReasoningChoice)
	if err != nil {
		return nil, err
	}
	localContext, cancelLocal := context.WithCancel(ctx)
	cancelLocal()
	_, localErr := operation.Wait(localContext)
	progress := make([]configuredRetryObservation, 0)
	_, receivedErr := operation.WaitWithProgress(ctx, func(update *extensionv1.ConfiguredModelRetryProgress) error {
		progress = append(progress, configuredRetryObservation{
			CompletedAttempts: update.GetCompletedAttempts(), AttemptLimit: update.GetAttemptLimit(),
			DelayMilliseconds: update.GetDelayMilliseconds(), Error: update.GetError(),
		})
		cancelStart()
		return nil
	})
	var canceled *extensionsdk.CanceledError
	typed := errors.As(receivedErr, &canceled)
	wrapped := fmt.Errorf(receiverFailureFormat, receivedErr)
	var wrappedCanceled *extensionsdk.CanceledError
	rewrapped := errors.As(wrapped, &wrappedCanceled) && wrappedCanceled == canceled && errors.Is(wrapped, receivedErr)
	encoded, err := json.Marshal(configuredCancellationReport{
		LocalText: fmt.Sprint(localErr), LocalCanceled: errors.Is(localErr, context.Canceled),
		Text: fmt.Sprint(receivedErr), Canceled: errors.Is(receivedErr, context.Canceled),
		Typed: typed, Rewrapped: rewrapped, Progress: progress,
	})
	if err != nil {
		return nil, err
	}
	return catalogueTextResult(encoded), nil
}
