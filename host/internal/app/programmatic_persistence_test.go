//go:build integration

package app

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

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
	for {
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
	resumed := sendProgrammaticCommand(
		t,
		fixture,
		"resume-after-terminal-failure",
		func(request *programmaticv1.OpenRequest) {
			request.SetResumeSession(programmaticv1.ResumeSession_builder{SessionId: new(named.GetId())}.Build())
		},
	).GetSessionInfo().
		GetInfo()
	assert.Equal(t, named.GetId(), resumed.GetId())
}

// TestTerminalToolResultPersistenceFailureProcessPath verifies one completed tool invocation precedes result
// persistence failure.
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

// TestResumeRecoveryPersistenceFailureProcessPath verifies Programmatic recovery failure preserves active state and
// permits retry.
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
	active := sendProgrammaticCommand(
		t,
		fixture,
		"persist-active-before-recovery-failure",
		func(request *programmaticv1.OpenRequest) {
			request.SetSetSessionName(
				programmaticv1.SetSessionName_builder{Name: new("active before recovery failure")}.Build(),
			)
		},
	).GetSessionInfo().
		GetInfo()
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
		request.SetResumeSession(
			programmaticv1.ResumeSession_builder{SessionId: new(recoveryFixtures.interruptedID)}.Build(),
		)
	}).GetRejected()
	priorInfo := sendProgrammaticCommand(
		t,
		fixture,
		"info-after-recovery-failure",
		func(request *programmaticv1.OpenRequest) {
			request.SetGetSessionInfo(new(programmaticv1.GetSessionInfo))
		},
	).GetSessionInfo().
		GetInfo()
	priorEntries := sendProgrammaticCommand(
		t,
		fixture,
		"entries-after-recovery-failure",
		func(request *programmaticv1.OpenRequest) {
			request.SetGetSessionEntries(new(programmaticv1.GetSessionEntries))
		},
	).GetSessionEntries().
		GetEntries()
	priorStatistics := sendProgrammaticCommand(
		t,
		fixture,
		"statistics-after-recovery-failure",
		func(request *programmaticv1.OpenRequest) {
			request.SetGetSessionStats(new(programmaticv1.GetSessionStats))
		},
	).GetSessionStats().
		GetStatistics()
	priorRenamed := sendProgrammaticCommand(
		t,
		fixture,
		"name-prior-active-after-recovery-failure",
		func(request *programmaticv1.OpenRequest) {
			request.SetSetSessionName(
				programmaticv1.SetSessionName_builder{Name: new("prior active remains writable")}.Build(),
			)
		},
	).GetSessionInfo().
		GetInfo()

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
	resumed := sendProgrammaticCommand(
		t,
		fixture,
		"resume-after-recovery-fault-cleared",
		func(request *programmaticv1.OpenRequest) {
			request.SetResumeSession(
				programmaticv1.ResumeSession_builder{SessionId: new(recoveryFixtures.interruptedID)}.Build(),
			)
		},
	).GetSessionInfo().
		GetInfo()
	recoveredEntries := sendProgrammaticCommand(
		t,
		fixture,
		"entries-after-recovery",
		func(request *programmaticv1.OpenRequest) {
			request.SetGetSessionEntries(new(programmaticv1.GetSessionEntries))
		},
	).GetSessionEntries().
		GetEntries()
	recoveredRenamed := sendProgrammaticCommand(
		t,
		fixture,
		"name-after-recovery",
		func(request *programmaticv1.OpenRequest) {
			request.SetSetSessionName(programmaticv1.SetSessionName_builder{Name: new("recovered writable")}.Build())
		},
	).GetSessionInfo().
		GetInfo()

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
	selection := sendProgrammaticCommand(
		t,
		fixture,
		"select-runtime-failure-model",
		func(request *programmaticv1.OpenRequest) {
			request.SetSelectModel(programmaticv1.SelectModel_builder{
				ProviderId: new("openai-codex"), ModelId: new("selected-model"),
			}.Build())
		},
	).GetModelSelection().
		GetSelection()
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

// TestSessionRecoveryProcessPaths verifies Programmatic resume rejects completed corruption and repairs one interrupted
// tail.
func (testSuite *ProgrammaticAppSuite) TestSessionRecoveryProcessPaths() {
	t := testSuite.T()

	// Arrange one persisted active session and four raw recovery fixtures in its project partition.
	paths := testPaths(t, restartSelectionSettings())
	accessToken := semanticAccessToken(t, "account")
	credentials := fmt.Sprintf(
		`{"version":1,"providers":{"openai-codex":{"access_token":%q,`+
			`"refresh_token":"refresh","account_id":"account",`+
			`"expires_at":"2099-01-01T00:00:00Z"}}}`,
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
		assert.Equal(
			t,
			programmaticv1.RejectionCode_REJECTION_CODE_SESSION_UNAVAILABLE,
			response.GetRejected().GetCode(),
		)
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
