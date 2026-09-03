//go:build integration

package runtime

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// TestRuntimeConnectionFailureStartsNoCancellationOperation verifies failed connections own operation cleanup.
func TestRuntimeConnectionFailureStartsNoCancellationOperation(t *testing.T) {
	t.Parallel()

	// Arrange: start a direct peer that fails the connection during Execute.
	runtime := startHelperRuntime(t, "failure-before-accepted")
	_, err := runtime.Register(t.Context())
	require.NoError(t, err)

	// Act: receive the connection failure for the second runtime operation.
	_, err = runtime.Execute(
		t.Context(), "read", []byte(`{"path":"notes.txt"}`), discardProgress,
	)

	// Assert: return the failure without allocating a third identifier for cancellation.
	require.Error(t, err)
	assert.Equal(t, uint64(2), runtime.nextOperationID.Load())
	requireRuntimeStopped(t, runtime)
}

// TestRuntimeKeepsExecuteRejectionAndFailureAvailable verifies valid terminal errors do not stop Execute use.
func TestRuntimeKeepsExecuteRejectionAndFailureAvailable(t *testing.T) {
	t.Parallel()

	// Arrange: define one valid rejection and one valid accepted-operation failure.
	testCases := map[string]struct {
		mode       string
		code       string
		sourceText string
		rejection  bool
	}{
		"rejection": {
			mode: "execute-rejection", code: "BUSY",
			sourceText: "complete Execute rejection source", rejection: true,
		},
		"failure": {
			mode: "execute-failure", code: "INTERNAL",
			sourceText: "complete Execute failure source", rejection: false,
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runtime := startHelperRuntime(t, testCase.mode)
			_, err := runtime.Register(t.Context())
			require.NoError(t, err)

			// Act: receive the selected terminal error, then execute later work.
			_, terminalErr := runtime.Execute(
				t.Context(), "read", []byte(`{"path":"notes.txt"}`), discardProgress,
			)
			later, laterErr := runtime.Execute(
				t.Context(), "read", []byte(`{"path":"notes.txt"}`), discardProgress,
			)

			// Assert: preserve the wrapper, category, full Host context, source text, and runtime availability.
			assertRuntimeTerminalError(t, terminalErr, testCase.rejection, testCase.code, testCase.sourceText)
			require.ErrorContains(t, terminalErr, `execute extension tool "read"`)
			require.NotErrorIs(t, terminalErr, extensionruntime.ErrExtensionUnavailable)
			require.NoError(t, laterErr)
			assert.Equal(t, tool.TextContents("done"), later.Contents)
		})
	}
}

// TestRuntimeKeepsHandleRejectionAndFailureAvailable verifies valid terminal errors do not stop Handle use.
func TestRuntimeKeepsHandleRejectionAndFailureAvailable(t *testing.T) {
	t.Parallel()

	// Arrange: define one valid rejection and one valid accepted-operation failure.
	testCases := map[string]struct {
		mode       string
		code       string
		sourceText string
		rejection  bool
	}{
		"rejection": {
			mode: "handle-rejection", code: "BUSY",
			sourceText: "complete Handle rejection source", rejection: true,
		},
		"failure": {
			mode: "handle-failure", code: "INTERNAL",
			sourceText: "complete Handle failure source", rejection: false,
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runtime := startHelperRuntime(t, testCase.mode)
			_, err := runtime.Register(t.Context())
			require.NoError(t, err)
			request := validSessionTreeHandlerRequest()

			// Act: receive the selected terminal error, then handle later work.
			_, terminalErr := runtime.Handle(t.Context(), "observer", request)
			later, laterErr := runtime.Handle(t.Context(), "observer", request)

			// Assert: preserve the wrapper, category, full Host context, source text, and runtime availability.
			assertRuntimeTerminalError(t, terminalErr, testCase.rejection, testCase.code, testCase.sourceText)
			require.ErrorContains(t, terminalErr, `handle extension handler "observer"`)
			require.NotErrorIs(t, terminalErr, extensionruntime.ErrExtensionUnavailable)
			require.NoError(t, laterErr)
			assert.True(t, later.Observer.IsPresent())
		})
	}
}

// TestRuntimeStopsAfterRegistrationRejectionOrFailure verifies startup terminal errors retain details and stop runtime.
func TestRuntimeStopsAfterRegistrationRejectionOrFailure(t *testing.T) {
	t.Parallel()

	// Arrange: define rejected and failed registration startup operations.
	testCases := map[string]struct {
		mode       string
		code       string
		sourceText string
		rejection  bool
	}{
		"rejection": {
			mode: "register-rejection", code: "INVALID_ARGUMENT",
			sourceText: "complete Register rejection source", rejection: true,
		},
		"failure": {
			mode: "register-failure", code: "INTERNAL",
			sourceText: "complete Register failure source", rejection: false,
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runtime := startHelperRuntime(t, testCase.mode)

			// Act: run the rejected or failed startup operation.
			_, terminalErr := runtime.Register(t.Context())

			// Assert: preserve the exact wrapper, code, context, and text, then stop startup.
			assertRuntimeTerminalError(t, terminalErr, testCase.rejection, testCase.code, testCase.sourceText)
			require.ErrorContains(t, terminalErr, "register extension")
			requireRuntimeStopped(t, runtime)
		})
	}
}

// assertRuntimeTerminalError checks one SDK terminal wrapper without classifying runtime unavailability.
func assertRuntimeTerminalError(
	t *testing.T,
	terminalErr error,
	rejection bool,
	expectedCode string,
	expectedSourceText string,
) {
	t.Helper()
	require.Error(t, terminalErr)
	if rejection {
		var rejectionErr *extensionsdk.RejectionError
		require.ErrorAs(t, terminalErr, &rejectionErr)
		assert.Equal(t, expectedCode, rejectionErr.Code())
		require.EqualError(t, rejectionErr, expectedSourceText)
	} else {
		var failureErr *extensionsdk.FailureError
		require.ErrorAs(t, terminalErr, &failureErr)
		assert.Equal(t, expectedCode, failureErr.Code())
		require.EqualError(t, failureErr, expectedSourceText)
	}
	require.ErrorContains(t, terminalErr, expectedSourceText)
}

// validSessionTreeHandlerRequest returns one valid observer invocation.
func validSessionTreeHandlerRequest() sessiontree.HandlerRequest {
	return sessiontree.HandlerRequest{
		Request: mo.None[sessiontree.RequestHandlerInvocation](),
		Result:  mo.None[sessiontree.ResultHandlerInvocation](),
		Observer: mo.Some(sessiontree.TreeObserverInvocation{
			SessionID: "session", TargetEntryID: "target",
			PrecedingActiveLeafID: mo.None[string](), NavigationDestinationID: mo.None[string](),
			CommittedActiveLeafID: mo.None[string](), CreatedSummary: mo.None[session.Entry](),
		}),
	}
}
