//go:build integration

package app

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestPublicTerminalObserverFailureAfterProviderCancel checks failed summaries retain aborted provider history.
func TestPublicTerminalObserverFailureAfterProviderCancel(t *testing.T) {
	for _, test := range []struct {
		name string
		kind extensionv1.HandlerKind
	}{
		{name: "MESSAGE_END", kind: extensionv1.HandlerKind_HANDLER_KIND_MESSAGE_END},
		{name: "TURN_END", kind: extensionv1.HandlerKind_HANDLER_KIND_TURN_END},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange a real terminal observer and a provider cancellation before the observer fails.
			t.Setenv(ordinaryObserverEnvironment, test.kind.String())
			t.Setenv(externalSignalsEnvironment, t.TempDir())
			paths := testPaths(t, codexSettings(""))
			writeProgrammaticCredentials(t, paths)
			transport := NewMockHTTPRoundTripper(gomock.NewController(t))
			attempts := 0
			started := make(chan struct{})
			transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
				attempts++
				if attempts == 1 {
					close(started)
					<-request.Context().Done()
					return nil, request.Context().Err()
				}
				return &http.Response{
					StatusCode:       http.StatusOK,
					Body:             io.NopCloser(strings.NewReader(finalResponseSSE)),
					Header:           make(http.Header),
					Status:           "",
					Proto:            "",
					ProtoMajor:       0,
					ProtoMinor:       0,
					ContentLength:    0,
					TransferEncoding: nil,
					Close:            false,
					Uncompressed:     false,
					Trailer:          nil,
					Request:          nil,
					TLS:              nil,
				}, nil
			}).AnyTimes()
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			fixture := startProgrammaticFixtureWithExtension(t, paths, buildPublicExtensionFixture(t))
			defer fixture.closeOwner(t)
			request := userRequest("terminal-cancel", "first request")
			require.NoError(t, fixture.stream.Send(request))
			<-started
			require.NoError(t, fixture.stream.Send(cancelRequest("terminal-owner-cancel", "terminal-cancel")))
			var summary *programmaticpb.AgentSummary
			failedRun, completedCancel := false, false
			// Act through public progress to exactly one failed operation completion.
			for !failedRun || !completedCancel {
				response, err := fixture.stream.Recv()
				require.NoError(t, err)
				if response.GetOperationId() == "terminal-owner-cancel" {
					if response.GetEvent().HasCompleted() {
						completedCancel = true
					}
					continue
				}
				if response.GetOperationId() != request.GetOperationId() {
					continue
				}
				event := response.GetEvent()
				if value := event.GetProgress().GetAgentEvent().GetAgent(); value != nil {
					summary = value
				}
				if failed := event.GetFailed(); failed != nil {
					require.Equal(t, "EXTENSION_FAILED", failed.GetFailure().GetCode())
					require.Contains(t, failed.GetFailure().GetMessage(), ordinaryObserverCause)
					require.False(t, failedRun)
					failedRun = true
				}
				require.False(t, event.HasCompleted() || event.HasCanceled())
			}
			// Assert the new summary is Failed while committed provider history remains Aborted and the next request succeeds.
			require.NotNil(t, summary)
			require.Equal(t, programmaticpb.RunOutcome_RUN_OUTCOME_FAILED, summary.GetOutcome())
			messages := activeProgrammaticMessages(t, fixture)
			require.Len(t, messages, 2)
			require.Equal(t, programmaticpb.ModelOutcome_MODEL_OUTCOME_ABORTED, messages[1].GetModel().GetOutcome())
			completeProgrammaticRequest(t, fixture, userRequest("terminal-healthy", "second request"))
			require.Equal(t, 2, attempts)
		})
	}
}
