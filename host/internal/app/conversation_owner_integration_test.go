//go:build integration

package app

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/encoding/protojson"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// conversationToolResponseSSE is a fixture with visible content, private reasoning and a tool request.
const conversationToolResponseSSE = `data: {"type":"response.completed","response":{"id":"resp-observed",` +
	`"model":"gpt-reported","status":"completed",` +
	`"usage":{"input_tokens":7,"output_tokens":4,"total_tokens":11,` +
	`"input_tokens_details":{"cached_tokens":2,"cache_write_tokens":1},` +
	`"output_tokens_details":{"reasoning_tokens":3}},"output":[` +
	`{"id":"reason-observed","type":"reasoning","encrypted_content":"private-observation-replay",` +
	`"summary":[{"type":"summary_text","text":"visible observation reasoning"}]},` +
	`{"id":"message-observed","type":"message","role":"assistant","status":"completed",` +
	`"content":[{"type":"output_text","text":"Request complete.","annotations":[],"logprobs":[]}]},` +
	`{"id":"function-observed","type":"function_call","call_id":"call-observed",` +
	`"name":"reference_observation_tool","arguments":"{}"}]}}` + "\n\n" + "data: [DONE]\n\n"

// conversationLengthResponseSSE is a provider response fixture for a successful length outcome.
const conversationLengthResponseSSE = `data: {"type":"response.incomplete","response":{"id":"length-response",` +
	`"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[]}}` + "\n\n" +
	"data: [DONE]\n\n"

// TestLengthObservation checks that a length-limited attempt reaches the policy owner.
func (s *conversationObservationSuite) TestLengthObservation() {
	t := s.T()
	// Arrange a provider length outcome without reported usage.
	t.Setenv(conversationProviderResponseEnvironment, conversationResponseLength)
	directory := buildManualPolicyFixture(t)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	// Act through the public conversation path.
	completeProgrammaticRequest(t, fixture, userRequest("length-observation", manualOriginalText))
	// Assert the owner receives the successful length outcome despite absent usage.
	encoded, err := os.ReadFile(os.Getenv(conversationObservationEnvironment))
	require.NoError(t, err)
	observed := new(extensionpb.CompletedConversationObservation)
	require.NoError(t, protojson.Unmarshal(encoded, observed))
	require.Equal(
		t,
		extensionpb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_LENGTH,
		observed.GetResponse().GetOutcome(),
	)
	require.False(t, observed.GetResponse().HasUsage())
	require.Len(t, bodies.snapshot(), 1)
	fixture.closeOwner(t)
}

// TestToolOutcomeObservationFailure checks that the application skips requested tools when observation fails.
func (s *conversationObservationSuite) TestToolOutcomeObservationFailure() {
	t := s.T()
	// Arrange a public tool with an observable execution artifact.
	toolPath := filepath.Join(t.TempDir(), "executed-tool.json")
	t.Setenv(conversationToolEnvironment, toolPath)
	// Act through the failed owner observation in assertObserverFailure.
	s.assertObserverFailure(false)
	// Assert the tool was not executed and private replay remains outside public observation.
	_, err := os.Stat(toolPath)
	require.True(t, os.IsNotExist(err))
	encoded, err := os.ReadFile(os.Getenv(conversationObservationEnvironment))
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-observation-replay")
	require.Contains(t, string(encoded), "visible observation reasoning")
	observed := new(extensionpb.CompletedConversationObservation)
	require.NoError(t, protojson.Unmarshal(encoded, observed))
	require.Positive(t, observed.GetResponsePrivatePayloadBytes())
	require.Equal(
		t,
		extensionpb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_TOOL_USE,
		observed.GetResponse().GetOutcome(),
	)
	require.Len(t, observed.GetTools(), 1)
	require.Equal(t, conversationReferenceToolName, observed.GetTools()[0].GetDescriptor().GetName())
}

// TestPresentZeroUsage checks the distinction between zero usage and absent usage in a successful observation.
func (s *conversationObservationSuite) TestPresentZeroUsage() {
	t := s.T()
	// Arrange a successful provider response with normalized usage present and all values zero.
	t.Setenv(conversationProviderResponseEnvironment, conversationResponseZero)
	directory := buildManualPolicyFixture(t)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	// Act through the public conversation path.
	completeProgrammaticRequest(t, fixture, userRequest("zero-usage-observation", manualOriginalText))
	// Assert presence and all normalized zero buckets through the public observation.
	encoded, err := os.ReadFile(os.Getenv(conversationObservationEnvironment))
	require.NoError(t, err)
	observed := new(extensionpb.CompletedConversationObservation)
	require.NoError(t, protojson.Unmarshal(encoded, observed))
	require.True(t, observed.GetResponse().HasUsage())
	require.Equal(t, int64(0), observed.GetResponse().GetUsage().GetInputTokens())
	require.Equal(t, int64(0), observed.GetResponse().GetUsage().GetOutputTokens())
	require.Equal(t, int64(0), observed.GetResponse().GetUsage().GetCachedInputTokens())
	require.Equal(t, int64(0), observed.GetResponse().GetUsage().GetCacheWriteTokens())
	require.Equal(t, int64(0), observed.GetResponse().GetUsage().GetReasoningTokens())
	require.Equal(t, int64(0), observed.GetResponse().GetUsage().GetTotalTokens())
	require.Len(t, bodies.snapshot(), 1)
	fixture.closeOwner(t)
}

// TestCapturedOwnerDisappears checks that losing the captured policy-owner runtime differs from having no policy owner.
func (s *conversationObservationSuite) TestCapturedOwnerDisappears() {
	t := s.T()
	// Arrange a real owner process that disappears during the external provider attempt.
	pidPath := filepath.Join(t.TempDir(), "owner.pid")
	t.Setenv(conversationPolicyPIDEnvironment, pidPath)
	directory := buildManualPolicyFixture(t)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	attempts := 0
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		attempts++
		if _, err := io.Copy(io.Discard, request.Body); err != nil {
			return nil, err
		}
		encoded, err := os.ReadFile(pidPath)
		if err != nil {
			return nil, err
		}
		pid, err := strconv.Atoi(string(encoded))
		if err != nil {
			return nil, err
		}
		process, err := os.FindProcess(pid)
		if err != nil {
			return nil, err
		}
		if err := process.Kill(); err != nil {
			return nil, err
		}
		if err := process.Release(); err != nil {
			return nil, err
		}
		return &http.Response{
			Status:     "",
			StatusCode: http.StatusOK,
			Proto:      "",
			ProtoMajor: 0,
			ProtoMinor: 0,
			Header: make(
				http.Header,
			),
			Body:             io.NopCloser(bytes.NewBufferString(conversationResponseSSE)),
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
	// Act through public conversation admission before the captured owner is killed.
	request := userRequest("disappeared-owner", manualOriginalText)
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
	// Assert that the application retains the successful provider response and usage with no replacement call.
	require.NotNil(t, terminal.GetFailed())
	require.Equal(t, conversationExtensionFailed, terminal.GetFailed().GetCode())
	stored := sendProgrammaticOperation(
		t,
		fixture,
		"disappeared-owner-history",
		func(request *programmaticpb.OpenRequest) {
			programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
		},
	).GetSessionEntries()
	encoded, err := protojson.Marshal(stored)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "Request complete.")
	require.Contains(t, string(encoded), "resp-observed")
	require.Contains(t, string(encoded), `"inputTokens":"4"`)
	require.Equal(t, 1, attempts)
	fixture.closeOwner(t)
}

// TestMultipleOwners checks that the application rejects ambiguous policy ownership before provider work.
func (s *conversationObservationSuite) TestMultipleOwners() {
	t := s.T()
	// Arrange two registered public policy processes.
	directory := buildManualPolicyFixture(t)
	executable, err := os.ReadFile(filepath.Join(directory, manualFixtureReferencePolicy))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "another-policy"), executable, 0o700))
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	// Act through public conversation admission.
	request := userRequest("ambiguous-conversation", manualOriginalText)
	require.NoError(t, fixture.stream.Send(request))
	var terminal *programmaticpb.HostEvent
	for terminal == nil {
		response, receiveErr := fixture.stream.Recv()
		require.NoError(t, receiveErr)
		if response.GetOperationId() != request.GetOperationId() {
			continue
		}
		event := response.GetEvent()
		if event.HasCompleted() || event.HasFailed() || event.HasCanceled() {
			terminal = event
		}
	}
	// Assert ambiguity remains an explicit failure without dispatching the provider.
	require.NotNil(t, terminal.GetFailed())
	require.Contains(t, terminal.GetFailed().GetMessage(), "multiple compaction policy owners are registered")
	require.Empty(t, bodies.snapshot())
	fixture.closeOwner(t)
}

// TestAcknowledgedNestedFailure checks that the application reports failed nested work
// despite owner acknowledgement.
func (s *conversationObservationSuite) TestAcknowledgedNestedFailure() {
	t := s.T()
	// Arrange a successful conversation and a rejected observer-owned configured request.
	t.Setenv(conversationModelEnvironment, manualFixtureEnabled)
	t.Setenv(conversationAcknowledgeFailureEnvironment, manualFixtureEnabled)
	directory := buildManualPolicyFixture(t)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
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
			body, status := conversationResponseSSE, http.StatusOK
			if position == 2 {
				body = `{"error":{"message":"acknowledged nested provider cause","type":"invalid_request_error"}}`
				status = http.StatusBadRequest
			}
			return &http.Response{
				Status: "", StatusCode: status, Proto: "", ProtoMajor: 0, ProtoMinor: 0,
				Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(body)), ContentLength: 0,
				TransferEncoding: nil, Close: false, Uncompressed: false, Trailer: nil, Request: nil, TLS: nil,
			}, nil
		})
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	// Act through public conversation admission and the owner's successful acknowledgement.
	request := userRequest("acknowledged-nested-failure", manualOriginalText)
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
	// Assert that the application retains the nested cause and original conversation response after logical failure.
	require.NotNil(t, terminal.GetFailed())
	require.Equal(t, conversationExtensionFailed, terminal.GetFailed().GetCode())
	require.Contains(t, terminal.GetFailed().GetMessage(), "acknowledged nested provider cause")
	stored := sendProgrammaticOperation(
		t,
		fixture,
		"acknowledged-failure-history",
		func(request *programmaticpb.OpenRequest) {
			programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
		},
	).GetSessionEntries()
	encoded, err := protojson.Marshal(stored)
	require.NoError(t, err)
	require.Len(t, stored.GetEntries(), 2)
	require.Contains(t, string(encoded), "Request complete.")
	require.Contains(t, string(encoded), "acknowledged nested provider cause")
	require.Contains(t, string(encoded), `"inputTokens":"4"`)
	require.Len(t, bodies.snapshot(), 2)
	calls, callsErr := filepath.Glob(os.Getenv(conversationObservationEnvironment) + conversationObservationCallGlob)
	require.NoError(t, callsErr)
	require.Len(t, calls, 1)
	fixture.closeOwner(t)
}
