//go:build integration

package app

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestProgrammaticJoinedPersistenceFailure verifies joined persistence and unrelated public failures.
func (testSuite *ProgrammaticAppSuite) TestProgrammaticJoinedPersistenceFailure() {
	t := testSuite.T()
	for _, persistenceFails := range []bool{true, false} {
		// Arrange a blocked provider failure after the first durable user entry.
		paths := testPaths(t, codexSettings(""))
		writeProgrammaticCredentials(t, paths)
		started, release := make(chan struct{}), make(chan struct{})
		transport := NewMockHTTPRoundTripper(gomock.NewController(t))
		transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(*http.Request) (*http.Response, error) {
			close(started)
			<-release
			return nil, errors.New(persistenceProviderCause)
		})
		previous := http.DefaultTransport
		http.DefaultTransport = transport
		t.Cleanup(func() { http.DefaultTransport = previous })
		fixture := startProgrammaticFixture(t, paths)
		defer fixture.closeOwner(t)
		named := sendProgrammaticCommand(t, fixture, "name-failure", func(request *programmaticv1.OpenRequest) {
			programmaticRequest(
				request,
			).SetSetSessionName(programmaticv1.SetSessionName_builder{Name: new("failure")}.Build())
		}).GetSessionInfo().GetInfo()

		// Act by failing the model append only in the persistence case, with an independent provider error in both cases.
		require.NoError(t, fixture.stream.Send(userRequest("run-failure", "hello")))
		<-started
		path := named.GetStoragePath()
		if persistenceFails {
			require.NoError(t, os.Chmod(path, 0o400))
		}
		close(release)
		for {
			response, err := fixture.stream.Recv()
			require.NoError(t, err)
			event := response.GetEvent()
			if !event.HasFailed() {
				continue
			}
			failure := event.GetFailed()

			// Assert a source-classified joined failure wins INTERNAL without losing either diagnostic.
			expectedCode := "INTERNAL"
			if persistenceFails {
				expectedCode = "PERSISTENCE_UNAVAILABLE"
				require.Contains(t, strings.ToLower(failure.GetMessage()), "permission")
			}
			require.NoError(t, os.Chmod(path, 0o600))
			require.Equal(t, expectedCode, failure.GetCode())
			require.Contains(t, failure.GetMessage(), persistenceProviderCause)
			break
		}
		http.DefaultTransport = previous
	}
}
