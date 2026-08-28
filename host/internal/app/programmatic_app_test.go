package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	"github.com/n-r-w/glyph/host/internal/domain/session"
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
	// Arrange a programmatic fixture backed by the configured model catalog.
	t := testSuite.T()
	fixture := startProgrammaticFixture(t, testPaths(t, codexSettings("")))

	// Act by querying models and selecting the model and reasoning choice.
	require.NoError(t, fixture.stream.Send(getModelsRequest("models")))
	modelsResponse, err := fixture.stream.Recv()

	// Assert every response uses the same catalog and confirmed selection.
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
	emptyStats := send("initial-stats", func(request *programmaticv1.OpenRequest) {
		request.SetGetSessionStats(new(programmaticv1.GetSessionStats))
	}).GetSessionStats().GetStatistics()
	assert.Zero(t, emptyStats.GetTotalMessages())
	assert.True(t, emptyStats.HasTokens())
	assert.Zero(t, emptyStats.GetTokens().GetTotalTokens())
	assert.True(t, emptyStats.HasEstimatedCost())
	assert.Zero(t, emptyStats.GetEstimatedCost().GetTotal())
	assert.Empty(t, emptyStats.GetCostBreakdown())

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
	availableEntryCosts := 0
	for _, entry := range beforeRestartEntries {
		if entry.HasEstimatedCost() {
			availableEntryCosts++
			assert.Zero(t, entry.GetEstimatedCost().GetTotal())
		}
	}
	assert.Zero(t, availableEntryCosts)
	beforeRestartStats := send("before-restart-stats", func(request *programmaticv1.OpenRequest) {
		request.SetGetSessionStats(new(programmaticv1.GetSessionStats))
	}).GetSessionStats().GetStatistics()
	assert.Equal(t, int64(4), beforeRestartStats.GetTotalMessages())
	assert.False(t, beforeRestartStats.HasTokens())
	assert.False(t, beforeRestartStats.HasEstimatedCost())
	require.Len(t, beforeRestartStats.GetCostBreakdown(), 1)
	assert.False(t, beforeRestartStats.GetCostBreakdown()[0].HasEstimatedCost())
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
	restartedEmptyStats := restartSend("restart-empty-stats", func(request *programmaticv1.OpenRequest) {
		request.SetGetSessionStats(new(programmaticv1.GetSessionStats))
	}).GetSessionStats().GetStatistics()
	assert.Zero(t, restartedEmptyStats.GetTotalMessages())
	assert.True(t, restartedEmptyStats.HasTokens())
	assert.Zero(t, restartedEmptyStats.GetTokens().GetTotalTokens())
	assert.True(t, restartedEmptyStats.HasEstimatedCost())
	assert.Zero(t, restartedEmptyStats.GetEstimatedCost().GetTotal())
	assert.Empty(t, restartedEmptyStats.GetCostBreakdown())
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
	restartedStats := restartSend("restart-resumed-stats", func(request *programmaticv1.OpenRequest) {
		request.SetGetSessionStats(new(programmaticv1.GetSessionStats))
	}).GetSessionStats().GetStatistics()
	assert.Equal(t, int64(7), restartedStats.GetTotalMessages())
	assert.False(t, restartedStats.HasTokens())
	assert.False(t, restartedStats.HasEstimatedCost())
	require.Len(t, restartedStats.GetCostBreakdown(), 1)
	assert.False(t, restartedStats.GetCostBreakdown()[0].HasEstimatedCost())

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

// TestRuntimePersistenceFailureProcessPaths verifies Programmatic mutation blocking, safe text, and create recovery.
func (testSuite *ProgrammaticAppSuite) TestRuntimePersistenceFailureProcessPaths() {
	t := testSuite.T()

	// Arrange one real Programmatic composition with valid credentials and a provider transport that must remain unused.
	paths := testPaths(t, restartSelectionSettings())
	writeProgrammaticCredentials(t, paths)
	requestCount := &atomic.Int32{}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = countingFailureTransport{requests: requestCount}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	fixture := startProgrammaticFixture(t, paths)
	defer fixture.closeOwner(t)
	send := func(correlationID string, configure func(*programmaticv1.OpenRequest)) *programmaticv1.CommandResponse {
		request := new(programmaticv1.OpenRequest)
		request.SetCorrelationId(correlationID)
		configure(request)
		require.NoError(t, fixture.stream.Send(request))
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		return response.GetCommandResponse()
	}

	initial := send("name-durable", func(request *programmaticv1.OpenRequest) {
		request.SetSetSessionName(programmaticv1.SetSessionName_builder{Name: new("durable name")}.Build())
	}).GetSessionInfo().GetInfo()
	require.True(t, initial.HasStoragePath())
	storagePath := initial.GetStoragePath()
	projectDirectory := filepath.Dir(storagePath)
	require.NoError(t, os.Chmod(storagePath, 0o400))
	t.Cleanup(func() {
		_ = os.Chmod(projectDirectory, 0o700)
		_ = os.Chmod(storagePath, 0o600)
	})

	// Act by failing naming, restoring OS permissions, and attempting the mutation again.
	rejected := send("name-failed", func(request *programmaticv1.OpenRequest) {
		request.SetSetSessionName(programmaticv1.SetSessionName_builder{Name: new("secret replacement")}.Build())
	}).GetRejected()
	require.NoError(t, os.Chmod(storagePath, 0o600))
	rejectedAgain := send("name-blocked", func(request *programmaticv1.OpenRequest) {
		request.SetSetSessionName(programmaticv1.SetSessionName_builder{Name: new("must not persist")}.Build())
	}).GetRejected()
	info := send("read-info", func(request *programmaticv1.OpenRequest) {
		request.SetGetSessionInfo(new(programmaticv1.GetSessionInfo))
	}).GetSessionInfo().GetInfo()
	entries := send("read-entries", func(request *programmaticv1.OpenRequest) {
		request.SetGetSessionEntries(new(programmaticv1.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	statistics := send("read-statistics", func(request *programmaticv1.OpenRequest) {
		request.SetGetSessionStats(new(programmaticv1.GetSessionStats))
	}).GetSessionStats().GetStatistics()

	// Assert both mutations retain persistence classification while durable queries retain the prior snapshot.
	for _, result := range []*programmaticv1.CommandRejected{rejected, rejectedAgain} {
		require.NotNil(t, result)
		assert.Equal(t, programmaticv1.RejectionCode_REJECTION_CODE_PERSISTENCE_UNAVAILABLE, result.GetCode())
		assert.Contains(t, result.GetMessage(), "session persistence failed")
	}
	assert.Contains(t, strings.ToLower(rejected.GetMessage()), "permission")
	assert.Equal(t, initial.GetId(), info.GetId())
	assert.Equal(t, "durable name", info.GetName())
	require.Empty(t, entries)
	assert.Zero(t, statistics.GetTotalMessages())

	created := send("create-recovery", func(request *programmaticv1.OpenRequest) {
		request.SetCreateSession(new(programmaticv1.CreateSession))
	}).GetSessionInfo().GetInfo()
	assert.NotEqual(t, initial.GetId(), created.GetId())
	require.NoError(t, os.Chmod(projectDirectory, 0o500))

	// Act by failing the first user append in the recovered session and observing terminal settlement.
	require.NoError(t, fixture.stream.Send(userRequest("first-user-failed", "private user content")))
	accepted, err := fixture.stream.Recv()
	require.NoError(t, err)
	require.True(t, accepted.GetCommandResponse().HasUserRequestAccepted())
	var terminalText string
	var eventTypes []programmaticv1.AgentEventType
	for {
		response, receiveErr := fixture.stream.Recv()
		require.NoError(t, receiveErr)
		event := response.GetAgentEvent()
		eventTypes = append(eventTypes, event.GetType())
		if event.GetType() == programmaticv1.AgentEventType_AGENT_EVENT_TYPE_AGENT_END {
			terminalText = event.GetAgent().GetErrorMessage()
		}
		if event.GetType() == programmaticv1.AgentEventType_AGENT_EVENT_TYPE_AGENT_SETTLED {
			break
		}
	}
	require.NoError(t, os.Chmod(projectDirectory, 0o700))

	// Assert no provider or dependent turn value escaped and create restores client mutation access.
	assert.Equal(t, []programmaticv1.AgentEventType{
		programmaticv1.AgentEventType_AGENT_EVENT_TYPE_AGENT_START,
		programmaticv1.AgentEventType_AGENT_EVENT_TYPE_AGENT_END,
		programmaticv1.AgentEventType_AGENT_EVENT_TYPE_AGENT_SETTLED,
	}, eventTypes)
	assert.Contains(t, terminalText, "session persistence failed")
	assert.Contains(t, strings.ToLower(terminalText), "permission")
	assert.Zero(t, requestCount.Load())
	recovered := send("create-after-run-failure", func(request *programmaticv1.OpenRequest) {
		request.SetCreateSession(new(programmaticv1.CreateSession))
	}).GetSessionInfo().GetInfo()
	require.NotEqual(t, created.GetId(), recovered.GetId())
	renamed := send("name-after-recovery", func(request *programmaticv1.OpenRequest) {
		request.SetSetSessionName(programmaticv1.SetSessionName_builder{Name: new("writable again")}.Build())
	}).GetSessionInfo().GetInfo()
	assert.Equal(t, "writable again", renamed.GetName())
}

// TestTerminalModelPersistenceFailureProcessPath verifies no terminal model value escapes before durability.
func (testSuite *ProgrammaticAppSuite) TestTerminalModelPersistenceFailureProcessPath() {
	t := testSuite.T()

	// Arrange a blocked provider response after a durable user append to a named real session file.
	paths := testPaths(t, restartSelectionSettings())
	writeProgrammaticCredentials(t, paths)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	requests := &atomic.Int32{}
	body := &atomic.Value{}
	body.Store(finalResponseSSE)
	previousTransport := http.DefaultTransport
	http.DefaultTransport = runtimeFailureTransport{body: body, requests: requests, started: started, release: release}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	fixture := startProgrammaticFixture(t, paths)
	defer fixture.closeOwner(t)
	named := sendProgrammaticCommand(t, fixture, "name-model-failure", func(request *programmaticv1.OpenRequest) {
		request.SetSetSessionName(programmaticv1.SetSessionName_builder{Name: new("model failure")}.Build())
	}).GetSessionInfo().GetInfo()
	selectProgrammaticFailureModel(t, fixture)

	// Act by accepting a run, waiting for its provider response, then denying the terminal model append.
	require.NoError(t, fixture.stream.Send(userRequest("model-failure", "private user text")))
	accepted, err := fixture.stream.Recv()
	require.NoError(t, err)
	require.True(t, accepted.GetCommandResponse().HasUserRequestAccepted())
	<-started
	contention := new(programmaticv1.OpenRequest)
	contention.SetCorrelationId("resume-contention")
	contention.SetResumeSession(programmaticv1.ResumeSession_builder{SessionId: new(named.GetId())}.Build())
	require.NoError(t, fixture.stream.Send(contention))
	preFailureEvents := make([]programmaticv1.AgentEventType, 0)
	var contentionResult *programmaticv1.CommandRejected
	for contentionResult == nil {
		response, receiveErr := fixture.stream.Recv()
		require.NoError(t, receiveErr)
		if response.GetCorrelationId() == "resume-contention" {
			contentionResult = response.GetCommandResponse().GetRejected()
			break
		}
		if event := response.GetAgentEvent(); event != nil {
			preFailureEvents = append(preFailureEvents, event.GetType())
		}
	}
	require.NoError(t, os.Chmod(named.GetStoragePath(), 0o400))
	t.Cleanup(func() { _ = os.Chmod(named.GetStoragePath(), 0o600) })
	close(release)
	events, terminalText := receiveProgrammaticFailure(t, fixture)
	events = append(preFailureEvents, events...)

	// Assert contention is busy, terminal model value stays hidden, and terminal cleanup releases the gate.
	require.NotNil(t, contentionResult)
	assert.Equal(t, programmaticv1.RejectionCode_REJECTION_CODE_BUSY, contentionResult.GetCode())
	assert.Equal(t, int32(1), requests.Load())
	assert.NotContains(t, events, programmaticv1.AgentEventType_AGENT_EVENT_TYPE_MESSAGE_END)
	assert.NotContains(t, events, programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_EXECUTION_START)
	assert.Contains(t, terminalText, "session persistence failed")
	assert.Contains(t, strings.ToLower(terminalText), "permission")
	assert.Equal(t, programmaticv1.AgentEventType_AGENT_EVENT_TYPE_AGENT_SETTLED, events[len(events)-1])
	require.NoError(t, os.Chmod(named.GetStoragePath(), 0o600))
	resumed := sendProgrammaticCommand(t, fixture, "resume-after-terminal-failure", func(request *programmaticv1.OpenRequest) {
		request.SetResumeSession(programmaticv1.ResumeSession_builder{SessionId: new(named.GetId())}.Build())
	}).GetSessionInfo().GetInfo()
	assert.Equal(t, named.GetId(), resumed.GetId())
}

// TestTerminalToolResultPersistenceFailureProcessPath verifies one completed tool invocation precedes result persistence failure.
func (testSuite *ProgrammaticAppSuite) TestTerminalToolResultPersistenceFailureProcessPath() {
	t := testSuite.T()

	// Arrange a real bash extension, one provider tool call, and the active session path used by the harness fault.
	paths := testPaths(t, restartSelectionSettings())
	writeProgrammaticCredentials(t, paths)
	requests := &atomic.Int32{}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = deterministicCodexTransport{requestCount: requests, lastBody: nil}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildToolsExecutable(t))
	defer fixture.closeOwner(t)
	named := sendProgrammaticCommand(t, fixture, "name-tool-failure", func(request *programmaticv1.OpenRequest) {
		request.SetSetSessionName(programmaticv1.SetSessionName_builder{Name: new("tool failure")}.Build())
	}).GetSessionInfo().GetInfo()
	selectProgrammaticFailureModel(t, fixture)
	t.Cleanup(func() { _ = os.Chmod(named.GetStoragePath(), 0o600) })

	// Act by changing the active session file mode after tool start and before terminal result persistence.
	require.NoError(t, fixture.stream.Send(userRequest("tool-failure", "change mode")))
	accepted, err := fixture.stream.Recv()
	require.NoError(t, err)
	require.True(t, accepted.GetCommandResponse().HasUserRequestAccepted())
	events := make([]programmaticv1.AgentEventType, 0)
	terminalText := ""
	for {
		response, receiveErr := fixture.stream.Recv()
		require.NoError(t, receiveErr)
		event := response.GetAgentEvent()
		events = append(events, event.GetType())
		if event.GetType() == programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_EXECUTION_START {
			require.NoError(t, os.Chmod(named.GetStoragePath(), 0o400))
		}
		if event.GetType() == programmaticv1.AgentEventType_AGENT_EVENT_TYPE_AGENT_END {
			terminalText = event.GetAgent().GetErrorMessage()
		}
		if event.GetType() == programmaticv1.AgentEventType_AGENT_EVENT_TYPE_AGENT_SETTLED {
			break
		}
	}

	// Assert reaching tool-result persistence proves one completed invocation without a terminal result or next request.
	info, err := os.Stat(named.GetStoragePath())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o400), info.Mode().Perm())
	assert.Equal(t, int32(1), requests.Load())
	assert.Contains(t, events, programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_EXECUTION_START)
	assert.NotContains(t, events, programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_EXECUTION_END)
	assert.NotContains(t, events, programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_RESULT)
	assert.Contains(t, terminalText, "session persistence failed")
	assert.Contains(t, strings.ToLower(terminalText), "permission")
}

// TestResumeRecoveryPersistenceFailureProcessPath verifies Programmatic recovery failure preserves active state and permits retry.
func (testSuite *ProgrammaticAppSuite) TestResumeRecoveryPersistenceFailureProcessPath() {
	t := testSuite.T()
	if runtime.GOOS != "darwin" {
		t.Skip("immutable-file recovery injection requires Darwin chflags")
	}

	// Arrange one durable active session and a mode-0640 interrupted-tail fixture, then make that fixture immutable.
	paths := testPaths(t, restartSelectionSettings())
	writeProgrammaticCredentials(t, paths)
	fixture := startProgrammaticFixture(t, paths)
	defer fixture.closeOwner(t)
	active := sendProgrammaticCommand(t, fixture, "persist-active-before-recovery-failure", func(request *programmaticv1.OpenRequest) {
		request.SetSetSessionName(programmaticv1.SetSessionName_builder{Name: new("active before recovery failure")}.Build())
	}).GetSessionInfo().GetInfo()
	recoveryFixtures := writeSessionRecoveryFixture(t, active.GetStoragePath(), active.GetWorkingDirectory())
	setImmutable := exec.CommandContext(t.Context(), "/usr/bin/chflags", "uchg", recoveryFixtures.interruptedPath)
	require.NoError(t, setImmutable.Run())
	t.Cleanup(func() {
		clearCommand := exec.CommandContext(
			context.WithoutCancel(t.Context()),
			"/usr/bin/chflags",
			"nouchg",
			recoveryFixtures.interruptedPath,
		)
		_ = clearCommand.Run()
	})
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	// Act by letting immutable interrupted-tail recovery fail during mode repair, then query and mutate prior state.
	rejected := sendProgrammaticCommand(t, fixture, "resume-immutable-tail", func(request *programmaticv1.OpenRequest) {
		request.SetResumeSession(programmaticv1.ResumeSession_builder{SessionId: new(recoveryFixtures.interruptedID)}.Build())
	}).GetRejected()
	priorInfo := sendProgrammaticCommand(t, fixture, "info-after-recovery-failure", func(request *programmaticv1.OpenRequest) {
		request.SetGetSessionInfo(new(programmaticv1.GetSessionInfo))
	}).GetSessionInfo().GetInfo()
	priorEntries := sendProgrammaticCommand(t, fixture, "entries-after-recovery-failure", func(request *programmaticv1.OpenRequest) {
		request.SetGetSessionEntries(new(programmaticv1.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	priorStatistics := sendProgrammaticCommand(t, fixture, "statistics-after-recovery-failure", func(request *programmaticv1.OpenRequest) {
		request.SetGetSessionStats(new(programmaticv1.GetSessionStats))
	}).GetSessionStats().GetStatistics()
	priorRenamed := sendProgrammaticCommand(t, fixture, "name-prior-active-after-recovery-failure", func(request *programmaticv1.OpenRequest) {
		request.SetSetSessionName(programmaticv1.SetSessionName_builder{Name: new("prior active remains writable")}.Build())
	}).GetSessionInfo().GetInfo()

	// Assert detailed rejection, preserved prior state, and a failed-recovery diagnostic.
	require.NotNil(t, rejected)
	assert.Equal(t, programmaticv1.RejectionCode_REJECTION_CODE_PERSISTENCE_UNAVAILABLE, rejected.GetCode())
	assert.Contains(t, rejected.GetMessage(), "session persistence failed")
	assert.Contains(t, rejected.GetMessage(), "operation not permitted")
	assert.Equal(t, active.GetId(), priorInfo.GetId())
	assert.Equal(t, "active before recovery failure", priorInfo.GetName())
	assert.Empty(t, priorEntries)
	assert.Zero(t, priorStatistics.GetTotalMessages())
	assert.Equal(t, active.GetId(), priorRenamed.GetId())
	assert.Equal(t, "prior active remains writable", priorRenamed.GetName())
	failedRecoveryLog := logs.String()
	assert.Contains(t, failedRecoveryLog, `"operation":"resume"`)
	assert.Contains(t, failedRecoveryLog, recoveryFixtures.interruptedPath)
	assert.NotContains(t, failedRecoveryLog, "preceding tail text")
	assert.NotContains(t, failedRecoveryLog, "provider-context")
	assert.NotContains(t, failedRecoveryLog, "extension-json")

	// Act by clearing the fault, resuming the interrupted session, and mutating recovered storage.
	clearCommand := exec.CommandContext(t.Context(), "/usr/bin/chflags", "nouchg", recoveryFixtures.interruptedPath)
	require.NoError(t, clearCommand.Run())
	resumed := sendProgrammaticCommand(t, fixture, "resume-after-recovery-fault-cleared", func(request *programmaticv1.OpenRequest) {
		request.SetResumeSession(programmaticv1.ResumeSession_builder{SessionId: new(recoveryFixtures.interruptedID)}.Build())
	}).GetSessionInfo().GetInfo()
	recoveredEntries := sendProgrammaticCommand(t, fixture, "entries-after-recovery", func(request *programmaticv1.OpenRequest) {
		request.SetGetSessionEntries(new(programmaticv1.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	recoveredRenamed := sendProgrammaticCommand(t, fixture, "name-after-recovery", func(request *programmaticv1.OpenRequest) {
		request.SetSetSessionName(programmaticv1.SetSessionName_builder{Name: new("recovered writable")}.Build())
	}).GetSessionInfo().GetInfo()

	// Assert successful retry publishes complete durable state and restores mutation.
	assert.Equal(t, recoveryFixtures.interruptedID, resumed.GetId())
	require.Len(t, recoveredEntries, 1)
	assert.Equal(t, "preceding tail text", recoveredEntries[0].GetUser().GetContent()[0].GetText())
	assert.Equal(t, recoveryFixtures.interruptedID, recoveredRenamed.GetId())
	assert.Equal(t, "recovered writable", recoveredRenamed.GetName())
}

func sendProgrammaticCommand(
	t *testing.T,
	fixture *programmaticFixture,
	correlationID string,
	configure func(*programmaticv1.OpenRequest),
) *programmaticv1.CommandResponse {
	t.Helper()
	request := new(programmaticv1.OpenRequest)
	request.SetCorrelationId(correlationID)
	configure(request)
	require.NoError(t, fixture.stream.Send(request))
	response, err := fixture.stream.Recv()
	require.NoError(t, err)
	return response.GetCommandResponse()
}

func selectProgrammaticFailureModel(t *testing.T, fixture *programmaticFixture) {
	t.Helper()
	selection := sendProgrammaticCommand(t, fixture, "select-runtime-failure-model", func(request *programmaticv1.OpenRequest) {
		request.SetSelectModel(programmaticv1.SelectModel_builder{
			ProviderId: new("openai-codex"), ModelId: new("selected-model"),
		}.Build())
	}).GetModelSelection().GetSelection()
	require.Equal(t, "openai-codex", selection.GetProviderId())
	require.Equal(t, "selected-model", selection.GetModelId())
}

func receiveProgrammaticFailure(
	t *testing.T,
	fixture *programmaticFixture,
) ([]programmaticv1.AgentEventType, string) {
	t.Helper()
	events := make([]programmaticv1.AgentEventType, 0)
	terminalText := ""
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		event := response.GetAgentEvent()
		events = append(events, event.GetType())
		if event.GetType() == programmaticv1.AgentEventType_AGENT_EVENT_TYPE_AGENT_END {
			terminalText = event.GetAgent().GetErrorMessage()
		}
		if event.GetType() == programmaticv1.AgentEventType_AGENT_EVENT_TYPE_AGENT_SETTLED {
			return events, terminalText
		}
	}
}

// TestSessionRecoveryProcessPaths verifies Programmatic resume rejects completed corruption and repairs one interrupted tail.
func (testSuite *ProgrammaticAppSuite) TestSessionRecoveryProcessPaths() {
	t := testSuite.T()

	// Arrange one persisted active session and four raw recovery fixtures in its project partition.
	paths := testPaths(t, restartSelectionSettings())
	accessToken := semanticAccessToken(t, "account")
	credentials := fmt.Sprintf(
		`{"version":1,"providers":{"openai-codex":{"access_token":%q,"refresh_token":"refresh","account_id":"account","expires_at":"2099-01-01T00:00:00Z"}}}`,
		accessToken,
	)
	require.NoError(t, os.WriteFile(paths.CredentialsFile, []byte(credentials), 0o600))
	process := startProgrammaticFixture(t, paths)
	defer process.closeOwner(t)
	send := func(correlationID string, configure func(*programmaticv1.OpenRequest)) *programmaticv1.CommandResponse {
		request := new(programmaticv1.OpenRequest)
		request.SetCorrelationId(correlationID)
		configure(request)
		require.NoError(t, process.stream.Send(request))
		response, err := process.stream.Recv()
		require.NoError(t, err)
		return response.GetCommandResponse()
	}
	active := send("persist-active", func(request *programmaticv1.OpenRequest) {
		request.SetSetSessionName(programmaticv1.SetSessionName_builder{Name: new("active")}.Build())
	}).GetSessionInfo().GetInfo()
	fixtures := writeSessionRecoveryFixture(t, active.GetStoragePath(), active.GetWorkingDirectory())

	// Act by rejecting each completed invalid file, listing, and resuming the interrupted file.
	for _, test := range []struct {
		name string
		id   string
	}{
		{name: "malformed", id: fixtures.malformedID},
		{name: "wrong cwd", id: fixtures.wrongCWDID},
		{name: "unsupported", id: fixtures.unsupportedID},
	} {
		response := send("reject-"+test.name, func(request *programmaticv1.OpenRequest) {
			request.SetResumeSession(programmaticv1.ResumeSession_builder{SessionId: new(test.id)}.Build())
		})
		require.NotNil(t, response.GetRejected())
		assert.Equal(t, programmaticv1.RejectionCode_REJECTION_CODE_SESSION_UNAVAILABLE, response.GetRejected().GetCode())
		current := send("active-after-"+test.name, func(request *programmaticv1.OpenRequest) {
			request.SetGetSessionInfo(new(programmaticv1.GetSessionInfo))
		}).GetSessionInfo().GetInfo()
		assert.Equal(t, active.GetId(), current.GetId())
	}
	listed := send("list-valid", func(request *programmaticv1.OpenRequest) {
		request.SetListSessions(new(programmaticv1.ListSessions))
	}).GetSessions().GetSessions()
	listedIDs := make([]string, 0, len(listed))
	for _, summary := range listed {
		listedIDs = append(listedIDs, summary.GetInfo().GetId())
	}
	resumed := send("recover-tail", func(request *programmaticv1.OpenRequest) {
		request.SetResumeSession(programmaticv1.ResumeSession_builder{SessionId: new(fixtures.interruptedID)}.Build())
	}).GetSessionInfo().GetInfo()
	entries := send("recovered-entries", func(request *programmaticv1.OpenRequest) {
		request.SetGetSessionEntries(new(programmaticv1.GetSessionEntries))
	}).GetSessionEntries().GetEntries()

	// Assert list skips invalid files, failures preserve identity, and recovery restores only the preceding entry.
	assert.ElementsMatch(t, []string{active.GetId(), fixtures.interruptedID}, listedIDs)
	assert.Equal(t, fixtures.interruptedID, resumed.GetId())
	require.Len(t, entries, 1)
	assert.Equal(t, "preceding tail text", entries[0].GetUser().GetContent()[0].GetText())
	recovered, err := os.ReadFile(fixtures.interruptedPath)
	require.NoError(t, err)
	assert.True(t, bytes.HasSuffix(recovered, []byte{'\n'}))
	info, err := os.Stat(fixtures.interruptedPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
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

// TestApplicationCancellationRetainsBufferedProtocolCompletion verifies ready terminal sources survive arbitration.
func (testSuite *ProgrammaticAppSuite) TestApplicationCancellationRetainsBufferedProtocolCompletion() {
	t := testSuite.T()

	// Arrange repeated valid canceled contexts with an already-buffered protocol completion.
	for index := range 16 {
		protocolErr := status.Error(codes.InvalidArgument, fmt.Sprintf("unique buffered protocol failure %d", index))
		cleanupErr := fmt.Errorf("unique buffered cleanup failure %d", index)
		completions := make(chan controllerprogrammatic.SessionCompletion, 1)
		completions <- controllerprogrammatic.SessionCompletion{
			Cause: controllerprogrammatic.SessionCompletionApplicationCanceled,
			Err:   errors.Join(context.Canceled, protocolErr), CleanupErr: cleanupErr,
		}
		server := grpc.NewServer(grpc.WaitForHandlers(true))
		socketService, err := programmaticsocket.New(t.Context(), "")
		require.NoError(t, err)
		canceledContext, cancel := context.WithCancel(t.Context())
		cancel()

		// Act with cancellation and completion ready before arbitration.
		runErr := runProgrammaticServer(
			canceledContext, server, socketService, completions, newIdleProgrammaticTestSession(t),
		)

		// Assert cancellation, protocol, and cleanup causes each survive once.
		require.ErrorIs(t, runErr, context.Canceled)
		require.ErrorIs(t, runErr, protocolErr)
		require.ErrorIs(t, runErr, cleanupErr)
		assert.Equal(t, 1, strings.Count(runErr.Error(), context.Canceled.Error()))
		assert.Equal(t, 1, strings.Count(runErr.Error(), protocolErr.Error()))
		assert.Equal(t, 1, strings.Count(runErr.Error(), cleanupErr.Error()))
		require.NoError(t, socketService.Close())
	}
}

// TestApplicationCancellationRetainsServeFailure verifies listener failure survives cancellation.
func (testSuite *ProgrammaticAppSuite) TestApplicationCancellationRetainsServeFailure() {
	t := testSuite.T()

	// Arrange cancellation and one preloaded independent Serve result before explicit Stop.
	serveErr := errors.New("unique deterministic Serve failure")
	serveResults := make(chan error, 1)
	serveResults <- serveErr
	collector := programmaticShutdownCollector{
		completions:  make(chan controllerprogrammatic.SessionCompletion),
		serveResults: serveResults, completionRead: false, serveResultRead: false,
	}
	stopCalled := false

	// Act through the same bounded shutdown collector used by the real server path.
	runErr := collector.finish(context.Canceled, context.Canceled, nil, func() { stopCalled = true })

	// Assert cancellation and the independent Serve failure survive without scheduler delays.
	require.ErrorIs(t, runErr, context.Canceled)
	require.ErrorIs(t, runErr, serveErr)
	assert.Equal(t, 1, strings.Count(runErr.Error(), context.Canceled.Error()))
	assert.Equal(t, 1, strings.Count(runErr.Error(), serveErr.Error()))
	assert.True(t, stopCalled)
}

// TestApplicationPureCancellationIgnoresExplicitStop verifies owned Stop adds no server error.
func (testSuite *ProgrammaticAppSuite) TestApplicationPureCancellationIgnoresExplicitStop() {
	t := testSuite.T()

	// Arrange a valid canceled context with no other terminal source.
	server := grpc.NewServer(grpc.WaitForHandlers(true))
	socketService, err := programmaticsocket.New(t.Context(), "")
	require.NoError(t, err)
	canceledContext, cancel := context.WithCancel(t.Context())
	cancel()
	completions := make(chan controllerprogrammatic.SessionCompletion)

	// Act through pure application cancellation and explicit server Stop.
	runErr := runProgrammaticServer(
		canceledContext, server, socketService, completions, newIdleProgrammaticTestSession(t),
	)

	// Assert cancellation stays canonical and server Stop adds no sibling.
	require.ErrorIs(t, runErr, context.Canceled)
	assert.Equal(t, context.Canceled.Error(), runErr.Error())
	assert.NotContains(t, runErr.Error(), grpc.ErrServerStopped.Error())
	require.NoError(t, socketService.Close())
}

// TestApplicationCancellationDoesNotDuplicateCleanupCancellation verifies context-first cleanup deduplication.
func (testSuite *ProgrammaticAppSuite) TestApplicationCancellationDoesNotDuplicateCleanupCancellation() {
	t := testSuite.T()

	// Arrange an active operation whose cancellation returns cancellation plus one independent cleanup error.
	controller := gomock.NewController(t)
	coordinator := hostprogrammatic.NewMockCoordinator(controller)
	coordinator.EXPECT().PrepareRun().Return("run-1", nil)
	coordinator.EXPECT().CancelPrepared(gomock.Any()).AnyTimes()
	cleanupErr := errors.New("unique context-selected cleanup failure")
	runStarted := make(chan struct{})
	coordinator.EXPECT().RunPrepared(gomock.Any(), "run-1", "request").DoAndReturn(
		func(ctx context.Context, _, _ string) (agent.RunOutcome, error) {
			close(runStarted)
			<-ctx.Done()
			return agent.RunOutcomeFailed, errors.Join(ctx.Err(), cleanupErr)
		},
	)
	delivery := hostprogrammatic.NewDelivery()
	sessionService := hostprogrammatic.New(
		coordinator, nil,
		func() agentrun.State {
			return agentrun.State{
				Status: agentrun.StatusIdle, RunID: mo.None[string](),
				PartialResponse: mo.None[model.Response](), ToolPreviews: nil,
			}
		},
		func() []agent.HistoryEntry { return nil }, nil, delivery,
	)
	_, operation, err := sessionService.Handle(t.Context(), controllerprogrammatic.Command{
		CorrelationID: "c1", Kind: controllerprogrammatic.CommandUserRequest, UserText: mo.Some("request"),
		ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](),
		ReasoningChoice: mo.None[model.ReasoningChoice](), SessionID: mo.None[session.ID](),
		SessionName: mo.None[string](),
	})
	require.NoError(t, err)
	require.NotNil(t, operation)
	operation.Start()
	<-runStarted
	server := grpc.NewServer(grpc.WaitForHandlers(true))
	socketService, err := programmaticsocket.New(t.Context(), "")
	require.NoError(t, err)
	completions := make(chan controllerprogrammatic.SessionCompletion)
	canceledContext, cancel := context.WithCancel(t.Context())
	cancel()

	// Act through the context-selected application cancellation branch.
	runErr := runProgrammaticServer(canceledContext, server, socketService, completions, sessionService)

	// Assert cancellation and cleanup remain visible without repeating cancellation text.
	require.ErrorIs(t, runErr, context.Canceled)
	require.ErrorIs(t, runErr, cleanupErr)
	assert.Equal(t, 1, strings.Count(runErr.Error(), context.Canceled.Error()))
	assert.Equal(t, 1, strings.Count(runErr.Error(), cleanupErr.Error()))
	require.NoError(t, socketService.Close())
}

// newIdleProgrammaticTestSession creates an idle concrete session for application arbitration tests.
func newIdleProgrammaticTestSession(t *testing.T) *hostprogrammatic.Service {
	t.Helper()
	return hostprogrammatic.New(
		hostprogrammatic.NewMockCoordinator(gomock.NewController(t)), nil,
		func() agentrun.State {
			return agentrun.State{
				Status: agentrun.StatusIdle, RunID: mo.None[string](),
				PartialResponse: mo.None[model.Response](), ToolPreviews: nil,
			}
		},
		func() []agent.HistoryEntry { return nil }, nil, hostprogrammatic.NewDelivery(),
	)
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
	// Arrange a fixture with an automatically allocated socket directory.
	t := testSuite.T()
	paths := testPaths(t, codexSettings(""))
	fixture := startProgrammaticFixtureAtPath(t, paths, t.TempDir(), "")
	directory := filepath.Dir(fixture.socketPath)

	// Act by closing the owning programmatic stream.
	fixture.closeOwner(t)
	_, err := os.Stat(directory)

	// Assert shutdown removes the automatically created directory.
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
type runtimeFailureTransport struct {
	body     *atomic.Value
	requests *atomic.Int32
	started  chan<- struct{}
	release  <-chan struct{}
}

func (transport runtimeFailureTransport) RoundTrip(*http.Request) (*http.Response, error) {
	if transport.requests.Add(1) != 1 {
		return nil, errors.New("runtime failure transport received a dependent provider request")
	}
	if transport.started != nil {
		transport.started <- struct{}{}
		<-transport.release
	}
	body, ok := transport.body.Load().(string)
	if !ok {
		return nil, errors.New("runtime failure transport has no response body")
	}
	return &http.Response{
		StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header),
		Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0, ContentLength: 0,
		TransferEncoding: nil, Close: false, Uncompressed: false, Trailer: nil, Request: nil, TLS: nil,
	}, nil
}

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

// TestSessionUsageAvailabilitySurvivesRestart verifies all cost states through the real RPC process.
func (testSuite *ProgrammaticAppSuite) TestSessionUsageAvailabilitySurvivesRestart() {
	t := testSuite.T()
	// Arrange the complete cost-state matrix for the existing Programmatic process fixture.
	tests := []struct {
		name      string
		submit    bool
		usageJSON string
		expected  statisticsObservation
	}{
		{
			name: "empty session", submit: false, usageJSON: "",
			expected: statisticsObservation{
				UserMessages: 0, ModelResponses: 0, ToolCalls: 0, ToolResults: 0, TotalMessages: 0,
				Tokens: tokenUsageObservation{
					Present: true, InputTokens: 0, OutputTokens: 0, CacheReadTokens: 0,
					CacheWriteTokens: 0, ReasoningTokens: 0, TotalTokens: 0,
				},
				EstimatedCost: costObservation{
					Present: true, Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0, Total: 0,
				},
				CostGroupCount: 0, GroupProvider: "", GroupModel: "", GroupCost: costObservation{},
			},
		},
		{
			name: "known zero", submit: true,
			usageJSON: `{"input_tokens":0,"output_tokens":0,"total_tokens":0,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}`,
			expected: statisticsObservation{
				UserMessages: 1, ModelResponses: 1, ToolCalls: 0, ToolResults: 0, TotalMessages: 2,
				Tokens: tokenUsageObservation{
					Present: true, InputTokens: 0, OutputTokens: 0, CacheReadTokens: 0,
					CacheWriteTokens: 0, ReasoningTokens: 0, TotalTokens: 0,
				},
				EstimatedCost: costObservation{
					Present: true, Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0, Total: 0,
				},
				CostGroupCount: 1, GroupProvider: "openai-codex", GroupModel: "gpt-test",
				GroupCost: costObservation{
					Present: true, Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0, Total: 0,
				},
			},
		},
		{
			name: "available nonzero", submit: true,
			usageJSON: `{"input_tokens":10,"output_tokens":4,"total_tokens":99,"input_tokens_details":{"cached_tokens":2,"cache_write_tokens":1},"output_tokens_details":{"reasoning_tokens":3}}`,
			expected: statisticsObservation{
				UserMessages: 1, ModelResponses: 1, ToolCalls: 0, ToolResults: 0, TotalMessages: 2,
				Tokens: tokenUsageObservation{
					Present: true, InputTokens: 7, OutputTokens: 4, CacheReadTokens: 2,
					CacheWriteTokens: 1, ReasoningTokens: 3, TotalTokens: 14,
				},
				EstimatedCost: costObservation{
					Present: true, Input: 0.000007, Output: 0.000008,
					CacheRead: 0.000006, CacheWrite: 0.000004, Total: 0.000025,
				},
				CostGroupCount: 1, GroupProvider: "openai-codex", GroupModel: "gpt-test",
				GroupCost: costObservation{
					Present: true, Input: 0.000007, Output: 0.000008,
					CacheRead: 0.000006, CacheWrite: 0.000004, Total: 0.000025,
				},
			},
		},
		{
			name: "unavailable", submit: true, usageJSON: "",
			expected: statisticsObservation{
				UserMessages: 1, ModelResponses: 1, ToolCalls: 0, ToolResults: 0, TotalMessages: 2,
				Tokens: tokenUsageObservation{}, EstimatedCost: costObservation{}, CostGroupCount: 1,
				GroupProvider: "openai-codex", GroupModel: "gpt-test", GroupCost: costObservation{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Arrange one durable matrix state and persistent process paths.
			paths := testPaths(t, pricedCodexSettings())
			accessToken := semanticAccessToken(t, "account")
			credentials := fmt.Sprintf(
				`{"version":1,"providers":{"openai-codex":{"access_token":%q,"refresh_token":"refresh","account_id":"account","expires_at":"2099-01-01T00:00:00Z"}}}`,
				accessToken,
			)
			require.NoError(t, os.WriteFile(paths.CredentialsFile, []byte(credentials), 0o600))
			previousTransport := http.DefaultTransport
			http.DefaultTransport = usageCodexTransport{usageJSON: test.usageJSON}
			t.Cleanup(func() { http.DefaultTransport = previousTransport })
			fixture := startProgrammaticFixture(t, paths)

			// Act by persisting the state and querying statistics before and after explicit resume.
			if test.submit {
				require.NoError(t, fixture.stream.Send(userRequest("usage", "usage request")))
				_, err := fixture.stream.Recv()
				require.NoError(t, err)
				waitProgrammaticSettled(t, fixture)
			} else {
				request := new(programmaticv1.OpenRequest)
				request.SetCorrelationId("persist-empty")
				request.SetSetSessionName(programmaticv1.SetSessionName_builder{Name: new("empty cost session")}.Build())
				require.NoError(t, fixture.stream.Send(request))
				_, err := fixture.stream.Recv()
				require.NoError(t, err)
			}
			before := requestProgrammaticStatistics(t, fixture.stream, "before")
			info := requestProgrammaticInfo(t, fixture.stream, "info")
			fixture.closeOwner(t)
			restarted := startProgrammaticFixture(t, paths)
			defer restarted.closeOwner(t)
			request := new(programmaticv1.OpenRequest)
			request.SetCorrelationId("resume")
			request.SetResumeSession(programmaticv1.ResumeSession_builder{SessionId: new(info.GetId())}.Build())
			require.NoError(t, restarted.stream.Send(request))
			resumeResponse, err := restarted.stream.Recv()
			require.NoError(t, err)
			after := requestProgrammaticStatistics(t, restarted.stream, "after")

			// Assert identity and every accounting field survive JSONL reopen and Host reconstruction.
			assertProgrammaticSessionInfoEqual(t, info, resumeResponse.GetCommandResponse().GetSessionInfo().GetInfo())
			assertProgrammaticStatisticsObservation(t, test.expected, before)
			assertProgrammaticStatisticsObservation(t, test.expected, after)
		})
	}
}

// assertProgrammaticStatisticsObservation compares all Programmatic accounting fields.
func assertProgrammaticStatisticsObservation(
	t *testing.T,
	expected statisticsObservation,
	statistics *programmaticv1.SessionStatistics,
) {
	t.Helper()
	assertStatisticsObservation(t, expected, observeProgrammaticStatistics(statistics))
}

// observeProgrammaticStatistics preserves all Programmatic statistics fields for matrix assertions.
func observeProgrammaticStatistics(statistics *programmaticv1.SessionStatistics) statisticsObservation {
	observation := statisticsObservation{
		UserMessages: statistics.GetUserMessages(), ModelResponses: statistics.GetModelResponses(),
		ToolCalls: statistics.GetToolCalls(), ToolResults: statistics.GetToolResults(),
		TotalMessages: statistics.GetTotalMessages(), Tokens: tokenUsageObservation{},
		EstimatedCost:  observeProgrammaticCost(statistics.GetEstimatedCost()),
		CostGroupCount: len(statistics.GetCostBreakdown()), GroupProvider: "", GroupModel: "",
		GroupCost: costObservation{},
	}
	if tokens := statistics.GetTokens(); tokens != nil {
		observation.Tokens = tokenUsageObservation{
			Present: true, InputTokens: tokens.GetInputTokens(), OutputTokens: tokens.GetOutputTokens(),
			CacheReadTokens: tokens.GetCacheReadTokens(), CacheWriteTokens: tokens.GetCacheWriteTokens(),
			ReasoningTokens: tokens.GetReasoningTokens(), TotalTokens: tokens.GetTotalTokens(),
		}
	}
	if len(statistics.GetCostBreakdown()) == 1 {
		group := statistics.GetCostBreakdown()[0]
		observation.GroupProvider = group.GetProviderId()
		observation.GroupModel = group.GetModelId()
		observation.GroupCost = observeProgrammaticCost(group.GetEstimatedCost())
	}
	return observation
}

// observeProgrammaticCost preserves optional Programmatic cost fields for process assertions.
func observeProgrammaticCost(cost *programmaticv1.EstimatedCost) costObservation {
	if cost == nil {
		return costObservation{}
	}
	return costObservation{
		Present: true, Input: cost.GetInput(), Output: cost.GetOutput(),
		CacheRead: cost.GetCacheRead(), CacheWrite: cost.GetCacheWrite(), Total: cost.GetTotal(),
	}
}

func requestProgrammaticStatistics(
	t *testing.T,
	stream grpc.BidiStreamingClient[programmaticv1.OpenRequest, programmaticv1.OpenResponse],
	correlationID string,
) *programmaticv1.SessionStatistics {
	t.Helper()
	request := new(programmaticv1.OpenRequest)
	request.SetCorrelationId(correlationID)
	request.SetGetSessionStats(new(programmaticv1.GetSessionStats))
	require.NoError(t, stream.Send(request))
	response, err := stream.Recv()
	require.NoError(t, err)
	return response.GetCommandResponse().GetSessionStats().GetStatistics()
}

func requestProgrammaticInfo(
	t *testing.T,
	stream grpc.BidiStreamingClient[programmaticv1.OpenRequest, programmaticv1.OpenResponse],
	correlationID string,
) *programmaticv1.SessionInfo {
	t.Helper()
	request := new(programmaticv1.OpenRequest)
	request.SetCorrelationId(correlationID)
	request.SetGetSessionInfo(new(programmaticv1.GetSessionInfo))
	require.NoError(t, stream.Send(request))
	response, err := stream.Recv()
	require.NoError(t, err)
	return response.GetCommandResponse().GetSessionInfo().GetInfo()
}

type usageCodexTransport struct {
	usageJSON string
}

func (transport usageCodexTransport) RoundTrip(*http.Request) (*http.Response, error) {
	usageField := ""
	if transport.usageJSON != "" {
		usageField = fmt.Sprintf(",\"usage\":%s", transport.usageJSON)
	}
	body := fmt.Sprintf("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"usage-response\",\"model\":\"selected-model\",\"status\":\"completed\",\"service_tier\":\"default\",\"metadata\":{}%s,\"output\":[]}}\n\ndata: [DONE]\n\n", usageField)
	return &http.Response{
		StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header),
		Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0, ContentLength: 0, TransferEncoding: nil,
		Close: false, Uncompressed: false, Trailer: nil, Request: nil, TLS: nil,
	}, nil
}
