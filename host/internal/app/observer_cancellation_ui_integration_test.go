//go:build integration

package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

const (
	// observerCancellationBehavior selects the public UI recovery scenario.
	observerCancellationBehavior = "observer-cancellation"
	// observerStartedSuffix identifies the pipe that proves the AgentStart observer is executing.
	observerStartedSuffix = ".observer-started"
	// observerRecoveryReceipt records that the second run completed on the retained connection.
	observerRecoveryReceipt = "one terminal result; empty history; later run completed"
)

// TestUIObserverCancellationReleasesRun verifies recovery through the production UI controller, Core, and output.
func TestUIObserverCancellationReleasesRun(t *testing.T) {
	// Arrange a public extension observer blocked before its append and a UI client with targeted cancellation.
	t.Setenv(externalLifecycleEnvironment, "1")
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	trace := filepath.Join(t.TempDir(), "recovery")
	started := trace + observerStartedSuffix
	require.NoError(t, syscall.Mkfifo(started, 0o600))
	requests := new(atomic.Int32)
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			// The UI reads this byte only after starting the run. Cancellation cannot precede observer entry.
			pipe, err := os.OpenFile(started, os.O_WRONLY, 0)
			if err != nil {
				return nil, err
			}
			_, err = pipe.Write([]byte{1})
			if err != nil {
				return nil, err
			}
			<-request.Context().Done()
			require.NoError(t, pipe.Close())
			return nil, request.Context().Err()
		}
		return &http.Response{
			StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(finalResponseSSE)),
			Header: make(http.Header), Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0,
			ContentLength: 0, TransferEncoding: nil, Close: false, Uncompressed: false,
			Trailer: nil, Request: nil, TLS: nil,
		}, nil
	}).AnyTimes()
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })

	// Act through the real Host assembly and selected UI process on one retained stream.
	runSummaryControlUI(t, paths, buildPublicExtensionFixture(t), trace, observerCancellationBehavior)

	// Assert the canceled observer made no dependent request, and the later observer and Core both completed.
	require.Equal(t, int32(3), requests.Load())
	receipt, err := os.ReadFile(trace)
	require.NoError(t, err)
	require.Equal(t, observerRecoveryReceipt, string(receipt))
}

// runObserverCancellationUIFixture cancels a blocked observer and verifies later admission on the same UI connection.
func runObserverCancellationUIFixture(t *testing.T, ctx context.Context, host *uisdk.Host) error {
	t.Helper()
	// Arrange a ready UI connection and the first user request.
	if err := waitForIdle(ctx, host); err != nil {
		return err
	}
	request := new(uiv1.UIRequest)
	request.SetSubmit(uiv1.SubmitCommand_builder{Text: new("first request")}.Build())
	first, err := host.Start(ctx, "blocked-observer", request)
	if err != nil {
		return err
	}
	pipe, err := os.Open(os.Getenv(appUITraceEnvironment) + observerStartedSuffix)
	if err != nil {
		return err
	}
	signal := make([]byte, 1)
	_, readErr := io.ReadFull(pipe, signal)
	closeErr := pipe.Close()
	require.NoError(t, readErr)
	require.NoError(t, closeErr)

	// Act by canceling the accepted run after its AgentStart observer reached the provider.
	cancellation, err := host.Cancel(ctx, "cancel-observer", "blocked-observer")
	if err != nil {
		return err
	}
	terminals := 0
	observe := func(notification *uisdk.Notification) {
		if notification.OperationID() == "blocked-observer" &&
			(notification.Kind() == uisdk.NotificationCompleted || notification.Kind() == uisdk.NotificationFailed) {
			terminals++
		}
	}
	_, runErr := waitUIOperation(ctx, host, "blocked-observer", first, observe)
	require.Error(t, runErr)
	_, err = cancellation.Wait(ctx)
	require.NoError(t, err)

	// Assert an empty committed tree and a later successful run without reconnecting.
	treeRequest := new(uiv1.UIRequest)
	treeRequest.SetGetSessionTree(new(uiv1.GetSessionTreeCommand))
	tree, err := host.Start(ctx, "tree-after-cancel", treeRequest)
	if err != nil {
		return err
	}
	treeResult, err := waitUIOperation(ctx, host, "tree-after-cancel", tree, observe)
	require.NoError(t, err)
	require.Empty(t, treeResult.GetSessionTree().GetTree().GetEntries())
	request.SetSubmit(uiv1.SubmitCommand_builder{Text: new("second request")}.Build())
	second, err := host.Start(ctx, "later-run", request)
	if err != nil {
		return err
	}
	_, err = waitUIOperation(ctx, host, "later-run", second, observe)
	require.NoError(t, err)
	require.Equal(t, 1, terminals)
	if err := os.WriteFile(os.Getenv(appUITraceEnvironment), []byte(observerRecoveryReceipt), 0o600); err != nil {
		return err
	}
	return host.Close(ctx)
}
