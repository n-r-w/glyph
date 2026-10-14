//go:build integration

package extensionv1

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/n-r-w/glyph/internal/errtree/diagnostic"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/proto"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestExtensionCanceledTransport checks that the SDK serializes cancellation diagnostics after settlement.
func TestExtensionCanceledTransport(t *testing.T) {
	t.Parallel()
	// Arrange a canceled extension operation and an acknowledged stream terminal.
	controller := gomock.NewController(t)
	service := NewMockService(controller)
	target := NewMockExecuteOperation(controller)
	stream := NewMockExtensionService_OpenServer[extensionpb.OpenRequest, extensionpb.OpenResponse](controller)
	text := "received failure diagnostic\nUnicode: λ\n" + strings.Repeat("detail ", 512) + "  "
	source := diagnostic.NewCurrentCause(context.Canceled, errors.New(text))
	terminal := make(chan struct{})
	signalTerminal := sync.OnceFunc(func() { close(terminal) })
	defer signalTerminal()
	service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(target, nil)
	target.EXPECT().Run(gomock.Any(), gomock.Any()).Return(nil, source)
	target.EXPECT().Release()
	stream.EXPECT().Context().AnyTimes().Return(t.Context())
	request := new(extensionpb.OpenRequest)
	request.SetOperationId("rich-canceled")
	request.SetRequest(new(extensionpb.HostRequest))
	request.GetRequest().SetExecute(extensionpb.ExecuteRequest_builder{
		Context: extensionpb.ExtensionContext_builder{
			ContextId:         new("binding"),
			ExtensionId:       new("extension"),
			RuntimeInstanceId: new("runtime"),
			SessionId:         new("session"),
			Cwd:               new("/project"),
		}.Build(),
		ToolName:      new("echo"),
		ArgumentsJson: []byte("{}"),
	}.Build())
	gomock.InOrder(
		stream.EXPECT().Recv().Return(request, nil),
		stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenRequest, error) {
			<-terminal
			return nil, io.EOF
		}),
	)
	var observed *extensionpb.OpenResponse
	stream.EXPECT().Send(gomock.Any()).AnyTimes().DoAndReturn(func(response *extensionpb.OpenResponse) error {
		if response.GetEvent().HasCanceled() {
			encoded, err := proto.Marshal(response)
			if err != nil {
				signalTerminal()
				return err
			}
			observed = new(extensionpb.OpenResponse)
			err = proto.Unmarshal(encoded, observed)
			signalTerminal()
			return err
		}
		return nil
	})
	server := newServer(service)
	server.ready = true
	// Act through the production SDK server, outcome classifier and writer.
	err := server.Open(stream)
	// Assert that the encoder transmits the source's full error text
	// before the Owner removes the acknowledged source report.
	require.NoError(t, err)
	require.NotNil(t, observed)
	require.True(t, observed.GetEvent().HasCanceled())
	require.Equal(t, source.Error(), observed.GetEvent().GetCanceled().GetMessage())
}
