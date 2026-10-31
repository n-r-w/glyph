//go:build integration

package app

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// steerRunID identifies the user request whose agent run receives steer messages.
	steerRunID = "steer-run"
	// steerFirstText is the steer message that the agent receives first.
	steerFirstText = "unique first steer instruction"
	// steerSecondText is the steer message that the agent receives second.
	steerSecondText = "unique second steer instruction"
	// steerToolOutput is the result text of the reference tool in ordinary mode.
	steerToolOutput = "ordinary complete"
	// steerNotActiveCode is the failure code of an enqueue without an active agent run.
	steerNotActiveCode = "RUN_NOT_ACTIVE"
	// steerPausedText is steer text whose input-text invocation the input reference pauses.
	steerPausedText = "glyph-pause-input stale steer"
	// steerPausedMarker identifies the paused steer text in provider requests and history.
	steerPausedMarker = "stale steer"
	// inputPausedSignal is the named pipe that a paused input invocation opens for writing when it starts.
	inputPausedSignal = "input-paused"
	// inputResumeSignal is the named pipe that a paused input invocation opens for reading before it continues.
	inputResumeSignal = "input-resume"
	// steerToolSSE requests one ordinary call of the reference extension tool.
	steerToolSSE = `data: {"type":"response.completed","response":{"id":"resp-steer-tool","model":"gpt-active",` +
		`"status":"completed","output":[` +
		`{"id":"fc-1","type":"function_call","call_id":"call-1","name":"external",` +
		`"arguments":"{\"mode\":\"ordinary\"}","status":"completed"}]}}` + "\n\n" + "data: [DONE]\n\n"
)

// steerProvider is a local Responses endpoint that records request bodies and holds its first requests.
type steerProvider struct {
	// server serves the scripted responses.
	server *httptest.Server
	// held contains one channel per held request, closed when that request arrives.
	held []chan struct{}
	// release contains one channel per held request; closing it lets that request return its response.
	release []chan struct{}
	// mutex protects bodies.
	mutex sync.Mutex
	// bodies contains each request body in arrival order.
	bodies []string
}

// startSteerProvider serves the responses in order; each of the first heldRequests requests waits until the test
// releases it.
func startSteerProvider(t *testing.T, heldRequests int, responses ...string) *steerProvider {
	t.Helper()
	provider := &steerProvider{
		server: nil, held: make([]chan struct{}, heldRequests), release: make([]chan struct{}, heldRequests),
		mutex: sync.Mutex{}, bodies: nil,
	}
	for index := range heldRequests {
		provider.held[index] = make(chan struct{})
		provider.release[index] = make(chan struct{})
	}
	provider.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		assert.NoError(t, err)
		provider.mutex.Lock()
		provider.bodies = append(provider.bodies, string(body))
		position := len(provider.bodies)
		provider.mutex.Unlock()
		if position > len(responses) {
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		if position <= heldRequests {
			close(provider.held[position-1])
			select {
			case <-provider.release[position-1]:
			case <-request.Context().Done():
				return
			}
		}
		writer.Header().Set(retryContentTypeHeader, retryStreamContentType)
		_, _ = writer.Write([]byte(responses[position-1]))
	}))
	t.Cleanup(provider.server.Close)
	return provider
}

// snapshot returns the recorded request bodies.
func (provider *steerProvider) snapshot() []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]string(nil), provider.bodies...)
}

// steerSettings selects a Responses-compatible provider at the given endpoint.
func steerSettings(serverURL string) string {
	compatible := strings.Replace(
		strings.SplitN(configuredRequestSettings, retryProvidersKey, 2)[1],
		retryCodexProviderKey,
		retryLocalProviderKey,
		1,
	)
	compatible = strings.Replace(
		compatible, retryCodexProviderType, fmt.Sprintf(retryCompatibleProviderFormat, serverURL), 1,
	)
	settings := strings.Replace(configuredRequestSettings, retryCodexDefault, retryLocalDefault, 1)
	return strings.Replace(settings, retryProvidersKey, retryProvidersKey+compatible, 1)
}

// startSteerFixture starts a Programmatic Control process against the provider.
func startSteerFixture(t *testing.T, provider *steerProvider, extensionDirectory string) *programmaticFixture {
	t.Helper()
	paths := testPaths(t, steerSettings(provider.server.URL))
	writeProgrammaticCredentials(t, paths)
	return startProgrammaticFixtureWithExtension(t, paths, extensionDirectory)
}

// steerRequest builds one operation frame that queues text as a steer message.
func steerRequest(operationID, text string) *programmaticv1.OpenRequest {
	return testProgrammaticRequest(operationID, func(request *programmaticv1.ControllerRequest) {
		payload := new(programmaticv1.EnqueueMessage)
		payload.SetContent([]*programmaticv1.UserContent{
			programmaticv1.UserContent_builder{Text: new(text), Image: nil}.Build(),
		})
		payload.SetDelivery(programmaticv1.MessageDelivery_MESSAGE_DELIVERY_STEER)
		request.SetEnqueueMessage(payload)
	})
}

// sendSteer sends one steer message and returns the terminal lifecycle event of its operation.
func sendSteer(t *testing.T, fixture *programmaticFixture, operationID, text string) *programmaticv1.HostEvent {
	t.Helper()
	require.NoError(t, fixture.stream.Send(steerRequest(operationID, text)))
	return waitForTerminalEvent(t, fixture, operationID)
}

// waitForTerminalEvent skips the events of other operations and returns the terminal event of one operation.
func waitForTerminalEvent(t *testing.T, fixture *programmaticFixture, operationID string) *programmaticv1.HostEvent {
	t.Helper()
	for {
		_, event := receiveOperationEvent(t, fixture, operationID)
		if event.HasCompleted() || event.HasFailed() || event.HasCanceled() || event.HasRejected() {
			return event
		}
	}
}

// requireQueued checks that Host queued the message instead of an extension handling it.
func requireQueued(t *testing.T, event *programmaticv1.HostEvent) {
	t.Helper()
	require.True(t, event.HasCompleted(), "steer operation did not complete: %v", event)
	require.True(t, event.GetCompleted().HasEnqueueMessage())
	require.False(t, event.GetCompleted().GetEnqueueMessage().GetHandled())
}

// TestPublicSteerAfterFinalResponse checks that a steer message sent during the final model response causes
// another model request with that message before the agent run completes.
func TestPublicSteerAfterFinalResponse(t *testing.T) {
	t.Parallel()
	// Arrange a provider that holds the final response of the first request.
	provider := startSteerProvider(t, 1, finalResponseSSE, finalResponseSSE)
	fixture := startSteerFixture(t, provider, t.TempDir())
	defer fixture.closeOwner(t)
	require.NoError(t, fixture.stream.Send(userRequest(steerRunID, "first request")))
	<-provider.held[0]

	// Act by queuing a steer message, then releasing the final response.
	queued := sendSteer(t, fixture, "steer-final", steerFirstText)
	close(provider.release[0])
	terminal := waitForTerminalEvent(t, fixture, steerRunID)

	// Assert the run made a second model request with the steer message and then completed.
	requireQueued(t, queued)
	require.True(t, terminal.HasCompleted(), "agent run did not complete: %v", terminal)
	bodies := provider.snapshot()
	require.Len(t, bodies, 2)
	require.NotContains(t, bodies[0], steerFirstText)
	require.Contains(t, bodies[1], steerFirstText)
}

// TestPublicSteerAfterToolBatch checks that a steer message sent while the model requests a tool call reaches
// the next model request together with the tool result.
func TestPublicSteerAfterToolBatch(t *testing.T) {
	t.Parallel()
	// Arrange a provider that holds a tool-call response of the reference extension tool.
	provider := startSteerProvider(t, 1, steerToolSSE, finalResponseSSE)
	fixture := startSteerFixture(t, provider, buildPublicExtensionFixture(t))
	defer fixture.closeOwner(t)
	require.NoError(t, fixture.stream.Send(userRequest(steerRunID, "first request")))
	<-provider.held[0]

	// Act by queuing a steer message, then releasing the tool-call response.
	queued := sendSteer(t, fixture, "steer-tool", steerFirstText)
	close(provider.release[0])
	terminal := waitForTerminalEvent(t, fixture, steerRunID)

	// Assert the next model request contains the tool result followed by the steer message.
	requireQueued(t, queued)
	require.True(t, terminal.HasCompleted(), "agent run did not complete: %v", terminal)
	bodies := provider.snapshot()
	require.Len(t, bodies, 2)
	require.NotContains(t, bodies[0], steerFirstText)
	resultIndex := strings.Index(bodies[1], steerToolOutput)
	steerIndex := strings.Index(bodies[1], steerFirstText)
	require.NotEqual(t, -1, resultIndex)
	require.NotEqual(t, -1, steerIndex)
	require.Less(t, resultIndex, steerIndex)
}

// TestPublicSteerOneAtATime checks that the default queue mode delivers one steer message per model request.
func TestPublicSteerOneAtATime(t *testing.T) {
	t.Parallel()
	// Arrange a provider that holds the final response of the first request.
	provider := startSteerProvider(t, 1, finalResponseSSE, finalResponseSSE, finalResponseSSE)
	fixture := startSteerFixture(t, provider, t.TempDir())
	defer fixture.closeOwner(t)
	require.NoError(t, fixture.stream.Send(userRequest(steerRunID, "first request")))
	<-provider.held[0]

	// Act by queuing two steer messages, then releasing the final response.
	first := sendSteer(t, fixture, "steer-first", steerFirstText)
	second := sendSteer(t, fixture, "steer-second", steerSecondText)
	close(provider.release[0])
	terminal := waitForTerminalEvent(t, fixture, steerRunID)

	// Assert each following model request adds one steer message in queue order.
	requireQueued(t, first)
	requireQueued(t, second)
	require.True(t, terminal.HasCompleted(), "agent run did not complete: %v", terminal)
	bodies := provider.snapshot()
	require.Len(t, bodies, 3)
	require.NotContains(t, bodies[0], steerFirstText)
	require.Contains(t, bodies[1], steerFirstText)
	require.NotContains(t, bodies[1], steerSecondText)
	require.Contains(t, bodies[2], steerSecondText)
}

// TestPublicSteerWithoutActiveRun checks that a steer message fails explicitly while no agent run is active.
func TestPublicSteerWithoutActiveRun(t *testing.T) {
	t.Parallel()
	// Arrange an idle Programmatic Control process.
	provider := startSteerProvider(t, 0)
	fixture := startSteerFixture(t, provider, t.TempDir())
	defer fixture.closeOwner(t)

	// Act by queuing a steer message.
	terminal := sendSteer(t, fixture, "steer-idle", steerFirstText)

	// Assert the operation failed with its category and complete text, and the model received nothing.
	require.True(t, terminal.HasFailed(), "steer operation did not fail: %v", terminal)
	failure := terminal.GetFailed().GetFailure()
	require.Equal(t, steerNotActiveCode, failure.GetCode())
	require.NotEmpty(t, failure.GetMessage())
	require.Empty(t, provider.snapshot())
}

// TestPublicSteerHandledWithoutActiveRun checks that a steer message fails before input processing while no agent
// run is active, also when a user-input handler would fully handle it.
//
//nolint:paralleltest // The scenario sets the trace environment of the extension process.
func TestPublicSteerHandledWithoutActiveRun(t *testing.T) {
	// Arrange an idle process with the input reference extension and its invocation trace.
	trace := enableInputTrace(t)
	provider := startSteerProvider(t, 0)
	fixture := startSteerFixture(t, provider, buildInputFixture(t, "input"))
	defer fixture.closeOwner(t)

	// Act by queuing input that the reference handler fully handles.
	terminal := sendSteer(t, fixture, "steer-handled-idle", inputHandledText)

	// Assert the operation failed without a handler invocation or a model request.
	require.True(t, terminal.HasFailed(), "steer operation did not fail: %v", terminal)
	require.Equal(t, steerNotActiveCode, terminal.GetFailed().GetFailure().GetCode())
	require.Empty(t, readInputTrace(t, trace))
	require.Empty(t, provider.snapshot())
}

// TestPublicSteerHandledDuringRun checks that a steer message that a user-input handler fully handles during an
// agent run completes as handled and reaches neither history nor the model.
func TestPublicSteerHandledDuringRun(t *testing.T) {
	t.Parallel()
	// Arrange a provider that holds the final response, with the input reference extension loaded.
	provider := startSteerProvider(t, 1, finalResponseSSE)
	fixture := startSteerFixture(t, provider, buildInputFixture(t, "input"))
	defer fixture.closeOwner(t)
	require.NoError(t, fixture.stream.Send(userRequest(steerRunID, "first request")))
	<-provider.held[0]

	// Act by queuing handled input, then releasing the final response.
	queued := sendSteer(t, fixture, "steer-handled", inputHandledText)
	close(provider.release[0])
	terminal := waitForTerminalEvent(t, fixture, steerRunID)

	// Assert the handled result, one model request, and a history without the handled input.
	require.True(t, queued.HasCompleted(), "steer operation did not complete: %v", queued)
	require.True(t, queued.GetCompleted().GetEnqueueMessage().GetHandled())
	require.True(t, terminal.HasCompleted(), "agent run did not complete: %v", terminal)
	require.Len(t, provider.snapshot(), 1)
	users := userMessages(t, fixture, "steer-handled-history")
	require.Len(t, users, 1)
	require.NotContains(t, users[0].String(), inputHandledText)
}

// openSignalPipe opens and closes one end of a named pipe; the open returns when the other end is open.
func openSignalPipe(t *testing.T, path string, flag int) {
	t.Helper()
	file, err := os.OpenFile(path, flag, 0)
	require.NoError(t, err)
	require.NoError(t, file.Close())
}

// TestPublicSteerFromEndedRunNotQueued checks that a steer message whose input processing starts during one agent
// run fails when that run has ended, and that the next agent run does not receive it.
func TestPublicSteerFromEndedRunNotQueued(t *testing.T) {
	// Arrange named pipes that pause the steer input handler, and a provider that holds the first request of each run.
	signals := t.TempDir()
	paused := filepath.Join(signals, inputPausedSignal)
	resume := filepath.Join(signals, inputResumeSignal)
	require.NoError(t, syscall.Mkfifo(paused, 0o600))
	require.NoError(t, syscall.Mkfifo(resume, 0o600))
	t.Setenv(inputSignalsEnvironment, signals)
	provider := startSteerProvider(t, 2, finalResponseSSE, finalResponseSSE, finalResponseSSE)
	fixture := startSteerFixture(t, provider, buildInputFixture(t, "input-pause"))
	defer fixture.closeOwner(t)
	require.NoError(t, fixture.stream.Send(userRequest("steer-run-a", "first request")))
	<-provider.held[0]

	// Act by pausing the steer input handler while the first run ends and the second run starts.
	require.NoError(t, fixture.stream.Send(steerRequest("steer-stale", steerPausedText)))
	openSignalPipe(t, paused, os.O_RDONLY)
	close(provider.release[0])
	first := waitForTerminalEvent(t, fixture, "steer-run-a")
	require.NoError(t, fixture.stream.Send(userRequest("steer-run-b", "second request")))
	<-provider.held[1]
	openSignalPipe(t, resume, os.O_WRONLY)
	queued := waitForTerminalEvent(t, fixture, "steer-stale")
	close(provider.release[1])
	second := waitForTerminalEvent(t, fixture, "steer-run-b")

	// Assert the steer operation failed, both runs completed, and neither the model nor history received it.
	require.True(t, first.HasCompleted(), "first agent run did not complete: %v", first)
	require.True(t, queued.HasFailed(), "steer operation did not fail: %v", queued)
	require.Equal(t, steerNotActiveCode, queued.GetFailed().GetFailure().GetCode())
	require.Contains(t, queued.GetFailed().GetFailure().GetMessage(), agent.ErrRunNotActive.Error())
	require.True(t, second.HasCompleted(), "second agent run did not complete: %v", second)
	bodies := provider.snapshot()
	require.Len(t, bodies, 2)
	for _, body := range bodies {
		require.NotContains(t, body, steerPausedMarker)
	}
	for _, user := range userMessages(t, fixture, "steer-stale-history") {
		require.NotContains(t, user.String(), steerPausedMarker)
	}
}
