//go:build integration

package app

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	operationpb "github.com/n-r-w/glyph/pkg/operation/v1"
	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// abortTargetID identifies the conversation canceled by the client.
	abortTargetID = "abort-target"
	// abortOperationID identifies the public cancellation operation.
	abortOperationID = "abort-operation"
	// abortRecoveryID identifies the request admitted after cancellation.
	abortRecoveryID = "abort-recovery"
	// abortRecoveryText distinguishes the later provider input from the canceled input.
	abortRecoveryText = "unique recovery request after public abort"
	// abortRetryDelay gives the client time to abort before the next scheduled attempt.
	abortRetryDelay = time.Hour
	// abortRetryPolicy configures a pending retry through ordinary Host settings.
	abortRetryPolicy = "\nretry:\n  enabled: true\n  maxRetries: 1\n  delays: [1h]\n"
)

// TestPublicAbortReleasesAgent checks active-request and pending-retry abort on one client connection.
func TestPublicAbortReleasesAgent(t *testing.T) {
	t.Parallel()
	// Arrange the real extension and isolated provider endpoints for both cancellation states.
	directory := buildPublicExtensionFixture(t)
	for _, pendingRetry := range []bool{true, false} {
		t.Run(fmt.Sprintf("pending-retry=%t", pendingRetry), func(t *testing.T) {
			t.Parallel()
			var targetAttempts, recoveryAttempts atomic.Int32
			providerStarted := make(chan struct{})
			providerCanceled := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				body, err := io.ReadAll(request.Body)
				assert.NoError(t, err)
				if strings.Contains(string(body), abortRecoveryText) {
					recoveryAttempts.Add(1)
					writer.Header().Set(retryContentTypeHeader, retryStreamContentType)
					_, _ = writer.Write([]byte(finalResponseSSE))
					return
				}
				attempt := targetAttempts.Add(1)
				if pendingRetry {
					writer.Header().Set(retryContentTypeHeader, retryJSONContentType)
					writer.WriteHeader(http.StatusServiceUnavailable)
					_, _ = writer.Write([]byte(retryProviderError))
					return
				}
				if attempt != 1 {
					writer.WriteHeader(http.StatusInternalServerError)
					return
				}
				writer.Header().Set(retryContentTypeHeader, retryStreamContentType)
				writer.WriteHeader(http.StatusOK)
				assert.NoError(t, http.NewResponseController(writer).Flush())
				close(providerStarted)
				<-request.Context().Done()
				close(providerCanceled)
			}))
			t.Cleanup(server.Close)
			settings := strings.Replace(configuredRequestSettings, retryCodexDefault, retryLocalDefault, 1)
			compatible := strings.Replace(
				strings.SplitN(configuredRequestSettings, retryProvidersKey, 2)[1],
				retryCodexProviderKey, retryLocalProviderKey, 1,
			)
			compatible = strings.Replace(compatible, retryCodexProviderType,
				fmt.Sprintf(retryCompatibleProviderFormat, server.URL), 1)
			settings = strings.Replace(settings, retryProvidersKey, retryProvidersKey+compatible, 1) + abortRetryPolicy
			paths := testPaths(t, settings)
			writeProgrammaticCredentials(t, paths)
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			defer func() {
				fixture.closeOwner(t)
				require.Equal(t, int32(1), targetAttempts.Load())
				require.Equal(t, int32(1), recoveryAttempts.Load())
			}()
			require.NoError(t, fixture.stream.Send(userRequest(abortTargetID, "first request")))
			if pendingRetry {
				waitForAbortRetryProgress(t, fixture)
			} else {
				<-providerStarted
			}
			require.Equal(t, int32(1), targetAttempts.Load())
			state := completeProgrammaticRequest(t, fixture, runStateRequest("state-before-abort")).GetRunState()
			require.Equal(t, programmaticpb.RunState_RUN_STATE_RUNNING, state.GetState())
			require.Equal(t, abortTargetID, state.GetActiveOperationId())

			// Act by canceling the accepted target and waiting for its terminal result and cancellation completion.
			require.NoError(t, fixture.stream.Send(cancelRequest(abortOperationID, abortTargetID)))
			waitForAbortCompletion(t, fixture, pendingRetry)
			if !pendingRetry {
				<-providerCanceled
			}

			// Assert public idle and a successful later request before joining the application owner.
			state = completeProgrammaticRequest(t, fixture, runStateRequest("state-after-abort")).GetRunState()
			require.Equal(t, programmaticpb.RunState_RUN_STATE_IDLE, state.GetState())
			require.Empty(t, state.GetActiveOperationId())
			completeProgrammaticRequest(t, fixture, userRequest(abortRecoveryID, abortRecoveryText))
		})
	}
}

// waitForAbortRetryProgress observes the public decision while the next provider attempt is pending.
func waitForAbortRetryProgress(t *testing.T, fixture *programmaticFixture) {
	t.Helper()
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetOperationId() != abortTargetID {
			continue
		}
		event := response.GetEvent()
		require.False(t, event.HasFailed() || event.HasCanceled() || event.HasCompleted() || event.HasRejected(),
			retryUnexpectedTerminalFormat, event)
		if progress := event.GetProgress().GetAgentEvent().GetRetryProgress(); progress != nil {
			require.Equal(t, int64(1), progress.GetCompletedAttempts())
			require.Equal(t, int64(2), progress.GetAttemptLimit())
			require.Equal(t, abortRetryDelay.Milliseconds(), progress.GetDelayMilliseconds())
			require.Contains(t, progress.GetError(), retryProviderCause)
			return
		}
	}
}

// waitForAbortCompletion checks target cancellation before the public cancellation operation completes.
func waitForAbortCompletion(t *testing.T, fixture *programmaticFixture, pendingRetry bool) {
	t.Helper()
	targetCanceled := false
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		event := response.GetEvent()
		switch response.GetOperationId() {
		case abortTargetID:
			require.False(t, event.HasFailed() || event.HasCompleted() || event.HasRejected(),
				retryUnexpectedTerminalFormat, event)
			if event.HasCanceled() {
				require.False(t, targetCanceled)
				targetCanceled = true
				if pendingRetry {
					require.Contains(t, event.GetCanceled().GetMessage(), retryProviderCause)
				}
			}
		case abortOperationID:
			require.False(t, event.HasFailed() || event.HasCanceled() || event.HasRejected(),
				retryUnexpectedTerminalFormat, event)
			if event.HasCompleted() {
				require.True(t, targetCanceled)
				require.Equal(t, operationpb.TerminalState_TERMINAL_STATE_CANCELED,
					event.GetCompleted().GetCancel().GetTargetState())
				return
			}
		}
	}
}
