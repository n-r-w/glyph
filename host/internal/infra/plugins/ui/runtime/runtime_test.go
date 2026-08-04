package runtime

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// runtimeContractService records Host frames and returns every supported UI command.
type runtimeContractService struct {
	uipb.UnimplementedUIServiceServer
	received chan *uipb.OpenRequest
}

// TestChannelMapsEveryFrameAndCommand verifies the complete generated transport boundary.
func TestChannelMapsEveryFrameAndCommand(t *testing.T) {
	t.Parallel()

	service := &runtimeContractService{
		UnimplementedUIServiceServer: uipb.UnimplementedUIServiceServer{},
		received:                     make(chan *uipb.OpenRequest, 5),
	}
	client := uisdk.TestClient(t, service)
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	transport := &channel{stream: stream, mutex: sync.Mutex{}}
	frames := []domainui.Frame{
		testInitializationFrame(),
		testLifecycleFrame(),
		testSimpleFrame(domainui.FrameAuthorization, "https://auth.example"),
		testSimpleFrame(domainui.FrameInformation, "information"),
		testErrorFrame(),
	}

	for _, frame := range frames {
		require.NoError(t, transport.Send(frame))
	}
	for range frames {
		assert.NotNil(t, <-service.received)
	}
	for _, expected := range []domainui.Command{
		{Kind: domainui.CommandSubmit, Text: "request"},
		{Kind: domainui.CommandStop, Text: ""},
		{Kind: domainui.CommandRetryAuthentication, Text: ""},
		{Kind: domainui.CommandQuit, Text: ""},
	} {
		command, receiveErr := transport.Receive()
		require.NoError(t, receiveErr)
		assert.Equal(t, expected, command)
	}
}

// TestMapInitializationPreservesWarningAndExtensionPath verifies public UI diagnostics mapping.
func TestMapInitializationPreservesWarningAndExtensionPath(t *testing.T) {
	t.Parallel()

	mapped := mapInitialization(domainui.Initialization{
		SelectedUIID: "ui",
		StartupContent: []domainui.StartupContent{{
			Severity: domainui.ContentSeverityWarning, Text: "excluded optional UI",
		}},
		Extensions: []domainui.ExtensionAvailability{{
			PluginID: "tools", Path: "/plugins/tools", Tools: []string{"read"},
		}},
		Availability: domainui.AvailabilityCheckingAuthentication,
	})

	require.Len(t, mapped.GetStartupContent(), 1)
	assert.Equal(t, uipb.ContentSeverity_CONTENT_SEVERITY_WARNING, mapped.GetStartupContent()[0].GetSeverity())
	require.Len(t, mapped.GetExtensions(), 1)
	assert.Equal(t, "/plugins/tools", mapped.GetExtensions()[0].GetPath())
}

// TestMappingRejectsMissingPayloads verifies malformed stream items fail explicitly.
func TestMappingRejectsMissingPayloads(t *testing.T) {
	t.Parallel()

	_, err := mapFrame(domainui.Frame{
		Kind: 0, Initialization: emptyTestInitialization(), Lifecycle: emptyTestLifecycle(),
		AuthorizationURL: "", Text: "", RetryAuthentication: false,
	})
	require.Error(t, err)
	_, err = mapCommand(&uipb.OpenResponse{Content: nil})
	require.Error(t, err)
}

// GetCapabilities returns the non-terminal capability used by the transport test.
func (*runtimeContractService) GetCapabilities(
	_ context.Context,
	_ *uipb.GetCapabilitiesRequest,
) (*uipb.GetCapabilitiesResponse, error) {
	return &uipb.GetCapabilitiesResponse{ControlsTerminal: false}, nil
}

// Open receives every Host frame before returning the complete command set.
func (s *runtimeContractService) Open(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
) error {
	for range cap(s.received) {
		request, err := stream.Recv()
		if err != nil {
			return err
		}
		s.received <- request
	}
	for _, response := range []*uipb.OpenResponse{
		{Content: &uipb.OpenResponse_Submit{Submit: &uipb.SubmitCommand{Text: "request"}}},
		{Content: &uipb.OpenResponse_Stop{Stop: &uipb.StopCommand{}}},
		{Content: &uipb.OpenResponse_RetryAuthentication{
			RetryAuthentication: &uipb.RetryAuthenticationCommand{},
		}},
		{Content: &uipb.OpenResponse_Quit{Quit: &uipb.QuitCommand{}}},
	} {
		if err := stream.Send(response); err != nil {
			return err
		}
	}
	return nil
}

// testInitializationFrame creates one complete initialization mapping fixture.
func testInitializationFrame() domainui.Frame {
	return domainui.Frame{
		Kind: domainui.FrameInitialization,
		Initialization: domainui.Initialization{
			SelectedUIID: "ui",
			StartupContent: []domainui.StartupContent{{
				Severity: domainui.ContentSeverityInformation, Text: "ready",
			}},
			Extensions: []domainui.ExtensionAvailability{{
				PluginID: "tools", Path: "/plugins/tools", Tools: []string{"read"},
			}},
			Availability: domainui.AvailabilityCheckingAuthentication,
		},
		Lifecycle: emptyTestLifecycle(), AuthorizationURL: "", Text: "", RetryAuthentication: false,
	}
}

// testLifecycleFrame creates one complete lifecycle mapping fixture.
func testLifecycleFrame() domainui.Frame {
	lifecycle := emptyTestLifecycle()
	lifecycle.Type = domainui.LifecycleToolExecutionUpdate
	lifecycle.RunID = "run"
	lifecycle.Position = 2
	lifecycle.Text = "progress"
	lifecycle.ToolCallID = "call"
	lifecycle.ToolName = "read"
	lifecycle.ProgressChannel = domainui.ProgressChannelStdout
	lifecycle.IsError = true
	lifecycle.Outcome = "failed"
	lifecycle.ErrorMessage = "safe"
	lifecycle.Availability = domainui.AvailabilityRunning
	return domainui.Frame{
		Kind: domainui.FrameLifecycle, Initialization: emptyTestInitialization(), Lifecycle: lifecycle,
		AuthorizationURL: "", Text: "", RetryAuthentication: false,
	}
}

// testSimpleFrame creates one authorization or information mapping fixture.
func testSimpleFrame(kind domainui.FrameKind, text string) domainui.Frame {
	frame := domainui.Frame{
		Kind: kind, Initialization: emptyTestInitialization(), Lifecycle: emptyTestLifecycle(),
		AuthorizationURL: "", Text: "", RetryAuthentication: false,
	}
	if kind == domainui.FrameAuthorization {
		frame.AuthorizationURL = text
	} else {
		frame.Text = text
	}
	return frame
}

// testErrorFrame creates one retryable error mapping fixture.
func testErrorFrame() domainui.Frame {
	return domainui.Frame{
		Kind: domainui.FrameError, Initialization: emptyTestInitialization(), Lifecycle: emptyTestLifecycle(),
		AuthorizationURL: "", Text: "safe error", RetryAuthentication: true,
	}
}

// emptyTestInitialization returns explicit zero values for non-initialization fixtures.
func emptyTestInitialization() domainui.Initialization {
	return domainui.Initialization{
		SelectedUIID: "", StartupContent: nil, Extensions: nil, Availability: 0,
	}
}

// emptyTestLifecycle returns explicit zero values for non-lifecycle fixtures.
func emptyTestLifecycle() domainui.Lifecycle {
	return domainui.Lifecycle{
		Type: 0, RunID: "", Position: 0, Text: "", ToolCallID: "", ToolName: "",
		ProgressChannel: 0, IsError: false, Outcome: "", ErrorMessage: "", Availability: 0,
	}
}
