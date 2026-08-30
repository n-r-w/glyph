//go:build integration

package app

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestSessionLifecycleRoundTrip verifies Programmatic restart restores full public and continuation content.
func (testSuite *ProgrammaticAppSuite) TestSessionLifecycleRoundTrip() {
	t := testSuite.T()

	// Arrange persistent paths, provider transport, extension tools, and a Programmatic stream.
	paths := testPaths(t, restartSelectionSettings())
	accessToken := semanticAccessToken(t, "account")
	credentials := fmt.Sprintf(
		`{"version":1,"providers":{"openai-codex":{"access_token":%q,`+
			`"refresh_token":"refresh","account_id":"account",`+
			`"expires_at":"2099-01-01T00:00:00Z"}}}`,
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
	restartSend := func(
		correlationID string,
		configure func(*programmaticv1.OpenRequest),
	) *programmaticv1.CommandResponse {
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
	require.Len(t, messages, 8)
	assert.Equal(t, "restart text", messages[0].GetUser().GetContent()[0].GetText())
	assert.Equal(t, "resp-1", messages[1].GetModel().GetResponseId())
	require.Len(t, messages[1].GetModel().GetContent(), 2)
	assert.Equal(
		t,
		programmaticv1.ModelResponseItem_Reasoning_case,
		messages[1].GetModel().GetContent()[0].WhichContent(),
	)
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
	require.Len(t, messages[7].GetUser().GetContent(), 1)
	assert.Contains(t, messages[7].GetUser().GetContent()[0].GetText(), "Full branch summary")
	entries := restartSend("restart-entries", func(request *programmaticv1.OpenRequest) {
		request.SetGetSessionEntries(new(programmaticv1.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	require.Len(t, entries, 8)
	assert.NotEmpty(t, entries[0].GetId())
	assert.True(t, entries[0].HasCreatedTime())
	assert.Equal(t, "## Goal\n\nFull branch summary", entries[7].GetBranchSummary().GetSummary())

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
	assert.Contains(t, string(body), "Full branch summary")
	assert.NotContains(t, string(body), "full-extension")
	assert.Contains(t, string(body), "continue")
	assert.Contains(t, string(body), `"model":"selected-model"`)
	assert.Contains(t, string(body), `"effort":"high"`)
	assert.Less(t, bytes.Index(body, []byte("restart text")), bytes.Index(body, []byte(`"type":"function_call"`)))
	assert.Less(
		t,
		bytes.Index(body, []byte(`"type":"function_call"`)),
		bytes.Index(body, []byte(`"type":"function_call_output"`)),
	)
	assert.Less(
		t,
		bytes.Index(body, []byte(`"type":"function_call_output"`)),
		bytes.Index(body, []byte("Request complete.")),
	)
	assert.Less(t, bytes.Index(body, []byte("Request complete.")), bytes.Index(body, []byte("continue")))
	assert.Equal(t, int32(3), requestCount.Load())
}
