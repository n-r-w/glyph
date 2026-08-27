package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	controllerprogrammatic "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/infra/persistence"
	programmaticsocket "github.com/n-r-w/glyph/host/internal/infra/programmatic/socket"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	hostprogrammatic "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestRunWithPathsRPCPublishesSocketAndCleansUp verifies the RPC process boundary on application cancellation.
func TestRunWithPathsRPCPublishesSocketAndCleansUp(t *testing.T) {
	t.Parallel()

	paths := testPaths(t, codexSettings(""))
	extensionDirectory := t.TempDir()
	socketDirectory, err := os.MkdirTemp("/tmp", "glyph-app-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(socketDirectory)) })
	socketPath := filepath.Join(socketDirectory, "glyph.sock")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	reader, writer := io.Pipe()
	lineResult := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(reader)
		if scanner.Scan() {
			lineResult <- scanner.Text()
			cancel()
			return
		}
		lineResult <- ""
	}()

	var stderr bytes.Buffer
	runErr := runWithPaths(ctx, paths, cli.Command{
		Mode:               cli.ModeRPC,
		ExtensionDirectory: extensionDirectory,
		SocketPath:         socketPath,
		Headless:           headless.Command{},
		UIDirectory:        "",
		UIID:               "",
	}, writer, &stderr)
	require.NoError(t, writer.Close())
	line := <-lineResult
	require.NoError(t, reader.Close())

	require.ErrorIs(t, runErr, context.Canceled)
	var announcement struct {
		Socket string `json:"socket"`
	}
	require.NoError(t, json.Unmarshal([]byte(line), &announcement))
	assert.Equal(t, socketPath, announcement.Socket)
	_, statErr := os.Lstat(socketPath)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

// ProgrammaticAppSuite exercises the owning process through its generated client.
type ProgrammaticAppSuite struct {
	suite.Suite
}

// TestProgrammaticAppSuite runs the real Unix-socket process contract.
//
//nolint:paralleltest // Suite cases temporarily replace the process-wide HTTP transport.
func TestProgrammaticAppSuite(t *testing.T) {
	suite.Run(t, new(ProgrammaticAppSuite))
}

// TestModelCommandsUseSharedCatalog verifies Programmatic Control application composition.
func (testSuite *ProgrammaticAppSuite) TestModelCommandsUseSharedCatalog() {
	t := testSuite.T()
	fixture := startProgrammaticFixture(t, testPaths(t, codexSettings("")))

	require.NoError(t, fixture.stream.Send(getModelsRequest("models")))
	modelsResponse, err := fixture.stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, "models", modelsResponse.GetCorrelationId())
	models := modelsResponse.GetCommandResponse().GetModels()
	require.Len(t, models.GetModels(), 1)
	assert.Equal(t, "openai-codex", models.GetModels()[0].GetProviderId())
	assert.Equal(t, "gpt-test", models.GetModels()[0].GetModelId())
	assert.Equal(t, []programmaticv1.ReasoningChoice{
		programmaticv1.ReasoningChoice_REASONING_CHOICE_OFF,
	}, models.GetModels()[0].GetReasoning().GetChoices())
	assert.Equal(t, programmaticv1.ReasoningChoice_REASONING_CHOICE_OFF, models.GetActiveSelection().GetReasoningChoice())

	require.NoError(t, fixture.stream.Send(selectModelRequest("model", "openai-codex", "gpt-test")))
	modelResponse, err := fixture.stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, "model", modelResponse.GetCorrelationId())
	modelSelection := modelResponse.GetCommandResponse().GetModelSelection().GetSelection()
	assert.Equal(t, "openai-codex", modelSelection.GetProviderId())
	assert.Equal(t, "gpt-test", modelSelection.GetModelId())

	require.NoError(t, fixture.stream.Send(selectReasoningRequest(
		"reasoning", programmaticv1.ReasoningChoice_REASONING_CHOICE_OFF,
	)))
	reasoningResponse, err := fixture.stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, "reasoning", reasoningResponse.GetCorrelationId())
	assert.Equal(t,
		programmaticv1.ReasoningChoice_REASONING_CHOICE_OFF,
		reasoningResponse.GetCommandResponse().GetModelSelection().GetSelection().GetReasoningChoice(),
	)

	fixture.closeOwner(t)
}

// TestSessionLifecycleRoundTrip verifies Programmatic restart restores full public and continuation content.
func (testSuite *ProgrammaticAppSuite) TestSessionLifecycleRoundTrip() {
	t := testSuite.T()

	// Arrange persistent paths, provider transport, extension tools, and a Programmatic stream.
	paths := testPaths(t, restartSelectionSettings())
	accessToken := semanticAccessToken(t, "account")
	credentials := fmt.Sprintf(
		`{"version":1,"providers":{"openai-codex":{"access_token":%q,"refresh_token":"refresh","account_id":"account","expires_at":"2099-01-01T00:00:00Z"}}}`,
		accessToken,
	)
	require.NoError(t, os.WriteFile(paths.CredentialsFile, []byte(credentials), 0o600))
	requestCount := &atomic.Int32{}
	requestCount.Store(0)
	lastBody := &atomic.Value{}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = deterministicCodexTransport{requestCount: requestCount, lastBody: lastBody}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	extensionDirectory := buildToolsExecutable(t)
	fixture := startProgrammaticFixtureWithExtension(t, paths, extensionDirectory)

	send := func(correlationID string, configure func(*programmaticv1.OpenRequest)) *programmaticv1.CommandResponse {
		request := new(programmaticv1.OpenRequest)
		request.SetCorrelationId(correlationID)
		configure(request)
		require.NoError(t, fixture.stream.Send(request))
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		assert.Equal(t, correlationID, response.GetCorrelationId())
		require.NotNil(t, response.GetCommandResponse())
		return response.GetCommandResponse()
	}

	// Act by creating, naming, selecting, persisting, restarting, and resuming one session.
	initial := send("initial", func(request *programmaticv1.OpenRequest) {
		request.SetGetSessionInfo(new(programmaticv1.GetSessionInfo))
	}).GetSessionInfo().GetInfo()
	// Assert session presence state before checking the complete restart sequence.
	require.NotEmpty(t, initial.GetId())
	assert.False(t, initial.HasName())
	assert.False(t, initial.HasStoragePath())

	named := send("name", func(request *programmaticv1.OpenRequest) {
		request.SetSetSessionName(programmaticv1.SetSessionName_builder{Name: new("named")}.Build())
	}).GetSessionInfo().GetInfo()
	assert.Equal(t, initial.GetId(), named.GetId())
	assert.Equal(t, "named", named.GetName())
	assert.True(t, named.HasStoragePath())

	listed := send("list", func(request *programmaticv1.OpenRequest) {
		request.SetListSessions(new(programmaticv1.ListSessions))
	}).GetSessions().GetSessions()
	require.Len(t, listed, 1)
	assert.Equal(t, initial.GetId(), listed[0].GetInfo().GetId())

	selectedModel := send("select-model", func(request *programmaticv1.OpenRequest) {
		request.SetSelectModel(programmaticv1.SelectModel_builder{
			ProviderId: new("openai-codex"), ModelId: new("selected-model"),
		}.Build())
	}).GetModelSelection().GetSelection()
	assert.Equal(t, "openai-codex", selectedModel.GetProviderId())
	assert.Equal(t, "selected-model", selectedModel.GetModelId())
	selectedReasoning := send("select-reasoning", func(request *programmaticv1.OpenRequest) {
		request.SetSelectReasoningChoice(programmaticv1.SelectReasoningChoice_builder{
			Choice: new(programmaticv1.ReasoningChoice_REASONING_CHOICE_HIGH),
		}.Build())
	}).GetModelSelection().GetSelection()
	assert.Equal(t, programmaticv1.ReasoningChoice_REASONING_CHOICE_HIGH, selectedReasoning.GetReasoningChoice())

	require.NoError(t, fixture.stream.Send(userRequest("first-turn", "restart text")))
	accepted, err := fixture.stream.Recv()
	require.NoError(t, err)
	assert.True(t, accepted.GetCommandResponse().HasUserRequestAccepted())
	waitProgrammaticSettled(t, fixture)
	require.Equal(t, int32(2), requestCount.Load())
	beforeRestartEntries := send("before-restart-entries", func(request *programmaticv1.OpenRequest) {
		request.SetGetSessionEntries(new(programmaticv1.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	require.Len(t, beforeRestartEntries, 4)
	named = send("after-turn-information", func(request *programmaticv1.OpenRequest) {
		request.SetGetSessionInfo(new(programmaticv1.GetSessionInfo))
	}).GetSessionInfo().GetInfo()
	fixture.closeOwner(t)
	appendFullContentFixture(t, paths, named.GetId())

	restarted := startProgrammaticFixtureWithExtension(t, paths, extensionDirectory)
	defer restarted.closeOwner(t)
	restartSend := func(correlationID string, configure func(*programmaticv1.OpenRequest)) *programmaticv1.CommandResponse {
		request := new(programmaticv1.OpenRequest)
		request.SetCorrelationId(correlationID)
		configure(request)
		require.NoError(t, restarted.stream.Send(request))
		response, err := restarted.stream.Recv()
		require.NoError(t, err)
		return response.GetCommandResponse()
	}
	restartedInfo := restartSend("restart-information", func(request *programmaticv1.OpenRequest) {
		request.SetGetSessionInfo(new(programmaticv1.GetSessionInfo))
	}).GetSessionInfo().GetInfo()
	require.NotEmpty(t, restartedInfo.GetId())
	assert.True(t, restartedInfo.HasId())
	assert.NotEqual(t, initial.GetId(), restartedInfo.GetId())
	assert.False(t, restartedInfo.HasName())
	assert.False(t, restartedInfo.HasStoragePath())
	assert.True(t, restartedInfo.HasWorkingDirectory())
	assert.Equal(t, named.GetWorkingDirectory(), restartedInfo.GetWorkingDirectory())
	assert.True(t, restartedInfo.HasCreatedTime())
	assert.True(t, restartedInfo.HasUpdateTime())
	restartedList := restartSend("restart-list", func(request *programmaticv1.OpenRequest) {
		request.SetListSessions(new(programmaticv1.ListSessions))
	}).GetSessions().GetSessions()
	require.Len(t, restartedList, 1)
	storedInfo := restartedList[0].GetInfo()
	assert.Equal(t, named.GetId(), storedInfo.GetId())
	assert.Equal(t, named.GetName(), storedInfo.GetName())
	assert.Equal(t, named.GetWorkingDirectory(), storedInfo.GetWorkingDirectory())
	assert.Equal(t, named.GetStoragePath(), storedInfo.GetStoragePath())
	assert.Equal(t, named.GetCreatedTime().AsTime(), storedInfo.GetCreatedTime().AsTime())
	assert.True(t, storedInfo.GetUpdateTime().AsTime().After(named.GetUpdateTime().AsTime()))
	assert.Equal(t, int64(7), restartedList[0].GetTotalMessages())

	restartedModel := restartSend("restart-select-model", func(request *programmaticv1.OpenRequest) {
		request.SetSelectModel(programmaticv1.SelectModel_builder{
			ProviderId: new("openai-codex"), ModelId: new("selected-model"),
		}.Build())
	}).GetModelSelection().GetSelection()
	assert.Equal(t, "openai-codex", restartedModel.GetProviderId())
	assert.Equal(t, "selected-model", restartedModel.GetModelId())
	restartedReasoning := restartSend("restart-select-reasoning", func(request *programmaticv1.OpenRequest) {
		request.SetSelectReasoningChoice(programmaticv1.SelectReasoningChoice_builder{
			Choice: new(programmaticv1.ReasoningChoice_REASONING_CHOICE_HIGH),
		}.Build())
	}).GetModelSelection().GetSelection()
	assert.Equal(t, programmaticv1.ReasoningChoice_REASONING_CHOICE_HIGH, restartedReasoning.GetReasoningChoice())

	restartedResume := restartSend("restart-resume", func(request *programmaticv1.OpenRequest) {
		request.SetResumeSession(programmaticv1.ResumeSession_builder{SessionId: new(named.GetId())}.Build())
	}).GetSessionInfo().GetInfo()
	assertProgrammaticSessionInfoEqual(t, storedInfo, restartedResume)
	restartedActive := restartSend("restart-resumed-information", func(request *programmaticv1.OpenRequest) {
		request.SetGetSessionInfo(new(programmaticv1.GetSessionInfo))
	}).GetSessionInfo().GetInfo()
	assert.Equal(t, named.GetId(), restartedActive.GetId())

	messages := restartSend("restart-messages", func(request *programmaticv1.OpenRequest) {
		request.SetGetMessages(new(programmaticv1.GetMessages))
	}).GetMessages().GetEntries()
	require.Len(t, messages, 7)
	assert.Equal(t, "restart text", messages[0].GetUser().GetContent()[0].GetText())
	assert.Equal(t, "resp-1", messages[1].GetModel().GetResponseId())
	require.Len(t, messages[1].GetModel().GetContent(), 2)
	assert.Equal(t, programmaticv1.ModelResponseItem_Reasoning_case, messages[1].GetModel().GetContent()[0].WhichContent())
	assert.Equal(t, "call-1", messages[1].GetModel().GetContent()[1].GetToolCall().GetCallId())
	assert.Equal(t, "call-1", messages[2].GetToolResult().GetCallId())
	assert.Contains(t, messages[2].GetToolResult().GetContents()[0].GetText(), "tool-ok")
	assert.Equal(t, "Request complete.", messages[3].GetModel().GetText())
	require.Len(t, messages[4].GetUser().GetContent(), 3)
	assert.Equal(t, "full user", messages[4].GetUser().GetContent()[0].GetText())
	assert.Equal(t, "image/png", messages[4].GetUser().GetContent()[1].GetImage().GetMediaType())
	assert.Equal(t, []byte{1, 2, 3, 4}, messages[4].GetUser().GetContent()[1].GetImage().GetData())
	fullModel := messages[5].GetModel()
	require.Len(t, fullModel.GetContent(), 3)
	assert.Equal(t, "full reasoning", fullModel.GetContent()[0].GetReasoning().GetText())
	assert.Equal(t, "full refusal", fullModel.GetContent()[1].GetRefusal().GetText())
	assert.Equal(t, "full-call", fullModel.GetContent()[2].GetToolCall().GetCallId())
	require.Len(t, fullModel.GetDiagnostics(), 1)
	assert.Equal(t, "full_notice", fullModel.GetDiagnostics()[0].GetCode())
	assert.Equal(t, "full diagnostic", fullModel.GetDiagnostics()[0].GetMessage())
	require.Len(t, messages[6].GetToolResult().GetContents(), 2)
	assert.Equal(t, []byte{9, 8, 7, 6}, messages[6].GetToolResult().GetContents()[1].GetImage().GetData())
	entries := restartSend("restart-entries", func(request *programmaticv1.OpenRequest) {
		request.SetGetSessionEntries(new(programmaticv1.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	require.Len(t, entries, 7)
	assert.NotEmpty(t, entries[0].GetId())
	assert.True(t, entries[0].HasCreatedTime())

	require.NoError(t, restarted.stream.Send(userRequest("continued-turn", "continue")))
	accepted, err = restarted.stream.Recv()
	require.NoError(t, err)
	assert.True(t, accepted.GetCommandResponse().HasUserRequestAccepted())
	waitProgrammaticSettled(t, restarted)
	body, ok := lastBody.Load().([]byte)
	require.True(t, ok)
	assert.Contains(t, string(body), "restart text")
	assert.Contains(t, string(body), "enc-restart")
	assert.Contains(t, string(body), "call-1")
	assert.Contains(t, string(body), "tool-ok")
	assert.Contains(t, string(body), "Request complete.")
	assert.Contains(t, string(body), "full user")
	assert.Contains(t, string(body), fullContentUserImageBase64)
	assert.Contains(t, string(body), "enc-full")
	assert.Contains(t, string(body), "full refusal")
	assert.Contains(t, string(body), "full-call")
	assert.Contains(t, string(body), "full tool output")
	assert.Contains(t, string(body), fullContentToolImageBase64)
	assert.NotContains(t, string(body), "full-extension")
	assert.Contains(t, string(body), "continue")
	assert.Contains(t, string(body), `"model":"selected-model"`)
	assert.Contains(t, string(body), `"effort":"high"`)
	assert.Less(t, bytes.Index(body, []byte("restart text")), bytes.Index(body, []byte(`"type":"function_call"`)))
	assert.Less(t, bytes.Index(body, []byte(`"type":"function_call"`)), bytes.Index(body, []byte(`"type":"function_call_output"`)))
	assert.Less(t, bytes.Index(body, []byte(`"type":"function_call_output"`)), bytes.Index(body, []byte("Request complete.")))
	assert.Less(t, bytes.Index(body, []byte("Request complete.")), bytes.Index(body, []byte("continue")))
	assert.Equal(t, int32(3), requestCount.Load())
}

func waitProgrammaticSettled(t *testing.T, fixture *programmaticFixture) {
	t.Helper()
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetAgentEvent().GetType() == programmaticv1.AgentEventType_AGENT_EVENT_TYPE_AGENT_SETTLED {
			return
		}
	}
}

func assertProgrammaticSessionInfoEqual(
	t *testing.T,
	expected *programmaticv1.SessionInfo,
	actual *programmaticv1.SessionInfo,
) {
	t.Helper()
	require.NotNil(t, actual)
	assert.Equal(t, expected.GetId(), actual.GetId())
	assert.Equal(t, expected.HasId(), actual.HasId())
	assert.Equal(t, expected.GetName(), actual.GetName())
	assert.Equal(t, expected.HasName(), actual.HasName())
	assert.Equal(t, expected.GetWorkingDirectory(), actual.GetWorkingDirectory())
	assert.Equal(t, expected.HasWorkingDirectory(), actual.HasWorkingDirectory())
	assert.Equal(t, expected.GetStoragePath(), actual.GetStoragePath())
	assert.Equal(t, expected.HasStoragePath(), actual.HasStoragePath())
	assert.Equal(t, expected.GetCreatedTime().AsTime(), actual.GetCreatedTime().AsTime())
	assert.Equal(t, expected.HasCreatedTime(), actual.HasCreatedTime())
	assert.Equal(t, expected.GetUpdateTime().AsTime(), actual.GetUpdateTime().AsTime())
	assert.Equal(t, expected.HasUpdateTime(), actual.HasUpdateTime())
}

// TestOwnerCanAbortAndStartAnotherRun verifies multi-operation ownership without a process restart.
func (testSuite *ProgrammaticAppSuite) TestOwnerCanAbortAndStartAnotherRun() {
	t := testSuite.T()
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	requestCount := new(atomic.Int32)
	providerStarted := make(chan struct{}, 1)
	previousTransport := http.DefaultTransport
	http.DefaultTransport = programmaticTransport{
		requestCount: requestCount,
		started:      providerStarted,
	}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	fixture := startProgrammaticFixtureWithExtension(t, paths, buildToolsExecutable(t))
	stream := fixture.stream
	require.NoError(t, stream.Send(userRequest("c1", "first request")))
	accepted, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, "c1", accepted.GetCorrelationId())
	require.Equal(t, programmaticv1.OpenResponse_CommandResponse_case, accepted.WhichContent())
	assert.Equal(t, programmaticv1.CommandResponse_UserRequestAccepted_case, accepted.GetCommandResponse().WhichResult())

	firstEvent, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, "c1", firstEvent.GetCorrelationId())
	require.Equal(t, programmaticv1.OpenResponse_AgentEvent_case, firstEvent.WhichContent())
	// Provider transport entry proves that abort cancels an active provider request.
	<-providerStarted

	require.NoError(t, stream.Send(abortRequest("abort-c1")))
	var settled, aborted bool
	for !settled || !aborted {
		response, receiveErr := stream.Recv()
		if receiveErr != nil {
			runErr := <-fixture.result
			require.NoError(t, receiveErr, "application error: %v", runErr)
		}
		switch response.WhichContent() {
		case programmaticv1.OpenResponse_Content_not_set_case:
			require.FailNow(t, "received response without content")
		case programmaticv1.OpenResponse_AgentEvent_case:
			if response.GetCorrelationId() == "c1" && response.GetAgentEvent().GetType() == programmaticv1.AgentEventType_AGENT_EVENT_TYPE_AGENT_SETTLED {
				settled = true
			}
		case programmaticv1.OpenResponse_CommandResponse_case:
			if response.GetCorrelationId() == "abort-c1" {
				assert.Equal(t, programmaticv1.CommandResponse_AbortCompleted_case, response.GetCommandResponse().WhichResult())
				aborted = true
			}
		}
	}

	require.NoError(t, stream.Send(runStateRequest("state-after-abort")))
	stateResponse, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, "state-after-abort", stateResponse.GetCorrelationId())
	assert.Equal(t, programmaticv1.RunState_RUN_STATE_IDLE, stateResponse.GetCommandResponse().GetRunState().GetState())

	require.NoError(t, stream.Send(userRequest("c2", "second request")))
	secondAccepted, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, "c2", secondAccepted.GetCorrelationId())
	assert.Equal(t, programmaticv1.CommandResponse_UserRequestAccepted_case, secondAccepted.GetCommandResponse().WhichResult())
	for {
		response, receiveErr := stream.Recv()
		require.NoError(t, receiveErr)
		if response.GetCorrelationId() == "c2" && response.WhichContent() == programmaticv1.OpenResponse_AgentEvent_case && response.GetAgentEvent().GetType() == programmaticv1.AgentEventType_AGENT_EVENT_TYPE_AGENT_SETTLED {
			break
		}
	}

	fixture.closeOwner(t)
	assert.Equal(t, int32(2), requestCount.Load())
}

// TestOwnerClosureCancelsActiveRun verifies clean disconnect cancellation and joining.
func (testSuite *ProgrammaticAppSuite) TestOwnerClosureCancelsActiveRun() {
	t := testSuite.T()
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	requestCount := new(atomic.Int32)
	providerStarted := make(chan struct{}, 1)
	previousTransport := http.DefaultTransport
	http.DefaultTransport = programmaticTransport{
		requestCount: requestCount,
		started:      providerStarted,
	}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	fixture := startProgrammaticFixture(t, paths)
	require.NoError(t, fixture.stream.Send(userRequest("c1", "disconnect this request")))
	accepted, err := fixture.stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, programmaticv1.CommandResponse_UserRequestAccepted_case, accepted.GetCommandResponse().WhichResult())
	_, err = fixture.stream.Recv()
	require.NoError(t, err)
	<-providerStarted

	fixture.closeOwner(t)
	assert.Equal(t, int32(1), requestCount.Load())
}

// TestApplicationCancellationWinsOverStreamShutdown verifies cancellation precedence while work is active.
func (testSuite *ProgrammaticAppSuite) TestApplicationCancellationWinsOverStreamShutdown() {
	t := testSuite.T()
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	requestCount := new(atomic.Int32)
	providerStarted := make(chan struct{}, 1)
	previousTransport := http.DefaultTransport
	http.DefaultTransport = programmaticTransport{
		requestCount: requestCount,
		started:      providerStarted,
	}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	fixture := startProgrammaticFixture(t, paths)
	require.NoError(t, fixture.stream.Send(userRequest("c1", "cancel this request")))
	accepted, err := fixture.stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, programmaticv1.CommandResponse_UserRequestAccepted_case, accepted.GetCommandResponse().WhichResult())
	_, err = fixture.stream.Recv()
	require.NoError(t, err)
	<-providerStarted

	fixture.cancel()
	_ = fixture.stream.CloseSend()
	runErr := <-fixture.result
	require.ErrorIs(t, runErr, context.Canceled)
	fixture.assertClosed(t)
	assert.Equal(t, int32(1), requestCount.Load())
}

// TestProtocolFailureReturnsNonzero verifies that invalid input terminates the owning process as an error.
func (testSuite *ProgrammaticAppSuite) TestProtocolFailureReturnsNonzero() {
	t := testSuite.T()

	// Arrange a Programmatic stream and a request without correlation identity.
	paths := testPaths(t, codexSettings(""))
	fixture := startProgrammaticFixture(t, paths)
	//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active UserRequest field.
	invalid := programmaticv1.OpenRequest_builder{
		UserRequest: programmaticv1.UserRequest_builder{
			Text: new("missing correlation"),
		}.Build(),
		CreateSession:  nil,
		ListSessions:   nil,
		ResumeSession:  nil,
		SetSessionName: nil,
		GetSessionInfo: nil,
	}.Build()
	// Act by sending the invalid protocol request and receiving stream termination.
	require.NoError(t, fixture.stream.Send(invalid))
	_, receiveErr := fixture.stream.Recv()
	require.Error(t, receiveErr)
	runErr := <-fixture.result
	// Assert the application exits nonzero with the safe protocol error.
	require.Error(t, runErr)
	require.ErrorContains(t, runErr, "correlation ID is required")
	fixture.assertClosed(t)
}

// TestServeFailureReturnsNonzero verifies that an independent server failure changes the process result.
func (testSuite *ProgrammaticAppSuite) TestServeFailureReturnsNonzero() {
	t := testSuite.T()
	delivery := hostprogrammatic.NewDelivery()
	coordinator := hostprogrammatic.NewMockCoordinator(gomock.NewController(t))
	session := hostprogrammatic.New(
		coordinator, nil,
		func() agentrun.State {
			return agentrun.State{
				Status:          agentrun.StatusIdle,
				RunID:           mo.None[string](),
				PartialResponse: mo.None[model.Response](),
				ToolPreviews:    nil,
			}
		},
		func() []agent.HistoryEntry { return nil },
		nil, delivery,
	)
	controller := controllerprogrammatic.New(t.Context(), session)
	server := grpc.NewServer(grpc.WaitForHandlers(true))
	socketService, err := programmaticsocket.New(t.Context(), "")
	require.NoError(t, err)
	require.NoError(t, socketService.Listener.Close())

	runErr := runProgrammaticServer(t.Context(), server, socketService, controller.Completions(), session)
	require.Error(t, runErr)
	require.ErrorContains(t, runErr, "serve Programmatic Control")
	require.NoError(t, socketService.Close())
}

// TestTransportCompletionReturnsNonzero verifies app handling of an independent send failure.
func (testSuite *ProgrammaticAppSuite) TestTransportCompletionReturnsNonzero() {
	t := testSuite.T()
	delivery := hostprogrammatic.NewDelivery()
	coordinator := hostprogrammatic.NewMockCoordinator(gomock.NewController(t))
	session := hostprogrammatic.New(
		coordinator, nil,
		func() agentrun.State {
			return agentrun.State{
				Status:          agentrun.StatusIdle,
				RunID:           mo.None[string](),
				PartialResponse: mo.None[model.Response](),
				ToolPreviews:    nil,
			}
		},
		func() []agent.HistoryEntry { return nil },
		nil, delivery,
	)
	server := grpc.NewServer(grpc.WaitForHandlers(true))
	socketService, err := programmaticsocket.New(t.Context(), "")
	require.NoError(t, err)
	sendErr := status.Error(codes.ResourceExhausted, "send failed")
	completions := make(chan controllerprogrammatic.SessionCompletion, 1)
	results := make(chan error, 1)
	go func() {
		results <- runProgrammaticServer(t.Context(), server, socketService, completions, session)
	}()
	connection, err := grpc.NewClient(
		"unix://"+socketService.Path(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	connection.Connect()
	for state := connection.GetState(); state != connectivity.Ready; state = connection.GetState() {
		require.True(t, connection.WaitForStateChange(t.Context(), state))
	}
	completions <- controllerprogrammatic.SessionCompletion{
		Cause:      controllerprogrammatic.SessionCompletionTransportFailure,
		Err:        sendErr,
		CleanupErr: nil,
	}

	runErr := <-results
	require.Error(t, runErr)
	require.ErrorIs(t, runErr, sendErr)
	require.Same(t, sendErr, runErr)
	assert.Equal(t, codes.ResourceExhausted, status.Code(runErr))
	require.NoError(t, connection.Close())
	require.NoError(t, socketService.Close())
}

// TestSocketCleanupFailureReturnsNonzero verifies that cleanup errors change the process result.
func (testSuite *ProgrammaticAppSuite) TestSocketCleanupFailureReturnsNonzero() {
	t := testSuite.T()
	paths := testPaths(t, codexSettings(""))
	fixture := startProgrammaticFixture(t, paths)
	require.NoError(t, os.Remove(fixture.socketPath))
	require.NoError(t, os.Mkdir(fixture.socketPath, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(fixture.socketPath, "keep"), []byte("keep"), 0o600))

	require.NoError(t, fixture.stream.CloseSend())
	runErr := <-fixture.result
	require.Error(t, runErr)
	require.ErrorContains(t, runErr, "directory not empty")
	fixture.assertStdout(t)
	require.NoError(t, fixture.connection.Close())
	fixture.cancel()
}

// TestAutomaticSocketDirectoryIsRemoved verifies process-owned directory cleanup.
func (testSuite *ProgrammaticAppSuite) TestAutomaticSocketDirectoryIsRemoved() {
	t := testSuite.T()
	paths := testPaths(t, codexSettings(""))
	fixture := startProgrammaticFixtureAtPath(t, paths, t.TempDir(), "")
	directory := filepath.Dir(fixture.socketPath)

	fixture.closeOwner(t)
	_, err := os.Stat(directory)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// programmaticFixture owns one generated client connection and its RPC process.
type programmaticFixture struct {
	cancel       context.CancelFunc
	connection   *grpc.ClientConn
	stream       grpc.BidiStreamingClient[programmaticv1.OpenRequest, programmaticv1.OpenResponse]
	result       <-chan error
	stdoutReader *bufio.Reader
	socketPath   string
}

// startProgrammaticFixture starts an RPC process without extension executables.
func startProgrammaticFixture(t *testing.T, paths persistence.Paths) *programmaticFixture {
	t.Helper()
	return startProgrammaticFixtureWithExtension(t, paths, t.TempDir())
}

// startProgrammaticFixtureWithExtension starts an RPC process and reads its socket announcement.
func startProgrammaticFixtureWithExtension(
	t *testing.T,
	paths persistence.Paths,
	extensionDirectory string,
) *programmaticFixture {
	t.Helper()
	socketDirectory, err := os.MkdirTemp("/tmp", "glyph-rpc-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(socketDirectory)) })
	return startProgrammaticFixtureAtPath(
		t, paths, extensionDirectory, filepath.Join(socketDirectory, "control.sock"),
	)
}

// startProgrammaticFixtureAtPath starts an RPC process at an explicit or automatic socket path.
func startProgrammaticFixtureAtPath(
	t *testing.T,
	paths persistence.Paths,
	extensionDirectory string,
	socketPath string,
) *programmaticFixture {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	reader, writer := io.Pipe()
	results := make(chan error, 1)
	unusedUIDirectory := filepath.Join(t.TempDir(), "must-not-load")
	go func() {
		results <- runWithPaths(ctx, paths, cli.Command{
			Mode:               cli.ModeRPC,
			ExtensionDirectory: extensionDirectory,
			UIDirectory:        unusedUIDirectory,
			UIID:               "must-not-load",
			SocketPath:         socketPath,
			Headless:           headless.Command{},
		}, writer, io.Discard)
		_ = writer.Close()
	}()

	stdoutReader := bufio.NewReader(reader)
	line, err := stdoutReader.ReadString('\n')
	require.NoError(t, err)
	var announcement struct {
		Socket string `json:"socket"`
	}
	require.NoError(t, json.Unmarshal([]byte(line), &announcement))
	if socketPath != "" {
		assert.Equal(t, socketPath, announcement.Socket)
	}
	connection, err := grpc.NewClient(
		"unix://"+announcement.Socket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	client := programmaticv1.NewProgrammaticControlServiceClient(connection)
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	return &programmaticFixture{
		cancel:       cancel,
		connection:   connection,
		stream:       stream,
		result:       results,
		stdoutReader: stdoutReader,
		socketPath:   announcement.Socket,
	}
}

// closeOwner closes the client send side and requires a clean process result.
func (fixture *programmaticFixture) closeOwner(t *testing.T) {
	t.Helper()
	require.NoError(t, fixture.stream.CloseSend())
	require.NoError(t, <-fixture.result)
	fixture.assertClosed(t)
}

// assertClosed verifies transport, stdout, and socket cleanup.
func (fixture *programmaticFixture) assertClosed(t *testing.T) {
	t.Helper()
	require.NoError(t, fixture.connection.Close())
	fixture.assertStdout(t)
	_, err := os.Lstat(fixture.socketPath)
	require.ErrorIs(t, err, os.ErrNotExist)
	fixture.cancel()
}

// assertStdout verifies that the announcement was the only stdout content.
func (fixture *programmaticFixture) assertStdout(t *testing.T) {
	t.Helper()
	remaining, err := io.ReadAll(fixture.stdoutReader)
	require.NoError(t, err)
	assert.Empty(t, remaining)
}

// programmaticTransport blocks the first run and completes the second run.
type programmaticTransport struct {
	requestCount *atomic.Int32
	started      chan<- struct{}
}

// RoundTrip returns deterministic provider behavior without network access.
func (transport programmaticTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	switch transport.requestCount.Add(1) {
	case 1:
		if transport.started != nil {
			// The signal proves that provider transport owns the active request before cancellation.
			transport.started <- struct{}{}
		}
		<-request.Context().Done()
		return nil, request.Context().Err()
	case 2:
		return &http.Response{
			StatusCode:       http.StatusOK,
			Body:             io.NopCloser(bytes.NewBufferString(finalResponseSSE)),
			Header:           make(http.Header),
			Status:           "",
			Proto:            "",
			ProtoMajor:       0,
			ProtoMinor:       0,
			ContentLength:    0,
			TransferEncoding: nil,
			Close:            false,
			Uncompressed:     false,
			Trailer:          nil,
			Request:          nil,
			TLS:              nil,
		}, nil
	default:
		return nil, fmt.Errorf("unexpected programmatic provider request")
	}
}

// writeProgrammaticCredentials stores credentials accepted by the deterministic provider.
func writeProgrammaticCredentials(t *testing.T, paths persistence.Paths) {
	t.Helper()
	accessToken := semanticAccessToken(t, "account")
	payload := fmt.Sprintf(`{"version":1,"providers":{"openai-codex":{"access_token":%q,"refresh_token":"refresh","account_id":"account","expires_at":"2099-01-01T00:00:00Z"}}}`, accessToken)
	require.NoError(t, os.WriteFile(paths.CredentialsFile, []byte(payload), 0o600))
}

// userRequest builds a generated user-request frame.
func userRequest(correlationID, text string) *programmaticv1.OpenRequest {
	//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active UserRequest field.
	return programmaticv1.OpenRequest_builder{
		CorrelationId: new(correlationID),
		UserRequest: programmaticv1.UserRequest_builder{
			Text: new(text),
		}.Build(),
		CreateSession:  nil,
		ListSessions:   nil,
		ResumeSession:  nil,
		SetSessionName: nil,
		GetSessionInfo: nil,
	}.Build()
}

// abortRequest builds a generated abort frame.
func abortRequest(correlationID string) *programmaticv1.OpenRequest {
	//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active Abort field.
	return programmaticv1.OpenRequest_builder{
		CorrelationId:  new(correlationID),
		Abort:          programmaticv1.Abort_builder{}.Build(),
		CreateSession:  nil,
		ListSessions:   nil,
		ResumeSession:  nil,
		SetSessionName: nil,
		GetSessionInfo: nil,
	}.Build()
}

// runStateRequest builds a generated run-state frame.
func runStateRequest(correlationID string) *programmaticv1.OpenRequest {
	//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active GetRunState field.
	return programmaticv1.OpenRequest_builder{
		CorrelationId:  new(correlationID),
		GetRunState:    programmaticv1.GetRunState_builder{}.Build(),
		CreateSession:  nil,
		ListSessions:   nil,
		ResumeSession:  nil,
		SetSessionName: nil,
		GetSessionInfo: nil,
	}.Build()
}

// getModelsRequest builds a generated model-catalog frame.
func getModelsRequest(correlationID string) *programmaticv1.OpenRequest {
	//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active GetModels field.
	return programmaticv1.OpenRequest_builder{
		CorrelationId:  new(correlationID),
		GetModels:      programmaticv1.GetModels_builder{}.Build(),
		CreateSession:  nil,
		ListSessions:   nil,
		ResumeSession:  nil,
		SetSessionName: nil,
		GetSessionInfo: nil,
	}.Build()
}

// selectModelRequest builds a generated model-selection frame.
func selectModelRequest(correlationID, providerID, modelID string) *programmaticv1.OpenRequest {
	//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active SelectModel field.
	return programmaticv1.OpenRequest_builder{
		CorrelationId: new(correlationID),
		SelectModel: programmaticv1.SelectModel_builder{
			ProviderId: new(providerID),
			ModelId:    new(modelID),
		}.Build(),
		CreateSession:  nil,
		ListSessions:   nil,
		ResumeSession:  nil,
		SetSessionName: nil,
		GetSessionInfo: nil,
	}.Build()
}

// selectReasoningRequest builds a generated reasoning-selection frame.
func selectReasoningRequest(
	correlationID string,
	level programmaticv1.ReasoningChoice,
) *programmaticv1.OpenRequest {
	//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active SelectReasoningChoice field.
	return programmaticv1.OpenRequest_builder{
		CorrelationId: new(correlationID),
		SelectReasoningChoice: programmaticv1.SelectReasoningChoice_builder{
			Choice: level.Enum(),
		}.Build(),
		CreateSession:  nil,
		ListSessions:   nil,
		ResumeSession:  nil,
		SetSessionName: nil,
		GetSessionInfo: nil,
	}.Build()
}
