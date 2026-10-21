//go:build integration

package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	operationpb "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

const (
	// abortUIBehavior selects ordinary conversation retry cancellation in the UI fixture.
	abortUIBehavior = "abort-retry-idle"
	// abortUIReceipt records completed public cancellation, idle, and recovery assertions.
	abortUIReceipt = "ordinary retry canceled; idle observed; recovery completed"
)

// TestUIAbortPendingRetryReleasesAgent checks ordinary retry cancellation through the retained UI connection.
func TestUIAbortPendingRetryReleasesAgent(t *testing.T) {
	t.Parallel()
	// Arrange an ordinary conversation rejection followed by a distinct successful recovery request.
	var targetAttempts, recoveryAttempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		assert.NoError(t, err)
		if strings.Contains(string(body), abortRecoveryText) {
			recoveryAttempts.Add(1)
			writer.Header().Set(retryContentTypeHeader, retryStreamContentType)
			_, _ = writer.Write([]byte(finalResponseSSE))
			return
		}
		targetAttempts.Add(1)
		writer.Header().Set(retryContentTypeHeader, retryJSONContentType)
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte(retryProviderError))
	}))
	t.Cleanup(server.Close)
	settings := strings.Replace(configuredRequestSettings, retryCodexDefault, retryLocalDefault, 1)
	compatible := strings.Replace(strings.SplitN(configuredRequestSettings, retryProvidersKey, 2)[1],
		retryCodexProviderKey, retryLocalProviderKey, 1)
	compatible = strings.Replace(compatible, retryCodexProviderType,
		fmt.Sprintf(retryCompatibleProviderFormat, server.URL), 1)
	settings = strings.Replace(settings, retryProvidersKey, retryProvidersKey+compatible, 1) + abortRetryPolicy
	paths := testPaths(t, settings)
	writeProgrammaticCredentials(t, paths)
	trace := filepath.Join(t.TempDir(), "abort-ui-receipt")

	// Act through the production UI application and public SDK in a real child process.
	runSummaryControlUI(t, paths, buildPublicExtensionFixture(t), trace, abortUIBehavior)

	// Assert one canceled target attempt and one recovery attempt after the application has joined.
	require.Equal(t, int32(1), targetAttempts.Load())
	require.Equal(t, int32(1), recoveryAttempts.Load())
	receipt, err := os.ReadFile(trace)
	require.NoError(t, err)
	require.Equal(t, abortUIReceipt, string(receipt))
}

// runAbortRetryUIFixture observes retry progress, cancels its conversation, and submits after public idle.
func runAbortRetryUIFixture(ctx context.Context, host *uisdk.Host) error {
	// Arrange a ready connection and an ordinary submitted conversation.
	if err := waitForIdle(ctx, host); err != nil {
		return err
	}
	request := new(uiv1.UIRequest)
	request.SetSubmit(uiv1.SubmitCommand_builder{Text: new("first request")}.Build())
	target, err := host.Start(ctx, abortTargetID, request)
	if err != nil {
		return err
	}
	if err := waitForUIAbortRetryProgress(ctx, host); err != nil {
		return err
	}

	// Act through public cancellation of the ordinary conversation target.
	cancellation, err := host.Cancel(ctx, abortOperationID, abortTargetID)
	if err != nil {
		return err
	}
	idleObserved := false
	targetTerminals := 0
	var orderingErr error
	observe := func(notification *uisdk.Notification) {
		if connection := notification.ConnectionEvent(); connection != nil &&
			connection.GetAvailabilityChanged().GetAvailability() == uiv1.Availability_AVAILABILITY_IDLE {
			idleObserved = true
		}
		if notification.OperationID() == abortTargetID &&
			(notification.Kind() == uisdk.NotificationCompleted || notification.Kind() == uisdk.NotificationFailed) {
			targetTerminals++
		}
		if notification.OperationID() == abortOperationID && notification.Kind() == uisdk.NotificationCompleted &&
			targetTerminals != 1 {
			orderingErr = fmt.Errorf(
				"cancellation completed after %d target terminal notifications, want 1",
				targetTerminals,
			)
		}
	}
	_, targetErr := waitUIOperation(ctx, host, abortTargetID, target, observe)
	if targetErr == nil {
		return errors.New("target completed successfully, want context.Canceled")
	}
	if !errors.Is(targetErr, context.Canceled) {
		return fmt.Errorf("target result must match context.Canceled, got: %w", targetErr)
	}
	if !strings.Contains(targetErr.Error(), retryProviderCause) {
		return fmt.Errorf("target cancellation must retain provider cause %q, got: %w", retryProviderCause, targetErr)
	}
	completed, err := cancellation.Wait(ctx)
	if err != nil {
		return err
	}
	if completed.GetTargetState() != operationpb.TerminalState_TERMINAL_STATE_CANCELED {
		return fmt.Errorf("cancellation target state = %s, want CANCELED", completed.GetTargetState())
	}
	if !idleObserved {
		if err := waitForIdle(ctx, host); err != nil {
			return err
		}
	}

	// Assert that public idle admits another ordinary request without reconnecting.
	request.SetSubmit(uiv1.SubmitCommand_builder{Text: new(abortRecoveryText)}.Build())
	recovery, err := host.Start(ctx, abortRecoveryID, request)
	if err != nil {
		return err
	}
	if _, err = waitUIOperation(ctx, host, abortRecoveryID, recovery, observe); err != nil {
		return fmt.Errorf("recovery request: %w", err)
	}
	if orderingErr != nil {
		return orderingErr
	}
	if targetTerminals != 1 {
		return fmt.Errorf("received %d target terminal notifications, want 1", targetTerminals)
	}
	if err := os.WriteFile(os.Getenv(appUITraceEnvironment), []byte(abortUIReceipt), 0o600); err != nil {
		return err
	}
	return host.Close(ctx)
}

// waitForUIAbortRetryProgress checks the public retry decision before canceling its next pending attempt.
func waitForUIAbortRetryProgress(ctx context.Context, host *uisdk.Host) error {
	for {
		notification, err := host.Receive(ctx)
		if err != nil {
			return err
		}
		if notification.OperationID() != abortTargetID {
			continue
		}
		if notification.Kind() == uisdk.NotificationCompleted {
			return errors.New("target completed before retry progress")
		}
		if notification.Kind() == uisdk.NotificationFailed {
			return fmt.Errorf("target failed before retry progress: %w", notification.OperationError())
		}
		if progress := notification.Progress().GetAgentEvent().GetRetryProgress(); progress != nil {
			if progress.GetCompletedAttempts() != 1 {
				return fmt.Errorf("retry completed attempts = %d, want 1", progress.GetCompletedAttempts())
			}
			if progress.GetAttemptLimit() != 2 {
				return fmt.Errorf("retry attempt limit = %d, want 2", progress.GetAttemptLimit())
			}
			if progress.GetDelayMilliseconds() != abortRetryDelay.Milliseconds() {
				return fmt.Errorf("retry delay = %d ms, want %d ms",
					progress.GetDelayMilliseconds(), abortRetryDelay.Milliseconds())
			}
			if !strings.Contains(progress.GetError(), retryProviderCause) {
				return fmt.Errorf(
					"retry progress must retain provider cause %q, got: %s",
					retryProviderCause,
					progress.GetError(),
				)
			}
			return nil
		}
	}
}
