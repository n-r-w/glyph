//go:build integration

package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	operationpb "github.com/n-r-w/glyph/pkg/operation/v1"

	"go.uber.org/mock/gomock"

	"google.golang.org/protobuf/encoding/protojson"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// conversationProviderResponseEnvironment names the environment variable for a response with reported usage.
const conversationProviderResponseEnvironment = "GLYPH_REFERENCE_CONVERSATION_RESPONSE"

// conversationProviderRetryEnvironment names the environment variable for a transient conversation failure.
const conversationProviderRetryEnvironment = "GLYPH_REFERENCE_CONVERSATION_RETRY"

// conversationProviderOverflowEnvironment names the environment variable for a single conversation-context rejection.
const conversationProviderOverflowEnvironment = "GLYPH_REFERENCE_CONVERSATION_OVERFLOW"

// conversationResponseSSE is a provider response fixture with provider identity and reported usage.
const conversationResponseSSE = `data: {"type":"response.output_text.delta","output_index":0,` +
	`"content_index":0,"delta":"Request complete."}` + "\n\n" +
	`data: {"type":"response.output_item.done","output_index":0,"item":{"id":"msg-observed",` +
	`"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text",` +
	`"text":"Request complete.","annotations":[],"logprobs":[]}]}}` + "\n\n" +
	`data: {"type":"response.completed","response":{"id":"resp-observed","model":"gpt-reported",` +
	`"status":"completed","usage":{"input_tokens":7,"output_tokens":4,"total_tokens":11,` +
	`"input_tokens_details":{"cached_tokens":2,"cache_write_tokens":1},` +
	`"output_tokens_details":{"reasoning_tokens":3}},"output":[]}}` + "\n\n" + "data: [DONE]\n\n"

// conversationZeroResponseSSE is a provider response fixture with usage present and all values zero.
const conversationZeroResponseSSE = `data: {"type":"response.completed",` +
	`"response":{"id":"zero-usage-response","status":"completed",` +
	`"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0},"output":[]}}` + "\n\n" + "data: [DONE]\n\n"

// conversationObservationSuite exercises the public conversation completion path.
type conversationObservationSuite struct {
	// Suite owns assertions and lifecycle.
	suite.Suite
}

// TestConversationObservation checks the captured owner's successful attempt input.
func TestConversationObservation(t *testing.T) {
	// Arrange the reference extension's observation artifact prefix.
	t.Setenv(conversationObservationEnvironment, filepath.Join(t.TempDir(), "observation.json"))
	// Act through the real application and SDK in TestScenario.
	// Assert the completed conversation projection in TestScenario.
	suite.Run(t, &conversationObservationSuite{Suite: suite.Suite{}})
}

// SetupTest isolates the reference extension's observation artifacts.
func (s *conversationObservationSuite) SetupTest() {
	t := s.T()
	prefix := os.Getenv(conversationObservationEnvironment)
	t.Setenv(conversationObservationEnvironment, prefix+"."+filepath.Base(t.Name()))
}

// TestScenario checks observation through production application, runtime, SDK and sessions.
func (s *conversationObservationSuite) TestScenario() {
	t := s.T()
	// Arrange the external provider and the public-only reference extension.
	directory := buildManualPolicyFixture(t)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	// Act through a public conversation request.
	completeProgrammaticRequest(t, fixture, userRequest("observed-conversation", manualOriginalText))
	// Assert observation includes the actual attempt and provider response.
	encoded, err := os.ReadFile(os.Getenv(conversationObservationEnvironment))
	require.NoError(t, err)
	require.Contains(t, string(encoded), manualOriginalText)
	require.Contains(t, string(encoded), "response")
	observed := new(extensionpb.CompletedConversationObservation)
	require.NoError(t, protojson.Unmarshal(encoded, observed))
	require.False(t, observed.GetResponse().HasUsage())
	calls, callsErr := filepath.Glob(os.Getenv(conversationObservationEnvironment) + conversationObservationCallGlob)
	require.NoError(t, callsErr)
	require.Len(t, calls, 1)
	require.Len(t, bodies.snapshot(), 1)
	fixture.closeOwner(t)
}

// TestObserverFailure checks retained provider content through the public failure and session path.
func (s *conversationObservationSuite) TestObserverFailure() {
	// Arrange, Act and Assert the failed observation in assertObserverFailure.
	s.assertObserverFailure(false)
}

// TestRetryThenObserverFailure checks retention of the earlier failed provider cause.
func (s *conversationObservationSuite) TestRetryThenObserverFailure() {
	// Arrange, Act and Assert the retried conversation in assertObserverFailure.
	s.assertObserverFailure(true)
}

// assertObserverFailure checks the completed attempt retained by a failed owner observation.
func (s *conversationObservationSuite) assertObserverFailure(retry bool) {
	t := s.T()
	t.Setenv(conversationProviderResponseEnvironment, manualFixtureEnabled)
	if retry {
		t.Setenv(conversationProviderRetryEnvironment, manualFixtureEnabled)
	}
	// Arrange an owner that rejects the successful provider attempt.
	t.Setenv(conversationFailureEnvironment, manualFixtureEnabled)
	directory := buildManualPolicyFixture(t)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	// Act through public request admission and terminal delivery.
	request := userRequest("failed-observation", manualOriginalText)
	require.NoError(t, fixture.stream.Send(request))
	var terminal *programmaticpb.HostEvent
	for terminal == nil {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetOperationId() != request.GetOperationId() {
			continue
		}
		event := response.GetEvent()
		if event.HasCompleted() || event.HasFailed() || event.HasCanceled() {
			terminal = event
		}
	}
	// Assert that the application preserves the owner cause and provider result without another attempt.
	require.NotNil(t, terminal.GetFailed())
	require.Contains(t, terminal.GetFailed().GetMessage(), "original conversation observer cause")
	stored := sendProgrammaticOperation(
		t,
		fixture,
		"observed-failed-history",
		func(request *programmaticpb.OpenRequest) {
			programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
		},
	).GetSessionEntries()
	encoded, err := protojson.Marshal(stored)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "Request complete.")
	require.Contains(t, string(encoded), "original conversation observer cause")
	observed := new(extensionpb.CompletedConversationObservation)
	observationBytes, readErr := os.ReadFile(os.Getenv(conversationObservationEnvironment))
	require.NoError(t, readErr)
	require.NoError(t, protojson.Unmarshal(observationBytes, observed))
	require.Equal(t, "resp-observed", observed.GetResponse().GetResponseId())
	require.Equal(t, "gpt-reported", observed.GetResponse().GetResponseModelId())
	require.Equal(t, int64(4), observed.GetResponse().GetUsage().GetInputTokens())
	require.Equal(t, int64(4), observed.GetResponse().GetUsage().GetOutputTokens())
	require.Equal(t, int64(11), observed.GetResponse().GetUsage().GetTotalTokens())
	require.Equal(t, int64(2), observed.GetResponse().GetUsage().GetCachedInputTokens())
	require.Equal(t, int64(1), observed.GetResponse().GetUsage().GetCacheWriteTokens())
	require.Equal(t, int64(3), observed.GetResponse().GetUsage().GetReasoningTokens())
	require.Contains(t, string(encoded), "resp-observed")
	require.Contains(t, string(encoded), `"inputTokens":"4"`)
	calls, callsErr := filepath.Glob(os.Getenv(conversationObservationEnvironment) + conversationObservationCallGlob)
	require.NoError(t, callsErr)
	require.Len(t, calls, 1)
	if retry {
		require.Contains(t, terminal.GetFailed().GetMessage(), "prior conversation provider cause")
		require.Contains(t, string(encoded), "prior conversation provider cause")
		require.Len(t, bodies.snapshot(), 2)
	} else {
		require.Len(t, bodies.snapshot(), 1)
	}
	fixture.closeOwner(t)
}

// TestHeldObservation checks that nested model work precedes logical completion and persistence.
func (s *conversationObservationSuite) TestHeldObservation() {
	// Arrange nested configured work in assertHeldObservation.
	// Act through public conversation completion.
	// Assert settlement before success and persistence.
	s.assertHeldObservation(false, false)
}

// TestCanceledObservation checks that the application retains diagnostics and joins nested model work
// after cancellation following a retry.
func (s *conversationObservationSuite) TestCanceledObservation() {
	// Arrange nested configured work in assertHeldObservation.
	// Act through public cancellation.
	// Assert joined children and an aborted logical outcome.
	s.assertHeldObservation(true, false)
}

// TestCanceledObservationWithIndependentFailure checks acquired failure precedence over cancellation.
func (s *conversationObservationSuite) TestCanceledObservationWithIndependentFailure() {
	// Arrange a provider failure acquired during nested cancellation.
	// Act through public cancellation in assertHeldObservation.
	// Assert joined children and the independent cause in the failed outcome.
	s.assertHeldObservation(true, true)
}

// assertHeldObservation checks owner settlement through ordinary nested model access.
func (s *conversationObservationSuite) assertHeldObservation(cancel, independent bool) {
	t := s.T()
	// Arrange a public observer with a blocked configured-model child.
	t.Setenv(conversationModelEnvironment, manualFixtureEnabled)
	toolPath := filepath.Join(t.TempDir(), "held-observation-tool.json")
	if cancel {
		t.Setenv(conversationToolEnvironment, toolPath)
	}
	childPosition := 2
	if cancel {
		childPosition = 3
	}
	directory := buildManualPolicyFixture(t)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	started, release := make(chan struct{}), make(chan struct{})
	childStopped := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	bodies := &requestBodies{}
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().
		RoundTrip(gomock.Any()).
		AnyTimes().
		DoAndReturn(func(request *http.Request) (*http.Response, error) {
			encoded, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			position := bodies.append(encoded)
			statusCode, payload := http.StatusOK, finalResponseSSE
			if cancel {
				payload = conversationToolResponseSSE
				if position == 1 {
					statusCode = http.StatusServiceUnavailable
					payload = `{"error":{"message":"prior conversation provider cause","type":"server_error"}}`
				}
			}
			if position == childPosition {
				defer close(childStopped)
				close(started)
				select {
				case <-release:
					payload = configuredModelResponseSSE
				case <-request.Context().Done():
					cause := context.Cause(request.Context())
					if independent {
						cause = errors.Join(cause, errors.New("independent nested provider cause"))
					}
					return nil, cause
				}
			}
			return &http.Response{
				Status:     "",
				StatusCode: statusCode,
				Proto:      "",
				ProtoMajor: 0,
				ProtoMinor: 0,
				Header: make(
					http.Header,
				),
				Body:             io.NopCloser(bytes.NewBufferString(payload)),
				ContentLength:    0,
				TransferEncoding: nil,
				Close:            false,
				Uncompressed:     false,
				Trailer:          nil,
				Request:          nil,
				TLS:              nil,
			}, nil
		})
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	// Act through the public stream while the owner awaits its nested result.
	request := userRequest("held-observation", manualOriginalText)
	require.NoError(t, fixture.stream.Send(request))
	responses := make(chan *programmaticpb.OpenResponse)
	receiveErrors := make(chan error, 1)
	joined := make(chan struct{})
	stop := make(chan struct{})
	defer func() { close(stop); fixture.closeOwner(t); <-joined }()
	go func() {
		defer close(joined)
		for {
			response, err := fixture.stream.Recv()
			if err != nil {
				receiveErrors <- err
				return
			}
			select {
			case responses <- response:
			case <-stop:
				return
			}
		}
	}()
	waiting := true
	for waiting {
		select {
		case <-started:
			waiting = false
		case err := <-receiveErrors:
			require.NoError(t, err)
		case response := <-responses:
			event := response.GetEvent()
			require.False(
				t,
				event.HasCompleted() || event.HasFailed() || event.HasCanceled(),
				"logical terminal arrived before observation child",
			)
		}
	}
	query := new(programmaticpb.OpenRequest)
	query.SetOperationId("held-history")
	query.SetRequest(new(programmaticpb.ControllerRequest))
	query.GetRequest().SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
	require.NoError(t, fixture.stream.Send(query))
	found := false
	for !found {
		select {
		case err := <-receiveErrors:
			require.NoError(t, err)
		case response := <-responses:
			event := response.GetEvent()
			if response.GetOperationId() == query.GetOperationId() && event.HasCompleted() {
				// Assert only the submitted user entry is durable before acknowledgement.
				require.Len(t, event.GetCompleted().GetSessionEntries().GetEntries(), 1)
				found = true
			} else {
				require.False(
					t,
					event.HasCompleted() || event.HasFailed() || event.HasCanceled(),
					"conversation completed before observation",
				)
			}
		}
	}
	if cancel {
		cancellation := new(programmaticpb.OpenRequest)
		cancellation.SetOperationId("cancel-held-observation")
		cancellation.SetRequest(new(programmaticpb.ControllerRequest))
		cancellation.GetRequest().
			SetCancel(operationpb.CancelOperation_builder{TargetOperationId: new(request.GetOperationId())}.Build())
		require.NoError(t, fixture.stream.Send(cancellation))
	} else {
		releaseOnce.Do(func() { close(release) })
	}
	var messageEnd *programmaticpb.ModelResponse
	var agentEnd *programmaticpb.AgentSummary
	completed := false
	for !completed {
		select {
		case err := <-receiveErrors:
			require.NoError(t, err)
		case response := <-responses:
			if response.GetOperationId() != request.GetOperationId() {
				continue
			}
			event := response.GetEvent()
			if progress := event.GetProgress().GetAgentEvent(); progress != nil {
				if response := progress.GetModelResponse(); response != nil {
					messageEnd = response
				}
				if summary := progress.GetAgent(); summary != nil {
					agentEnd = summary
				}
			}
			if cancel {
				completed = event.HasFailed() || event.HasCanceled()
				require.False(t, event.HasCompleted())
			} else {
				require.False(t, event.HasFailed() || event.HasCanceled())
				completed = event.HasCompleted()
			}
			if completed && cancel && independent {
				require.NotNil(t, event.GetFailed())
				require.Contains(t, event.GetFailed().GetMessage(), "independent nested provider cause")
			}
			if completed && cancel && !independent {
				require.NotNil(t, event.GetCanceled())
				require.Contains(t, event.GetCanceled().GetMessage(), "prior conversation provider cause")
				require.Contains(t, event.GetCanceled().GetMessage(), context.Canceled.Error())
			}
		}
	}
	// Assert that the application joins its configured child and observes only the successful conversation.
	select {
	case <-childStopped:
	default:
		require.FailNow(t, "nested provider child was not joined before terminal delivery")
	}
	require.Len(t, bodies.snapshot(), childPosition)
	if cancel {
		expected := programmaticpb.ModelOutcome_MODEL_OUTCOME_ABORTED
		if independent {
			expected = programmaticpb.ModelOutcome_MODEL_OUTCOME_FAILED
		}
		require.NotNil(t, messageEnd)
		require.Equal(t, expected, messageEnd.GetOutcome())
		require.Contains(t, messageEnd.GetErrorMessage(), "prior conversation provider cause")
		require.Contains(t, messageEnd.GetErrorMessage(), context.Canceled.Error())
		require.NotNil(t, agentEnd)
		require.Contains(t, agentEnd.GetErrorMessage(), "prior conversation provider cause")
		require.Equal(t, "resp-observed", messageEnd.GetResponseId())
		require.Equal(t, int64(4), messageEnd.GetUsage().GetInputTokens())
		require.Equal(t, int64(11), messageEnd.GetUsage().GetTotalTokens())
		require.Contains(t, messageEnd.GetText(), "Request complete.")
		_, toolErr := os.Stat(toolPath)
		require.True(t, os.IsNotExist(toolErr))
		query.SetOperationId("settled-held-history")
		require.NoError(t, fixture.stream.Send(query))
		for {
			response := <-responses
			if response.GetOperationId() != query.GetOperationId() || !response.GetEvent().HasCompleted() {
				continue
			}
			entries := response.GetEvent().GetCompleted().GetSessionEntries().GetEntries()
			require.Len(t, entries, 2)
			require.Equal(t, messageEnd, entries[1].GetModel())
			break
		}
		stateRequest := new(programmaticpb.OpenRequest)
		stateRequest.SetOperationId("settled-held-state")
		stateRequest.SetRequest(new(programmaticpb.ControllerRequest))
		stateRequest.GetRequest().SetGetRunState(new(programmaticpb.GetRunState))
		require.NoError(t, fixture.stream.Send(stateRequest))
		for {
			response := <-responses
			if response.GetOperationId() != stateRequest.GetOperationId() || !response.GetEvent().HasCompleted() {
				continue
			}
			state := response.GetEvent().GetCompleted().GetRunState()
			require.Equal(t, programmaticpb.RunState_RUN_STATE_IDLE, state.GetState())
			require.Empty(t, state.GetActiveOperationId())
			break
		}
		require.Len(t, bodies.snapshot(), childPosition)
	}
	encoded, readErr := os.ReadFile(os.Getenv(conversationObservationEnvironment))
	require.NoError(t, readErr)
	require.Contains(t, string(encoded), manualOriginalText)
	require.NotContains(t, string(encoded), "nested cancellation checkpoint")
	calls, callsErr := filepath.Glob(os.Getenv(conversationObservationEnvironment) + conversationObservationCallGlob)
	require.NoError(t, callsErr)
	require.Len(t, calls, 1)
}

// TestOverflowRebuiltInput checks observation of committed projection and success-observer messages.
func (s *conversationObservationSuite) TestOverflowRebuiltInput() {
	t := s.T()
	// Arrange short retained history and a single external context rejection.
	t.Setenv(conversationProviderOverflowEnvironment, manualFixtureEnabled)
	policyInputPath := filepath.Join(t.TempDir(), "overflow-policy-input.json")
	t.Setenv(manualFixtureGlyphReferencePolicyInput, policyInputPath)
	directory := buildManualPolicyFixture(t)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	completeProgrammaticRequest(t, fixture, userRequest("overflow-original", manualOriginalText))
	completeProgrammaticRequest(t, fixture, userRequest("overflow-retained", manualRetainedText))
	// Act through provider overflow and the changed successful attempt.
	completeProgrammaticRequest(t, fixture, userRequest("overflow-next", manualNextText))
	// Assert actual provider input and observation contain the same committed replacement and appended message.
	attempts := bodies.snapshot()
	require.Len(t, attempts, 4)
	policyBytes, err := os.ReadFile(policyInputPath)
	require.NoError(t, err)
	policyInput := new(extensionpb.CompactionPolicyInvocation)
	require.NoError(t, protojson.Unmarshal(policyBytes, policyInput))
	require.Equal(t, extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_OVERFLOW, policyInput.GetPurpose())
	require.True(t, policyInput.HasOverflowError())
	require.NotEmpty(t, policyInput.GetOverflowError())
	encoded, err := os.ReadFile(os.Getenv(conversationObservationEnvironment))
	require.NoError(t, err)
	for _, text := range []string{
		"conversation overflow replacement summary", "conversation overflow observer message", manualNextText,
	} {
		require.Contains(t, string(attempts[3]), text)
		require.Contains(t, string(encoded), text)
	}
	require.NotContains(t, string(encoded), manualOriginalText)
	stored := sendProgrammaticOperation(
		t,
		fixture,
		"overflow-original-history",
		func(request *programmaticpb.OpenRequest) {
			programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
		},
	).GetSessionEntries()
	require.Len(t, stored.GetEntries(), 8)
	originals, markers := countManualStoredEntries(stored.GetEntries())
	require.Equal(t, 1, originals)
	require.Equal(t, 1, markers)
	fixture.closeOwner(t)
}

// TestAbsentOwner checks a public conversation request with no policy owner.
func (s *conversationObservationSuite) TestAbsentOwner() {
	t := s.T()
	// Arrange an extension with its policy capability disabled.
	t.Setenv(conversationPolicyDisabledEnvironment, manualFixtureEnabled)
	directory := buildManualPolicyFixture(t)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	// Act through public conversation execution.
	completeProgrammaticRequest(t, fixture, userRequest("absent-observation", manualOriginalText))
	// Assert that, with no policy owner, the application skips observation and preserves the ordinary provider call.
	_, err := os.Stat(os.Getenv(conversationObservationEnvironment))
	require.True(t, os.IsNotExist(err))
	require.Len(t, bodies.snapshot(), 1)
	fixture.closeOwner(t)
}
