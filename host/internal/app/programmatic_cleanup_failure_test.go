//go:build integration

package app

import (
	"errors"
	"net/http"
	"os"
	"sync/atomic"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const programmaticCleanupProviderMarker = "private model provider-context extension-json"

// TestOwnerClosurePersistenceFailurePreservesContext verifies Programmatic cleanup returns storage details.
func (testSuite *ProgrammaticAppSuite) TestOwnerClosurePersistenceFailurePreservesContext() {
	t := testSuite.T()

	// Arrange one named real session and a provider request that reports cancellation completion.
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	requests := new(atomic.Int32)
	providerStarted := make(chan struct{}, 1)
	providerCanceled := make(chan struct{}, 1)
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).AnyTimes().DoAndReturn(
		func(request *http.Request) (*http.Response, error) {
			if requests.Add(1) != 1 {
				return nil, errors.New("unexpected dependent provider request")
			}
			providerStarted <- struct{}{}
			<-request.Context().Done()
			providerCanceled <- struct{}{}
			return nil, errors.Join(request.Context().Err(), errors.New(programmaticCleanupProviderMarker))
		},
	)
	previousTransport := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	fixture := startProgrammaticFixture(t, paths)
	t.Cleanup(func() {
		fixture.cancel()
		_ = fixture.connection.Close()
	})
	named := sendProgrammaticCommand(t, fixture, "name-cleanup-failure", func(request *programmaticv1.OpenRequest) {
		programmaticRequest(
			request,
		).SetSetSessionName(programmaticv1.SetSessionName_builder{Name: new("cleanup durable")}.Build())
	}).GetSessionInfo().GetInfo()
	privateUserText := "private cleanup user"
	require.NoError(t, fixture.stream.Send(userRequest("cleanup-failure", privateUserText)))
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetOperationId() == "cleanup-failure" && response.GetEvent().HasRunning() {
			break
		}
	}
	<-providerStarted
	require.NoError(t, os.Chmod(named.GetStoragePath(), 0o400))
	t.Cleanup(func() { _ = os.Chmod(named.GetStoragePath(), 0o600) })

	// Act by closing the owner while terminal aborted-model persistence targets the read-only active file.
	require.NoError(t, fixture.stream.CloseSend())
	runErr := <-fixture.result
	require.NoError(t, runErr)
	fixture.assertClosed(t)
	require.NoError(t, os.Chmod(named.GetStoragePath(), 0o600))
	stored, err := os.ReadFile(named.GetStoragePath())
	require.NoError(t, err)
	// Assert cancellation completion and durable user state.
	select {
	case <-providerCanceled:
	default:
		require.Fail(t, "process returned before provider cancellation completed")
	}
	assert.Equal(t, int32(1), requests.Load())
	assert.Contains(t, string(stored), privateUserText)
	assert.NotContains(t, string(stored), `"type":"model"`)
}
