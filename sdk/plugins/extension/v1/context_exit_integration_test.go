//go:build integration

package extensionv1

import (
	"context"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestExternalExitCancelsAndJoinsHostReads verifies a real extension exit settles work in both initiator namespaces.
func TestExternalExitCancelsAndJoinsHostReads(t *testing.T) {
	t.Parallel()

	// Arrange: build the public-only extension and block its first nested catalog read at Host release.
	binary := filepath.Join(t.TempDir(), "external")
	build := exec.CommandContext(t.Context(), "go", "build", "-race", "-o", binary, "./cmd/extension")
	build.Dir = filepath.Join("..", "..", "..", "..", "testdata", "external-plugins")
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))
	client, err := Connect(t.Context(), exec.CommandContext(t.Context(), binary))
	require.NoError(t, err)
	t.Cleanup(client.Close)
	connection, err := client.Open(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	controller := gomock.NewController(t)
	host := NewMockHostService(controller)
	read := NewMockHostOperation(controller)
	connection.BindHostService(host)
	running := make(chan struct{})
	releaseStarted := make(chan struct{})
	releaseGate := make(chan struct{})
	release := sync.OnceFunc(func() { close(releaseGate) })
	t.Cleanup(release)
	host.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).Return(read, nil)
	read.EXPECT().Run(gomock.Any()).DoAndReturn(func(ctx context.Context) (*extensionpb.HostCompleted, error) {
		close(running)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	read.EXPECT().Release().Do(func() { close(releaseStarted); <-releaseGate })
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	register := new(extensionpb.HostRequest)
	register.SetRegister(new(extensionpb.RegisterRequest))
	registration, err := connection.Start(ctx, "register", register)
	require.NoError(t, err)
	_, err = registration.Wait(ctx, nil)
	require.NoError(t, err)
	request := new(extensionpb.HostRequest)
	request.SetExecute(
		extensionpb.ExecuteRequest_builder{
			ToolName:      new("external"),
			ArgumentsJson: []byte(`{"mode":"catalogs"}`),
			Context:       testInvocationIdentity(),
		}.Build(),
	)
	execution, err := connection.Start(ctx, "invoke", request)
	require.NoError(t, err)
	awaitHostDisconnectSignal(t, ctx, running, "external nested catalog read")

	// Act: terminate the external process without an orderly operation-stream close.
	client.Close()
	awaitHostDisconnectSignal(t, ctx, releaseStarted, "Host read cancellation and release")
	closed := make(chan error, 1)
	go func() { closed <- connection.Close() }()

	// Assert: connection cleanup cannot finish before its canceled Host operation releases owned work.
	select {
	case closeErr := <-closed:
		t.Fatalf("connection closed before Host release: %v", closeErr)
	default:
	}
	release()
	closeErr := awaitHostDisconnectSignal(t, ctx, closed, "joined connection cleanup")
	require.Error(t, closeErr)
	_, err = execution.Wait(ctx, nil)
	require.Error(t, err)
}
