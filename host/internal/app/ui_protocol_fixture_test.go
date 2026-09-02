//go:build integration

package app

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/samber/lo"
	"go.uber.org/mock/gomock"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// TestUIPluginHelperProcess serves the fake UI when this test binary is a child process.
func TestUIPluginHelperProcess(t *testing.T) {
	t.Parallel()
	// Arrange the generated UI service mock when the test binary runs as a plugin child.
	// Act by calling uisdk.Serve when the child-process mode is active.
	// Assert the child process exposes the configured UI fixture behavior.
	if os.Getenv(appUIHelperEnvironment) == "serve" {
		uisdk.Serve(newAppUIService(t))
	}
}

// newAppUIService configures deterministic initialization and Host behavior.
func newAppUIService(t *testing.T) *uisdk.MockService {
	t.Helper()
	controller := gomock.NewController(t)
	service := uisdk.NewMockService(controller)
	initializationOperation := uisdk.NewMockInitializeOperation(controller)
	service.EXPECT().PrepareInitialize(gomock.Any(), gomock.Any()).DoAndReturn(func(
		_ context.Context,
		initialization *uiv1.Initialization,
	) (uisdk.InitializeOperation, error) {
		startupText := lo.Map(initialization.GetStartupContent(), func(content *uiv1.StartupContent, _ int) string {
			return content.GetText()
		})
		trace := fmt.Sprintf(
			"%d\n%s\n%s\n",
			os.Getpid(), initialization.GetSelectedUiId(), strings.Join(startupText, "\n"),
		)
		behavior := os.Getenv(appUIBehaviorEnvironment)
		if behavior != "semantic" && behavior != "authentication" {
			if err := os.WriteFile(os.Getenv(appUITraceEnvironment), []byte(trace), 0o600); err != nil {
				return nil, err
			}
		}
		return initializationOperation, nil
	})
	initializationOperation.EXPECT().Run(gomock.Any()).Return(new(uiv1.Initialized), nil)
	initializationOperation.EXPECT().Release()
	service.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, host *uisdk.Host) error {
		switch os.Getenv(appUIBehaviorEnvironment) {
		case "crash":
			os.Exit(23)
		case "authentication":
			return runAuthenticationFixture(ctx, host)
		case "semantic":
			return runSemanticFixture(ctx, host)
		default:
			return host.Close(ctx)
		}
		return nil
	})
	service.EXPECT().Close().Return(nil)
	return service
}

// runAuthenticationFixture records one stable availability and closes the fake UI.
func runAuthenticationFixture(ctx context.Context, host *uisdk.Host) error {
	recordedAuthenticating := false
	for {
		event, err := host.Receive(ctx)
		if err != nil {
			return err
		}
		availability := event.GetAvailabilityChanged().GetAvailability()
		if availability == uiv1.Availability_AVAILABILITY_AUTHENTICATING {
			if err := os.WriteFile(os.Getenv(appUITraceEnvironment), []byte(availability.String()), 0o600); err != nil {
				return err
			}
			recordedAuthenticating = true
			continue
		}
		if availability != uiv1.Availability_AVAILABILITY_IDLE &&
			availability != uiv1.Availability_AVAILABILITY_AUTHENTICATION_FAILED {
			continue
		}
		if !recordedAuthenticating {
			if err := os.WriteFile(os.Getenv(appUITraceEnvironment), []byte(availability.String()), 0o600); err != nil {
				return err
			}
		}
		return host.Close(ctx)
	}
}

// runSemanticFixture records retained lifecycle, settlement, and availability events.
func runSemanticFixture(ctx context.Context, host *uisdk.Host) (returnErr error) {
	if err := waitForIdle(ctx, host); err != nil {
		return err
	}
	request := new(uiv1.UIRequest)
	request.SetSubmit(uiv1.SubmitCommand_builder{Text: new("read input.txt")}.Build())
	operation, err := host.Start(ctx, "semantic-submit", request)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(os.Getenv(appUITraceEnvironment), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	var progressWriteErr error
	_, err = operation.Wait(ctx, func(progress *uiv1.HostProgress) {
		lifecycle := progress.GetAgentEvent()
		if lifecycle == nil || progressWriteErr != nil {
			return
		}
		progressWriteErr = writeSemanticObservation(file, map[string]any{
			"type": lifecycle.GetType(), "text": lifecycle.GetText(),
			"model_text": lifecycle.GetModelResponse().GetText(), "tool_name": lifecycle.GetToolName(),
			"tool_status": !lifecycle.GetIsError(), "outcome": lifecycle.GetOutcome(), "settled": false,
			"availability":         uiv1.Availability_AVAILABILITY_UNSPECIFIED,
			"tool_result_contents": semanticToolResultContents(lifecycle.GetToolResultContents()),
		})
	})
	if err != nil {
		return err
	}
	if progressWriteErr != nil {
		return progressWriteErr
	}
	if err := writeSemanticObservation(file, map[string]any{"settled": true}); err != nil {
		return err
	}
	if err := waitForIdle(ctx, host); err != nil {
		return err
	}
	if err := writeSemanticObservation(file, map[string]any{
		"availability": uiv1.Availability_AVAILABILITY_IDLE,
	}); err != nil {
		return err
	}
	return host.Close(ctx)
}

// waitForIdle waits for one idle connection event.
func waitForIdle(ctx context.Context, host *uisdk.Host) error {
	for {
		event, err := host.Receive(ctx)
		if err != nil {
			return err
		}
		if event.GetAvailabilityChanged().GetAvailability() == uiv1.Availability_AVAILABILITY_IDLE {
			return nil
		}
	}
}

// writeSemanticObservation appends one JSON observation.
func writeSemanticObservation(file *os.File, observation map[string]any) error {
	payload, err := json.Marshal(observation)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(file, "%s\n", payload)
	return err
}

// semanticToolResultContents converts retained text result content into fixture values.
func semanticToolResultContents(contents []*uiv1.ToolResultContent) []map[string]any {
	return lo.Map(contents, func(content *uiv1.ToolResultContent, _ int) map[string]any {
		return map[string]any{"text": content.GetText()}
	})
}
