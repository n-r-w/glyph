//go:build integration

package app

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/samber/lo"

	"google.golang.org/grpc"

	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// appUIService records initialization and terminates through one quit command.
type appUIService struct {
	uipb.UnimplementedUIServiceServer
}

// TestUIPluginHelperProcess serves the fake UI when this test binary is a child process.
func TestUIPluginHelperProcess(t *testing.T) {
	t.Parallel()

	// Arrange helper mode through the subprocess environment.
	if os.Getenv(appUIHelperEnvironment) == "serve" {
		// Act by serving the test UI protocol in the helper process.
		uisdk.Serve(&appUIService{
			UnimplementedUIServiceServer: uipb.UnimplementedUIServiceServer{},
		})
	}

	// Assert helper protocol behavior is observed by the parent process tests.
}

// GetCapabilities declares a non-terminal fake UI for application composition tests.
func (*appUIService) GetCapabilities(
	_ context.Context,
	_ *uipb.GetCapabilitiesRequest,
) (*uipb.GetCapabilitiesResponse, error) {
	controlsTerminal := os.Getenv(appUITerminalEnvironment) == "1"
	if os.Getenv(appUIBehaviorEnvironment) == "snapshot" {
		_ = os.WriteFile(os.Getenv(appUITraceEnvironment), []byte(strconv.Itoa(os.Getpid())), 0o600)
	}
	return uipb.GetCapabilitiesResponse_builder{
		ControlsTerminal: new(controlsTerminal),
	}.Build(), nil
}

// Open records the first frame and sends the authoritative quit command.
func (*appUIService) Open(stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse]) error {
	frame, err := stream.Recv()
	if err != nil {
		return err
	}
	initialization := frame.GetInitialization()
	startupText := lo.Map(initialization.GetStartupContent(), func(content *uipb.StartupContent, _ int) string {
		return content.GetText()
	})
	trace := fmt.Sprintf(
		"%d\n%s\n%s\n",
		os.Getpid(), initialization.GetSelectedUiId(), strings.Join(startupText, "\n"),
	)
	behavior := os.Getenv(appUIBehaviorEnvironment)
	if behavior == "session-restart" {
		return runSessionRestartUI(stream, initialization)
	}
	if behavior == "session-usage-restart" {
		return runSessionUsageRestartUI(stream, initialization)
	}
	if behavior == "session-recovery" {
		return runSessionRecoveryUI(stream, initialization)
	}
	if behavior == "runtime-failure" {
		return runRuntimeFailureUI(stream, initialization)
	}
	if behavior != "semantic" {
		if err := os.WriteFile(os.Getenv(appUITraceEnvironment), []byte(trace), 0o600); err != nil {
			return err
		}
	}
	if os.Getenv(appUITerminalEnvironment) == "1" {
		terminalFile, terminalErr := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if terminalErr != nil {
			return terminalErr
		}
		if _, terminalErr = term.MakeRaw(terminalFile.Fd()); terminalErr != nil {
			return terminalErr
		}
		_, terminalErr = terminalFile.WriteString(
			ansi.SetMode(ansi.ModeAltScreenSaveCursor, ansi.ModeBracketedPaste) + ansi.HideCursor,
		)
		if terminalErr != nil {
			return terminalErr
		}
	}
	if os.Getenv(appUIBehaviorEnvironment) == "crash" {
		os.Exit(23)
	}
	if os.Getenv(appUIBehaviorEnvironment) == "authentication" {
		for {
			frame, receiveErr := stream.Recv()
			if receiveErr != nil {
				return receiveErr
			}
			lifecycle := frame.GetLifecycle()
			if lifecycle == nil || lifecycle.GetType() != uipb.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED {
				continue
			}
			availability := lifecycle.GetAvailability()
			if availability != uipb.Availability_AVAILABILITY_IDLE &&
				availability != uipb.Availability_AVAILABILITY_AUTHENTICATING &&
				availability != uipb.Availability_AVAILABILITY_AUTHENTICATION_FAILED {
				continue
			}
			if err := os.WriteFile(os.Getenv(appUITraceEnvironment), []byte(availability.String()), 0o600); err != nil {
				return err
			}
			//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active Quit field.
			return stream.Send(uipb.OpenResponse_builder{
				Quit: &uipb.QuitCommand{},
			}.Build())
		}
	}
	//nolint:nestif // The helper serves one explicit lifecycle mode for this process fixture.
	if os.Getenv(appUIBehaviorEnvironment) == "semantic" {
		// Wait for Host readiness because commands received during authentication are rejected as busy.
		for {
			frame, err = stream.Recv()
			if err != nil {
				return err
			}
			if hostError := frame.GetError(); hostError != nil {
				return fmt.Errorf("wait for semantic UI Host idle: %s", hostError.GetText())
			}
			lifecycle := frame.GetLifecycle()
			if lifecycle == nil || lifecycle.GetType() != uipb.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED {
				continue
			}
			if lifecycle.GetAvailability() == uipb.Availability_AVAILABILITY_IDLE {
				break
			}
		}
		//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active Submit field.
		if err := stream.Send(uipb.OpenResponse_builder{
			Submit: uipb.SubmitCommand_builder{
				Text: new("read input.txt"),
			}.Build(),
		}.Build()); err != nil {
			return err
		}
		file, err := os.OpenFile(os.Getenv(appUITraceEnvironment), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer func() {
			if closeErr := file.Close(); closeErr != nil {
				slog.ErrorContext(stream.Context(), "close semantic UI trace", "error", closeErr)
			}
		}()
		settled := false
		for {
			frame, err := stream.Recv()
			if err != nil {
				return err
			}
			if lifecycle := frame.GetLifecycle(); lifecycle != nil {
				payload, marshalErr := json.Marshal(map[string]any{
					"type":                 lifecycle.GetType(),
					"text":                 lifecycle.GetText(),
					"model_text":           lifecycle.GetModelResponse().GetText(),
					"tool_name":            lifecycle.GetToolName(),
					"tool_status":          !lifecycle.GetIsError(),
					"outcome":              lifecycle.GetOutcome(),
					"settled":              lifecycle.GetType() == uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED,
					"availability":         lifecycle.GetAvailability(),
					"tool_result_contents": semanticToolResultContents(lifecycle.GetToolResultContents()),
				})
				if marshalErr != nil {
					return marshalErr
				}
				if _, writeErr := fmt.Fprintf(file, "%s\n", payload); writeErr != nil {
					return writeErr
				}
				if lifecycle.GetType() == uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED {
					settled = true
				}
				if settled && lifecycle.GetType() == uipb.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED &&
					lifecycle.GetAvailability() == uipb.Availability_AVAILABILITY_IDLE {
					//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active Quit field.
					return stream.Send(uipb.OpenResponse_builder{
						Quit: &uipb.QuitCommand{},
					}.Build())
				}
			}
		}
	}
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active Quit field.
	return stream.Send(uipb.OpenResponse_builder{
		Quit: &uipb.QuitCommand{},
	}.Build())
}
