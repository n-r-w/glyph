//go:build integration

package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/n-r-w/glyph/host/internal/controller/programmatic"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	// retrySignalsEnvironment selects the fixture's observation directory.
	retrySignalsEnvironment = "GLYPH_EXTERNAL_SIGNALS"
	// retryHandlerEnvironment selects the fixture's retry handlers.
	retryHandlerEnvironment = "GLYPH_EXTERNAL_RETRY_HANDLER"
	// retryComposeMode enables ordered replacement handlers.
	retryComposeMode = "compose"
	// retryPreserveMode enables a preserving retry handler.
	retryPreserveMode = "1"
	// retryFirstHandlerID identifies the first registered retry handler.
	retryFirstHandlerID = "preserve-retry"
	// retrySecondHandlerID identifies the second registered retry handler.
	retrySecondHandlerID = "compose-retry"
	// retryInvocationFile is the filename format for recorded public retry invocations.
	retryInvocationFile = "retry-%d-%s.json"
	// retryInvokedSignal records invocation of a public retry handler.
	retryInvokedSignal = "retry-handler-invoked"
	// retryProviderCause identifies the source explanation retained in public failures.
	retryProviderCause = "retry acceptance full provider cause"
	// retryProviderError supplies a provider rejection with the retained source explanation.
	retryProviderError = `{"error":{"message":"` + retryProviderCause + `","type":"invalid_request_error"}}`
	// retryUnexpectedTerminalFormat describes an unexpected operation termination.
	retryUnexpectedTerminalFormat = "unexpected terminal: %v"
	// retryContentTypeHeader identifies the provider response media type.
	retryContentTypeHeader = "Content-Type"
	// retryAfterHeader carries the provider's requested delay.
	retryAfterHeader = "Retry-After"
	// retryJSONContentType identifies the provider error representation.
	retryJSONContentType = "application/json"
	// retryStreamContentType identifies the provider event stream.
	retryStreamContentType = "text/event-stream"
	// retryProvidersKey locates the shared provider configuration.
	retryProvidersKey = "providers:\n"
	// retryCodexProviderKey identifies the shared Codex fixture configuration.
	retryCodexProviderKey = "  openai-codex:"
	// retryLocalProviderKey identifies the controlled compatible provider.
	retryLocalProviderKey = "  local:"
	// retryCodexProviderType locates the shared provider type.
	retryCodexProviderType = "type: openai-codex"
	// retryCompatibleProviderFormat configures Responses against the controlled endpoint.
	retryCompatibleProviderFormat = "type: openai-compatible\n    api: responses\n    baseURL: %s"
	// retryCodexDefault selects the shared fixture's default provider.
	retryCodexDefault = "defaultProvider: openai-codex"
	// retryLocalDefault selects the controlled provider for the public request.
	retryLocalDefault = "defaultProvider: local"
)

// retryAcceptanceObservation records public progress and provider arrival times.
type retryAcceptanceObservation struct {
	// arrivals records actual provider attempt arrival times.
	arrivals []time.Time
	// delays records public retry progress delays.
	delays []int64
	// limits records public effective attempt limits.
	limits []int64
	// code records the terminal public failure category.
	code string
	// message records the complete public failure text.
	message string
}

// runRetryAcceptance invokes the public application against controlled provider HTTP responses.
func runRetryAcceptance(
	t *testing.T,
	directory, policy, retryAfter string,
	status, failures int,
) (retryAcceptanceObservation, string) {
	t.Helper()
	var mutex sync.Mutex
	arrivals := make([]time.Time, 0)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		mutex.Lock()
		arrivals = append(arrivals, time.Now())
		attempt := len(arrivals)
		mutex.Unlock()
		if attempt <= failures {
			writer.Header().Set(retryContentTypeHeader, retryJSONContentType)
			if retryAfter != "" {
				writer.Header().Set(retryAfterHeader, retryAfter)
			}
			writer.WriteHeader(status)
			_, _ = writer.Write(
				[]byte(retryProviderError),
			)
			return
		}
		writer.Header().Set(retryContentTypeHeader, retryStreamContentType)
		_, _ = writer.Write([]byte(finalResponseSSE))
	}))
	t.Cleanup(server.Close)
	compatible := strings.Replace(
		strings.SplitN(configuredRequestSettings, retryProvidersKey, 2)[1],
		retryCodexProviderKey,
		retryLocalProviderKey,
		1,
	)
	compatible = strings.Replace(
		compatible,
		retryCodexProviderType,
		fmt.Sprintf(retryCompatibleProviderFormat, server.URL),
		1,
	)
	settings := strings.Replace(configuredRequestSettings, retryCodexDefault, retryLocalDefault, 1)
	settings = strings.Replace(settings, retryProvidersKey, retryProvidersKey+compatible, 1) + policy
	paths := testPaths(t, settings)
	writeProgrammaticCredentials(t, paths)
	signals := t.TempDir()
	t.Setenv(retrySignalsEnvironment, signals)
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	request := userRequest("retry-acceptance", "hello")
	require.NoError(t, fixture.stream.Send(request))
	observation := retryAcceptanceObservation{}
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetOperationId() != request.GetOperationId() || !response.HasEvent() {
			continue
		}
		event := response.GetEvent()
		if retry := event.GetProgress().GetAgentEvent().GetRetryProgress(); retry != nil {
			observation.delays = append(observation.delays, retry.GetDelayMilliseconds())
			observation.limits = append(observation.limits, retry.GetAttemptLimit())
		}
		if failure := event.GetFailed(); failure != nil {
			observation.code, observation.message = failure.GetCode(), failure.GetMessage()
			break
		}
		if event.HasCompleted() {
			break
		}
		require.False(t, event.HasRejected() || event.HasCanceled(), retryUnexpectedTerminalFormat, event)
	}
	fixture.closeOwner(t)
	mutex.Lock()
	observation.arrivals = append([]time.Time(nil), arrivals...)
	mutex.Unlock()
	return observation, signals
}

// TestPublicRetryOverrideComposition checks immutable originals, ordered replacements, and actual scheduled attempts.
func TestPublicRetryOverrideComposition(t *testing.T) {
	// Arrange a non-retryable provider failure and two public replacement handlers.
	directory := buildPublicExtensionFixture(t)
	t.Setenv(retryHandlerEnvironment, retryComposeMode)
	policy := "\nretry:\n  enabled: true\n  maxRetries: 0\n  delays: [0s]\n"
	// Act through Programmatic Control and the real extension runtime.
	observation, signals := runRetryAcceptance(t, directory, policy, "", http.StatusBadRequest, 3)
	// Assert the final handler decision permits exactly two repeats and stops at its effective limit.
	require.Len(t, observation.arrivals, 3)
	assert.Equal(t, programmatic.FailureCodeModelFailed, observation.code)
	assert.Contains(t, observation.message, retryProviderCause)
	assert.Equal(t, []int64{80, 80}, observation.delays)
	assert.Equal(t, []int64{3, 3}, observation.limits)
	for index := 1; index < len(observation.arrivals); index++ {
		assert.GreaterOrEqual(t, observation.arrivals[index].Sub(observation.arrivals[index-1]), 80*time.Millisecond)
	}
	for attempt := 1; attempt <= 3; attempt++ {
		first := readRetryInvocation(t, signals, attempt, retryFirstHandlerID)
		second := readRetryInvocation(t, signals, attempt, retrySecondHandlerID)
		assert.Equal(t, int64(attempt), first.GetCompletedAttempts())
		assert.False(t, first.GetOriginal().GetRetryable())
		assert.False(t, first.GetOriginal().GetRetry())
		assert.Equal(t, int64(1), first.GetOriginal().GetAttemptLimit())
		assert.Zero(t, first.GetOriginal().GetDelayMilliseconds())
		assert.True(t, second.GetCurrent().GetRetryable())
		assert.True(t, second.GetCurrent().GetRetry())
		assert.Equal(t, int64(40), second.GetCurrent().GetDelayMilliseconds())
		assert.Equal(t, int64(4), second.GetCurrent().GetAttemptLimit())
		assert.True(t, proto.Equal(first.GetOriginal(), first.GetCurrent()))
		assert.True(t, proto.Equal(first.GetOriginal(), second.GetOriginal()))
	}
}

// readRetryInvocation reads a public fixture's received retry invocation.
func readRetryInvocation(
	t *testing.T,
	directory string,
	attempt int,
	handler string,
) *extensionpb.RetryHandlerInvocation {
	t.Helper()
	encoded, err := os.ReadFile(filepath.Join(directory, fmt.Sprintf(retryInvocationFile, attempt, handler)))
	require.NoError(t, err)
	invocation := new(extensionpb.RetryHandlerInvocation)
	require.NoError(t, protojson.Unmarshal(encoded, invocation))
	return invocation
}

// TestPublicRetryPolicySchedule checks default, configured, and disabled retry through actual attempts.
func TestPublicRetryPolicySchedule(t *testing.T) {
	// Arrange isolated policies with a provider that always fails transiently.
	directory := buildPublicExtensionFixture(t)
	for _, scenario := range []struct {
		// name identifies the policy.
		name string
		// policy supplies Host configuration.
		policy string
		// delays lists expected delays before repeat attempts.
		delays []int64
		// code is the terminal category.
		code string
	}{
		{name: "default", policy: "", delays: []int64{1000, 2000, 4000}, code: programmatic.FailureCodeRetryExhausted},
		{
			name: "configured", policy: "\nretry:\n  maxRetries: 2\n  delays: [40ms, 90ms]\n",
			delays: []int64{40, 90}, code: programmatic.FailureCodeRetryExhausted,
		},
		{name: "disabled", policy: "\nretry:\n  enabled: false\n", delays: nil, code: programmatic.FailureCodeModelFailed},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			// Arrange the preserving handler for this policy.
			t.Setenv(retryHandlerEnvironment, retryPreserveMode)
			// Act through the public application with preserving handlers enabled.
			observation, signals := runRetryAcceptance(
				t,
				directory,
				scenario.policy,
				"",
				http.StatusServiceUnavailable,
				10,
			)
			// Assert retry counts exclude the initial attempt and provider-arrival intervals meet each delay lower bound.
			require.Len(t, observation.arrivals, len(scenario.delays)+1)
			assert.Equal(t, scenario.delays, observation.delays)
			require.Len(t, observation.limits, len(scenario.delays))
			assert.Equal(t, scenario.code, observation.code)
			assert.Contains(t, observation.message, retryProviderCause)
			for index, delay := range scenario.delays {
				assert.Equal(t, int64(len(scenario.delays)+1), observation.limits[index])
				assert.GreaterOrEqual(
					t,
					observation.arrivals[index+1].Sub(observation.arrivals[index]),
					time.Duration(delay)*time.Millisecond,
				)
			}
			if scenario.name == "disabled" {
				_, err := os.Stat(filepath.Join(signals, retryInvokedSignal))
				assert.True(t, os.IsNotExist(err))
			}
		})
	}
}

// TestPublicProviderRetryAfter checks provider-arrival lower bounds and rejection at default and configured thresholds.
func TestPublicProviderRetryAfter(t *testing.T) {
	// Arrange provider-requested delays around Host's accepted maximum.
	directory := buildPublicExtensionFixture(t)
	for _, scenario := range []struct {
		// name identifies the delay boundary.
		name string
		// policy overrides the default maximum when needed.
		policy string
		// requested is the provider delay in seconds.
		requested string
		// accepted determines whether a second attempt runs.
		accepted bool
		// duration is the expected minimum wait.
		duration time.Duration
		// maximum appears in the rejected public error.
		maximum string
	}{
		{name: "default accepts ten", policy: "", requested: "10", accepted: true, duration: 10 * time.Second, maximum: ""},
		{name: "default rejects sixty", policy: "", requested: "60", accepted: false, duration: 0, maximum: "30s"},
		{
			name: "raised maximum accepts sixty", policy: "\nretry:\n  maxProviderDelay: 60s\n",
			requested: "60", accepted: true, duration: 60 * time.Second, maximum: "",
		},
		{
			name: "lowered maximum rejects ten", policy: "\nretry:\n  maxProviderDelay: 5s\n",
			requested: "10", accepted: false, duration: 0, maximum: "5s",
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			// Arrange the preserving handler for this provider delay.
			t.Setenv(retryHandlerEnvironment, retryPreserveMode)
			// Act through the real provider adapter and public operation.
			observation, _ := runRetryAcceptance(
				t,
				directory,
				scenario.policy,
				scenario.requested,
				http.StatusTooManyRequests,
				1,
			)
			// Assert provider-arrival lower bounds and rejection outcomes independently of progress values.
			if scenario.accepted {
				require.Len(t, observation.arrivals, 2)
				assert.Empty(t, observation.code)
				assert.Equal(t, []int64{scenario.duration.Milliseconds()}, observation.delays)
				assert.GreaterOrEqual(t, observation.arrivals[1].Sub(observation.arrivals[0]), scenario.duration)
			} else {
				require.Len(t, observation.arrivals, 1)
				assert.Empty(t, observation.delays)
				assert.Equal(t, programmatic.FailureCodeRetryDelayExceeded, observation.code)
				assert.Contains(t, observation.message, retryProviderCause)
				requested, err := time.ParseDuration(scenario.requested + "s")
				require.NoError(t, err)
				assert.Contains(t, observation.message, requested.String())
				assert.Contains(t, observation.message, scenario.maximum)
			}
		})
	}
}
