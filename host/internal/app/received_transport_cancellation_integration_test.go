//go:build integration

package app

import (
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestReceivedTransportCancellationStopsCore checks Core abort and later-work suppression through the public stream.
func TestReceivedTransportCancellationStopsCore(t *testing.T) {
	const (
		helperEnvironment = "GLYPH_EXTENSION_RUNTIME_HELPER"
		peerMode          = "received-transport-canceled"
		sourceText        = "received transport cancellation diagnostic Ω"
		operationID       = "received-transport-conversation"
	)
	// Arrange the existing runtime peer process and a provider response with two tool calls.
	t.Setenv(helperEnvironment, peerMode)
	directory := buildReceivedTransportExtension(t)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	var requests atomic.Int64
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).AnyTimes().DoAndReturn(func(*http.Request) (*http.Response, error) {
		payload := finalResponseSSE
		if requests.Add(1) == 1 {
			payload = receivedTransportToolResponse()
		}
		return &http.Response{
			Status: "", StatusCode: http.StatusOK, Proto: "", ProtoMajor: 0, ProtoMinor: 0,
			Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload)), ContentLength: 0,
			TransferEncoding: nil, Close: false, Uncompressed: false, Trailer: nil, Request: nil, TLS: nil,
		}, nil
	})
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	defer fixture.closeOwner(t)
	// Act through the real public conversation, Core, Host tool adapters and extension Runtime.
	require.NoError(t, fixture.stream.Send(userRequest(operationID, "Execute the requested tools.")))
	requested := make(map[string]struct{})
	executed := make(map[string]struct{})
	var response *programmaticpb.ModelResponse
	var summary *programmaticpb.AgentSummary
	var terminal *programmaticpb.HostEvent
	for {
		event, receiveErr := fixture.stream.Recv()
		require.NoError(t, receiveErr)
		if event.GetOperationId() != operationID {
			continue
		}
		if progress := event.GetEvent().GetProgress().GetAgentEvent(); progress != nil {
			if call := progress.GetFinalToolCall(); call != nil {
				requested[call.GetCallId()] = struct{}{}
			}
			if execution := progress.GetToolExecution(); execution != nil {
				executed[execution.GetCallId()] = struct{}{}
			}
			if modelResponse := progress.GetModelResponse(); modelResponse != nil {
				response = modelResponse
			}
			if agentSummary := progress.GetAgent(); agentSummary != nil {
				summary = agentSummary
			}
		}
		if event.GetEvent().HasFailed() || event.GetEvent().HasCompleted() || event.GetEvent().HasCanceled() {
			terminal = event.GetEvent()
			break
		}
	}
	// Assert that Core preserves Aborted while the public adapter reports the independent unavailable failure.
	require.NoError(t, t.Context().Err())
	require.NotNil(t, summary)
	require.Equal(t, programmaticpb.RunOutcome_RUN_OUTCOME_ABORTED, summary.GetOutcome())
	require.Contains(t, summary.GetErrorMessage(), sourceText)
	require.NotNil(t, response)
	require.Equal(t, programmaticpb.ModelOutcome_MODEL_OUTCOME_TOOL_USE, response.GetOutcome())
	require.NotNil(t, terminal.GetFailed())
	require.Contains(t, terminal.GetFailed().GetMessage(), sourceText)
	require.Equal(t, map[string]struct{}{"call-1": {}, "call-2": {}}, requested)
	require.Equal(t, map[string]struct{}{"call-1": {}}, executed)
	require.Equal(t, int64(1), requests.Load())
}

// buildReceivedTransportExtension exposes the existing runtime helper as an application extension executable.
func buildReceivedTransportExtension(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	executable := filepath.Join(directory, "peer.test")
	command := exec.CommandContext(t.Context(), "go", "test", "-c", "-tags=integration", "-o", executable,
		"./host/internal/infra/plugins/extension/runtime")
	command.Dir = repoRoot(t)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	const wrapperPrefix = "#!/bin/sh\nexec \""
	const wrapperSuffix = "\" -test.run='^TestRuntimeHelperProcess$'\n"
	wrapper := wrapperPrefix + executable + wrapperSuffix
	// The launcher lives in a separate directory so Host discovers only the selected helper process.
	extensionDirectory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(extensionDirectory, "received-transport"), []byte(wrapper), 0o700))
	return extensionDirectory
}

// receivedTransportToolResponse supplies two finalized calls to exercise Core's later-tool guard.
func receivedTransportToolResponse() string {
	payload := strings.ReplaceAll(toolResponseSSE, `"bash"`, `"read"`)
	payload = strings.ReplaceAll(payload, `\"command\":\"printf tool-ok\"`, `\"path\":\"notes.txt\"`)
	const completedPrefix = `data: {"type":"response.completed"`
	end := strings.Index(payload, completedPrefix)
	const addedPrefix = `data: {"type":"response.output_item.added"`
	start := strings.Index(payload, addedPrefix)
	second := strings.NewReplacer(`"output_index":1`, `"output_index":2`, "fc-1", "fc-2", "call-1", "call-2").
		Replace(payload[start:end])
	return payload[:end] + second + payload[end:]
}
