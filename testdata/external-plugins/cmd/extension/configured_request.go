package main

import (
	"context"
	"encoding/json/v2"
	"errors"

	"google.golang.org/protobuf/encoding/protojson"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// configuredRetryObservation is the stable retry progress subset returned by the external fixture.
type configuredRetryObservation struct {
	// CompletedAttempts is the number of completed configured-model attempts.
	CompletedAttempts int64 `json:"completed_attempts"`
	// AttemptLimit is the effective configured-model attempt limit.
	AttemptLimit int64 `json:"attempt_limit"`
	// DelayMilliseconds is the accepted delay before the replacement attempt.
	DelayMilliseconds int64 `json:"delay_milliseconds"`
	// Error is the complete failed-attempt error text.
	Error string `json:"error"`
}

// configuredProgressReport carries the nested result and only its operation-scoped progress.
type configuredProgressReport struct {
	// ResultJSON contains the public configured-model protobuf JSON.
	ResultJSON string `json:"result_json"`
	// Progress contains progress observed while awaiting this configured-model operation.
	Progress []configuredRetryObservation `json:"progress"`
}

// requestConfiguredModel executes one explicit public request and returns its public result as tool text.
func requestConfiguredModel(ctx context.Context) (*extensionv1.ToolResult, error) {
	operation, err := startConfiguredModel(ctx)
	if err != nil {
		return nil, err
	}
	result, err := operation.Wait(ctx)
	if err != nil {
		return nil, err
	}
	encoded, err := protojson.Marshal(result)
	if err != nil {
		return nil, err
	}
	return catalogueTextResult(encoded), nil
}

// requestConfiguredModelWithProgress awaits one configured request and records its scoped retry updates.
func requestConfiguredModelWithProgress(ctx context.Context) (*extensionv1.ToolResult, error) {
	operation, err := startConfiguredModel(ctx)
	if err != nil {
		return nil, err
	}
	progress := make([]configuredRetryObservation, 0)
	result, err := operation.WaitWithProgress(ctx, func(update *extensionv1.ConfiguredModelRetryProgress) error {
		progress = append(progress, configuredRetryObservation{
			CompletedAttempts: update.GetCompletedAttempts(), AttemptLimit: update.GetAttemptLimit(),
			DelayMilliseconds: update.GetDelayMilliseconds(), Error: update.GetError(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	resultJSON, err := protojson.Marshal(result)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(configuredProgressReport{ResultJSON: string(resultJSON), Progress: progress})
	if err != nil {
		return nil, err
	}
	return catalogueTextResult(encoded), nil
}

// configuredFailureObservation contains one SDK operation failure surface.
type configuredFailureObservation struct {
	// Code is the machine-readable SDK failure category or UNCLASSIFIED.
	Code string `json:"code"`
	// Error is the complete SDK failure text.
	Error string `json:"error"`
}

// configuredFailuresReport contains Wait and WaitWithProgress failure observations.
type configuredFailuresReport struct {
	// Wait is the terminal error returned by Wait.
	Wait configuredFailureObservation `json:"wait"`
	// WaitWithProgress is the terminal error returned by WaitWithProgress.
	WaitWithProgress configuredFailureObservation `json:"wait_with_progress"`
	// ProgressCount is the number of retry updates delivered before the mixed callback failure.
	ProgressCount int `json:"progress_count"`
}

// requestConfiguredModelFailures records the public SDK failure surface for both wait APIs.
func requestConfiguredModelFailures(ctx context.Context) (*extensionv1.ToolResult, error) {
	waitOperation, err := startConfiguredModel(ctx)
	if err != nil {
		return nil, err
	}
	_, waitErr := waitOperation.Wait(ctx)
	if waitErr == nil {
		return nil, errors.New("configured-model Wait unexpectedly succeeded")
	}
	progressOperation, err := startConfiguredModel(ctx)
	if err != nil {
		return nil, err
	}
	progressCount := 0
	_, progressErr := progressOperation.WaitWithProgress(
		ctx,
		func(*extensionv1.ConfiguredModelRetryProgress) error {
			progressCount++
			return nil
		},
	)
	if progressErr == nil {
		return nil, errors.New("configured-model WaitWithProgress unexpectedly succeeded")
	}
	encoded, err := json.Marshal(configuredFailuresReport{
		Wait: observeConfiguredFailure(waitErr), WaitWithProgress: observeConfiguredFailure(progressErr),
		ProgressCount: progressCount,
	})
	if err != nil {
		return nil, err
	}
	return catalogueTextResult(encoded), nil
}

// observeConfiguredFailure retains a public SDK code when classification survives transport.
func observeConfiguredFailure(err error) configuredFailureObservation {
	if failure, found := errors.AsType[*extensionsdk.FailureError](err); found {
		return configuredFailureObservation{Code: failure.Code(), Error: failure.Error()}
	}
	return configuredFailureObservation{Code: "UNCLASSIFIED", Error: err.Error()}
}

// startConfiguredModel starts the shared explicit configured-model fixture request.
func startConfiguredModel(ctx context.Context) (*extensionsdk.ConfiguredModelOperation, error) {
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return nil, err
	}
	request := extensionv1.ConfiguredModelRequest_builder{
		Context: nil,
		Selection: extensionv1.ModelSelection_builder{
			ProviderId: new("openai-codex"), ModelId: new("gpt-test"), ReasoningChoice: new("off"),
		}.Build(),
		Instructions: new("extension instructions"),
		Messages: []*extensionv1.ConfiguredModelMessage{
			extensionv1.ConfiguredModelMessage_builder{
				Role: new(extensionv1.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_USER), Text: new("first user"),
			}.Build(),
			extensionv1.ConfiguredModelMessage_builder{
				Role: new(extensionv1.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_ASSISTANT),
				Text: new("assistant reply"),
			}.Build(),
			extensionv1.ConfiguredModelMessage_builder{
				Role: new(extensionv1.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_USER), Text: new("second user"),
			}.Build(),
		},
	}.Build()
	return binding.StartConfiguredModel(ctx, request)
}
