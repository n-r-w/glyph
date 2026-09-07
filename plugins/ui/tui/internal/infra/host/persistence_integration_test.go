//go:build integration

package host

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
	tuiinput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"
	"github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// TestSDKPersistenceCleanupUsesCause verifies both incoming error paths retain semantics and complete diagnostics.
func TestSDKPersistenceCleanupUsesCause(t *testing.T) {
	t.Parallel()
	for _, connection := range []bool{false, true} {
		for _, test := range []struct {
			// code is the category supplied by the Host.
			code string
			// text is the complete diagnostic independent of that category.
			text string
			// clears states whether provisional history must be discarded.
			clears bool
		}{
			{
				code: "PERSISTENCE_UNAVAILABLE",
				text: "write rejected: " + strings.Repeat("diagnostic ", 7000) + "final cause", clears: true,
			},
			{code: "INTERNAL", text: "session persistence failed upstream", clears: false},
			{code: "UNKNOWN", text: "session persistence failed from an unknown source", clears: false},
		} {
			name := test.code + "/operation"
			if connection {
				name = test.code + "/connection"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				// Arrange the real SDK, input controller, and application owner with isolated display and runtime ports.
				mocks := gomock.NewController(t)
				runtime := presentation.NewMockRuntime(mocks)
				display := presentation.NewMockDisplay(mocks)
				adapter := New()
				application := presentation.New(adapter, display, runtime)
				input := plugininput.New(application)
				adapter.BindInput(input)
				var snapshot presentation.Snapshot
				display.EXPECT().
					Publish(gomock.Any()).
					AnyTimes().
					Do(func(value presentation.Snapshot) { snapshot = value })
				runtime.EXPECT().Open().Return(nil)
				runtime.EXPECT().Close().Return(nil)
				projected := make(chan presentation.Snapshot, 1)
				failures := make(chan error, 1)
				runtime.EXPECT().Run(gomock.Any()).DoAndReturn(func(ctx context.Context) presentation.RuntimeResult {
					application.Key(tuiinput.Key{Code: 0, Text: "durable user", Mod: 0})
					work := application.Key(tuiinput.Key{Code: tuiinput.KeyEnter, Text: "", Mod: 0})
					application.Complete(work.Execute())
					for range 2 {
						notification, err := adapter.ReceiveNotification(ctx)
						if err == nil {
							err = input.Notify(notification)
						}
						if err != nil {
							failures <- err
							return presentation.RuntimeResult{ProgramExited: false, Err: err}
						}
					}
					projected <- snapshot
					<-ctx.Done()
					return presentation.RuntimeResult{ProgramExited: false, Err: nil}
				})
				stream, err := uisdk.TestClient(t, adapter).Open(t.Context())
				require.NoError(t, err)
				sendInitialization(t, stream)
				request, err := stream.Recv()
				require.NoError(t, err)
				id := request.GetOperationId()
				accepted := new(uiv1.HostEvent)
				accepted.SetAccepted(new(operationv1.Accepted))
				sendHostEvent(t, stream, id, accepted)
				running := new(uiv1.HostEvent)
				running.SetRunning(new(operationv1.Running))
				sendHostEvent(t, stream, id, running)
				delta := new(uiv1.AgentEvent)
				delta.SetType(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA)
				delta.SetRunId("run")
				delta.SetModelContent(uiv1.ModelContent_builder{
					Type: new(uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA), Position: new(int64(0)),
					Text: new("unconfirmed model"), Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
				}.Build())
				progress := new(uiv1.HostProgress)
				progress.SetAgentEvent(delta)
				progressEvent := new(uiv1.HostEvent)
				progressEvent.SetProgress(progress)
				sendHostEvent(t, stream, id, progressEvent)

				// Act with the same category and diagnostic through operation and connection inputs.
				if connection {
					event := new(uiv1.HostConnectionEvent)
					event.SetError(uiv1.Error_builder{Code: new(test.code), Text: new(test.text)}.Build())
					require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
						OperationId: nil, Request: nil, Event: nil, ConnectionEvent: event, Close: nil,
					}.Build()))
				} else {
					failed := new(uiv1.HostEvent)
					failed.SetFailed(operationv1.Failed_builder{Code: new(test.code), Message: new(test.text)}.Build())
					sendHostEvent(t, stream, id, failed)
				}

				// Assert cleanup follows source category while the user transcript and full text survive.
				select {
				case final := <-projected:
					if test.clears {
						require.Empty(t, final.Body.ActiveModel)
					} else {
						require.Contains(t, final.Body.ActiveModel, 0)
						require.Equal(t, "unconfirmed model", final.Body.ActiveModel[0].Text.OrEmpty())
					}
					require.Equal(t, "durable user", final.Body.Transcript[0].Text.OrEmpty())
					require.Equal(t, test.text, final.Body.Transcript[len(final.Body.Transcript)-1].Text.OrEmpty())
				case err := <-failures:
					require.NoError(t, err)
				}
				require.NoError(t, stream.CloseSend())
				_, err = stream.Recv()
				require.ErrorIs(t, err, io.EOF)
			})
		}
	}
}
