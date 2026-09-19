//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestRetryAcrossUIAndProgrammaticModes verifies assembled reset, progress, persistence, and tool ownership.
func TestRetryAcrossUIAndProgrammaticModes(t *testing.T) {
	// Arrange one public extension executable shared by isolated mode scenarios.
	directory := buildPublicExtensionFixture(t)
	for _, scenario := range []struct {
		// name identifies the assembled client mode.
		name string
		// mode selects the public client contract.
		mode cli.Mode
	}{
		{name: "ui", mode: cli.ModeUI},
		{name: "programmatic", mode: cli.ModeRPC},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			mode := scenario.mode
			paths := testPaths(t, retryModeSettings)
			writeProgrammaticCredentials(t, paths)
			transport, bodies := retryingProviderTransport(t)
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })

			// Act through the selected real application assembly and public client contract.
			var eventTypes []int32
			var completedAttempts, attemptLimit int64
			switch mode {
			case cli.ModeUI:
				tracePath := filepath.Join(t.TempDir(), "retry-ui.jsonl")
				t.Setenv(appUIBehaviorEnvironment, "semantic")
				t.Setenv(appUITraceEnvironment, tracePath)
				uiDirectory := t.TempDir()
				writeUIExecutable(t, uiDirectory, "Retry_UI")
				err := runWithPaths(t.Context(), paths, cli.Command{
					Mode: cli.ModeUI, Headless: headless.Command{}, ExtensionDirectory: directory,
					UIDirectory: uiDirectory, UIID: "retry-ui", SocketPath: "",
				}, &bytes.Buffer{}, &bytes.Buffer{})
				require.NoError(t, err)
				eventTypes, completedAttempts, attemptLimit = readUIRetryTrace(t, tracePath)
				inspection := startProgrammaticFixtureWithExtension(t, paths, directory)
				assertRetryMessages(t, activeProgrammaticMessages(t, inspection))
				inspection.closeOwner(t)
			case cli.ModeRPC:
				fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
				eventTypes, completedAttempts, attemptLimit = completeProgrammaticRetryRequest(
					t, fixture, userRequest("retry-run", "read input.txt"),
				)
				assertRetryMessages(t, activeProgrammaticMessages(t, fixture))
				fixture.closeOwner(t)
			case cli.ModeHeadless:
				require.FailNow(t, "unexpected retry integration mode")
			}

			// Assert partial output resets before progress and one replacement terminal result.
			assertRetryEventOrder(t, mode, eventTypes)
			assert.Equal(t, int64(1), completedAttempts)
			assert.Equal(t, int64(2), attemptLimit)
			actualBodies := bodies.snapshot()
			require.Len(t, actualBodies, 3)
			assertSingleCompletedToolInRequest(t, actualBodies[1])
			assertSingleCompletedToolInRequest(t, actualBodies[2])
		})
	}
}

// TestPublicRetryHandlerAndConfiguredModelProgress verifies public handler invocation and scoped nested progress.
func TestPublicRetryHandlerAndConfiguredModelProgress(t *testing.T) {
	// Arrange a public extension handler and configured-model request with one transient nested failure.
	directory := buildPublicExtensionFixture(t)
	paths := testPaths(t, retryModeSettings)
	writeProgrammaticCredentials(t, paths)
	signals := t.TempDir()
	t.Setenv("GLYPH_EXTERNAL_SIGNALS", signals)
	t.Setenv("GLYPH_EXTERNAL_RETRY_HANDLER", "1")
	transport, bodies := configuredProgressProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })

	// Act through the public Extension SDK configured-model operation.
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	completeProgrammaticRequest(t, fixture, userRequest("configured-progress", "request configured retry progress"))
	fixture.closeOwner(t)

	// Assert the public handler ran and configured retry progress stayed in the nested operation result.
	_, err := os.Stat(filepath.Join(signals, "retry-handler-invoked"))
	require.NoError(t, err)
	actualBodies := bodies.snapshot()
	require.Len(t, actualBodies, 4)
	var report struct {
		// ResultJSON contains the configured-model protobuf JSON.
		ResultJSON string `json:"result_json"`
		// Progress contains operation-scoped configured-model retry updates.
		Progress []struct {
			// CompletedAttempts is the completed nested-attempt count.
			CompletedAttempts int64 `json:"completed_attempts"`
			// AttemptLimit is the effective nested-attempt limit.
			AttemptLimit int64 `json:"attempt_limit"`
			// DelayMilliseconds is the accepted nested retry delay.
			DelayMilliseconds int64 `json:"delay_milliseconds"`
		} `json:"progress"`
	}
	require.NoError(t, json.Unmarshal([]byte(externalToolOutput(t, actualBodies[3])), &report))
	require.Len(t, report.Progress, 1)
	assert.Equal(t, int64(1), report.Progress[0].CompletedAttempts)
	assert.Equal(t, int64(2), report.Progress[0].AttemptLimit)
	assert.Zero(t, report.Progress[0].DelayMilliseconds)
	result := new(extensionpb.ConfiguredModelResult)
	require.NoError(t, protojson.Unmarshal([]byte(report.ResultJSON), result))
	assert.Equal(t, "Configured response.", result.GetContent()[0].GetText().GetText())
}

// configuredProgressProviderTransport returns a tool call, nested failure and success, then outer success.
func configuredProgressProviderTransport(t *testing.T) (*MockHTTPRoundTripper, *requestBodies) {
	t.Helper()
	bodies := new(requestBodies)
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		position := bodies.append(body)
		response := finalResponseSSE
		switch position {
		case 1:
			response = strings.NewReplacer(
				`"bash"`, `"external"`,
				`{\"command\":\"printf tool-ok\"}`, `{\"mode\":\"configured-request-progress\"}`,
			).Replace(toolResponseSSE)
		case 2:
			response = retryPartialFailureSSE
		case 3:
			response = configuredModelResponseSSE
		case 4:
		default:
			t.Fatalf("unexpected configured-progress provider request %d", position)
		}
		return &http.Response{
			StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(response)),
			Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0, ContentLength: 0, TransferEncoding: nil,
			Close: false, Uncompressed: false, Trailer: nil, Request: nil, TLS: nil,
		}, nil
	}).AnyTimes()
	return transport, bodies
}

// TestConfiguredModelSDKFailuresPreserveLogicalIdentity verifies Wait APIs retain typed timeout and mixed failures.
//
//nolint:paralleltest // The test replaces process-global provider transport.
func TestConfiguredModelSDKFailuresPreserveLogicalIdentity(t *testing.T) {
	// Arrange the external SDK fixture with timeout exhaustion and a mixed progress callback failure.
	directory := buildPublicExtensionFixture(t)
	paths := testPaths(t, retryModeSettings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := configuredFailureProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })

	// Act through two real configured-model SDK operations inside the external process.
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	completeProgrammaticRequest(t, fixture, userRequest("configured-failures", "request configured failures"))
	fixture.closeOwner(t)

	// Assert Wait and WaitWithProgress expose their logical categories and complete acquired causes.
	actualBodies := bodies.snapshot()
	require.Len(t, actualBodies, 6)
	var report struct {
		// Wait contains the public Wait failure.
		Wait struct {
			// Code is the public SDK failure category.
			Code string `json:"code"`
			// Error is the complete public SDK failure text.
			Error string `json:"error"`
		} `json:"wait"`
		// WaitWithProgress contains the public progress-wait failure.
		WaitWithProgress struct {
			// Code is the public SDK failure category.
			Code string `json:"code"`
			// Error is the complete public SDK failure text.
			Error string `json:"error"`
		} `json:"wait_with_progress"`
		// ProgressCount is the number of delivered retry updates.
		ProgressCount int `json:"progress_count"`
	}
	require.NoError(t, json.Unmarshal([]byte(externalToolOutput(t, actualBodies[len(actualBodies)-1])), &report))
	assert.Equal(t, "RETRY_EXHAUSTED", report.Wait.Code)
	assert.Contains(t, report.Wait.Error, "first configured timeout")
	assert.Contains(t, report.Wait.Error, "second configured timeout")
	assert.Equal(t, "MODEL_FAILED", report.WaitWithProgress.Code)
	assert.Contains(t, report.WaitWithProgress.Error, "configured mixed source failure")
	assert.Contains(t, report.WaitWithProgress.Error, context.Canceled.Error())
	assert.Contains(t, report.WaitWithProgress.Error, "mixed final provider failure")
	assert.Equal(t, 1, report.ProgressCount)
}

// configuredFailureProviderTransport supplies two timeouts, one mixed-progress source failure, and outer success.
func configuredFailureProviderTransport(t *testing.T) (*MockHTTPRoundTripper, *requestBodies) {
	t.Helper()
	bodies := new(requestBodies)
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		position := bodies.append(body)
		response := finalResponseSSE
		switch position {
		case 1:
			response = strings.NewReplacer(
				`"bash"`, `"external"`,
				`{\"command\":\"printf tool-ok\"}`, `{\"mode\":\"configured-request-failures\"}`,
			).Replace(toolResponseSSE)
		case 2:
			return nil, fmt.Errorf("first configured timeout: %w", context.DeadlineExceeded)
		case 3:
			return nil, fmt.Errorf("second configured timeout: %w", context.DeadlineExceeded)
		case 4:
			response = strings.Replace(
				retryPartialFailureSSE, "temporary provider failure", "configured mixed source failure", 1,
			)
		case 5:
			return nil, errors.Join(context.Canceled, errors.New("mixed final provider failure"))
		case 6:
		default:
			response = finalResponseSSE
		}
		return &http.Response{
			StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(response)),
			Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0, ContentLength: 0, TransferEncoding: nil,
			Close: false, Uncompressed: false, Trailer: nil, Request: nil, TLS: nil,
		}, nil
	}).AnyTimes()
	return transport, bodies
}

// retryingProviderTransport returns one tool call, one partial transient failure, and one successful replacement.
func retryingProviderTransport(t *testing.T) (*MockHTTPRoundTripper, *requestBodies) {
	t.Helper()
	bodies := new(requestBodies)
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		position := bodies.append(body)
		response := finalResponseSSE
		switch position {
		case 1:
			response = strings.NewReplacer(
				`"bash"`, `"external"`, `{\"command\":\"printf tool-ok\"}`, `{\"mode\":\"ordinary\"}`,
			).Replace(toolResponseSSE)
		case 2:
			response = retryPartialFailureSSE
		case 3:
		default:
			t.Fatalf("unexpected retry provider request %d", position)
		}
		return &http.Response{
			StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(response)),
			Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0, ContentLength: 0, TransferEncoding: nil,
			Close: false, Uncompressed: false, Trailer: nil, Request: nil, TLS: nil,
		}, nil
	}).AnyTimes()
	return transport, bodies
}

// completeProgrammaticRetryRequest returns correlated agent event types and retry progress.
func completeProgrammaticRetryRequest(
	t *testing.T,
	fixture *programmaticFixture,
	request *programmaticpb.OpenRequest,
) ([]int32, int64, int64) {
	t.Helper()
	require.NoError(t, fixture.stream.Send(request))
	var eventTypes []int32
	var completedAttempts, attemptLimit int64
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetOperationId() != request.GetOperationId() || !response.HasEvent() {
			continue
		}
		event := response.GetEvent()
		if progress := event.GetProgress().GetAgentEvent(); progress != nil {
			eventTypes = append(eventTypes, int32(progress.GetType()))
			if retry := progress.GetRetryProgress(); retry != nil {
				completedAttempts, attemptLimit = retry.GetCompletedAttempts(), retry.GetAttemptLimit()
			}
		}
		if event.HasCompleted() {
			return eventTypes, completedAttempts, attemptLimit
		}
		if event.HasRejected() || event.HasFailed() || event.HasCanceled() {
			require.FailNow(t, "retry operation did not complete", "event: %v", event)
		}
	}
}

// readUIRetryTrace returns UI lifecycle types and operation-scoped retry progress.
func readUIRetryTrace(t *testing.T, path string) ([]int32, int64, int64) {
	t.Helper()
	payload, err := os.ReadFile(path)
	require.NoError(t, err)
	var eventTypes []int32
	var completedAttempts, attemptLimit int64
	for line := range strings.SplitSeq(strings.TrimSpace(string(payload)), "\n") {
		var item struct {
			// Type identifies one UI lifecycle event.
			Type uipb.LifecycleType `json:"type"`
			// RetryCompletedAttempts contains retry progress when present.
			RetryCompletedAttempts int64 `json:"retry_completed_attempts"`
			// RetryAttemptLimit contains retry progress when present.
			RetryAttemptLimit int64 `json:"retry_attempt_limit"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &item))
		if item.Type != uipb.LifecycleType_LIFECYCLE_TYPE_UNSPECIFIED {
			eventTypes = append(eventTypes, int32(item.Type))
		}
		if item.RetryCompletedAttempts != 0 {
			completedAttempts, attemptLimit = item.RetryCompletedAttempts, item.RetryAttemptLimit
		}
	}
	return eventTypes, completedAttempts, attemptLimit
}

// assertRetryEventOrder verifies partial content, reset, retry progress, and final terminal order.
func assertRetryEventOrder(t *testing.T, mode cli.Mode, eventTypes []int32) {
	t.Helper()
	textDelta := int32(programmaticpb.AgentEventType_AGENT_EVENT_TYPE_MODEL_TEXT_DELTA)
	responseReset := int32(programmaticpb.AgentEventType_AGENT_EVENT_TYPE_RESPONSE_RESET)
	retryProgress := int32(programmaticpb.AgentEventType_AGENT_EVENT_TYPE_RETRY_PROGRESS)
	messageEnd := int32(programmaticpb.AgentEventType_AGENT_EVENT_TYPE_MESSAGE_END)
	if mode == cli.ModeUI {
		textDelta = int32(uipb.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA)
		responseReset = int32(uipb.LifecycleType_LIFECYCLE_TYPE_RESPONSE_RESET)
		retryProgress = int32(uipb.LifecycleType_LIFECYCLE_TYPE_RETRY_PROGRESS)
		messageEnd = int32(uipb.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END)
	}
	partialIndex := slices.Index(eventTypes, textDelta)
	resetIndex := slices.Index(eventTypes, responseReset)
	retryIndex := slices.Index(eventTypes, retryProgress)
	terminalIndex := slicesLastIndex(eventTypes, messageEnd)
	require.GreaterOrEqual(t, partialIndex, 0)
	require.Greater(t, resetIndex, partialIndex)
	require.Greater(t, retryIndex, resetIndex)
	require.Greater(t, terminalIndex, retryIndex)
	assert.Equal(t, 2, countValue(eventTypes, messageEnd))
}

// assertSingleCompletedToolInRequest verifies retries retain but do not duplicate completed tool results.
func assertSingleCompletedToolInRequest(t *testing.T, body []byte) {
	t.Helper()
	var request struct {
		// Input contains provider request items.
		Input []struct {
			// Type identifies the provider input item.
			Type string `json:"type"`
		} `json:"input"`
	}
	require.NoError(t, json.Unmarshal(body, &request))
	count := 0
	for _, item := range request.Input {
		if item.Type == "function_call_output" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

// activeProgrammaticMessages queries the active session's public entries.
func activeProgrammaticMessages(t *testing.T, fixture *programmaticFixture) []*programmaticpb.HistoryEntry {
	t.Helper()
	messages := sendProgrammaticOperation(t, fixture, "retry-messages", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetMessages(new(programmaticpb.GetMessages))
	}).GetMessages().GetEntries()
	if len(messages) > 0 {
		return messages
	}
	listed := sendProgrammaticOperation(t, fixture, "retry-sessions", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetListSessions(new(programmaticpb.ListSessions))
	}).GetSessions().GetSessions()
	for index, stored := range listed {
		sessionID := stored.GetInfo().GetId()
		sendProgrammaticOperation(
			t,
			fixture,
			fmt.Sprintf("retry-resume-%d", index),
			func(request *programmaticpb.OpenRequest) {
				programmaticRequest(request).SetResumeSession(
					programmaticpb.ResumeSession_builder{SessionId: new(sessionID)}.Build(),
				)
			},
		)
		messages = sendProgrammaticOperation(
			t, fixture, fmt.Sprintf("retry-messages-%d", index), func(request *programmaticpb.OpenRequest) {
				programmaticRequest(request).SetGetMessages(new(programmaticpb.GetMessages))
			},
		).GetMessages().GetEntries()
		if len(messages) > 0 {
			return messages
		}
	}
	return nil
}

// assertRetryMessages verifies only one terminal model result and one completed tool were persisted.
func assertRetryMessages(t *testing.T, entries []*programmaticpb.HistoryEntry) {
	t.Helper()
	require.Len(t, entries, 4)
	assert.NotNil(t, entries[0].GetUser())
	assert.NotNil(t, entries[1].GetModel())
	require.NotNil(t, entries[2].GetToolResult())
	assert.False(t, entries[2].GetToolResult().GetIsError())
	assert.Equal(t, "ordinary complete", entries[2].GetToolResult().GetContents()[0].GetText())
	assert.NotNil(t, entries[3].GetModel())
	assert.Equal(t, "Request complete.", entries[3].GetModel().GetText())
}

// slicesLastIndex returns the final event position or -1.
func slicesLastIndex(values []int32, target int32) int {
	for index, value := range slices.Backward(values) {
		if value == target {
			return index
		}
	}
	return -1
}

// countValue counts exact event occurrences.
func countValue(values []int32, target int32) int {
	count := 0
	for _, value := range values {
		if value == target {
			count++
		}
	}
	return count
}

// retryModeSettings enables one immediate replacement attempt in assembled fixtures.
const retryModeSettings = configuredRequestSettings + `
retry:
  enabled: true
  maxRetries: 1
  delays: [0s]
  maxProviderDelay: 30s
`

// retryPartialFailureSSE emits partial text before a transient provider error.
const retryPartialFailureSSE = `data: {"type":"response.output_text.delta","output_index":0,` +
	`"content_index":0,"delta":"discarded partial"}` + "\n\n" +
	`data: {"type":"error","code":"server_error","message":"temporary provider failure"}` + "\n\n"
