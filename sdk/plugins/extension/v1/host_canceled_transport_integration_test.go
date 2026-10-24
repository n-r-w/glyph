//go:build integration

package extensionv1

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/n-r-w/glyph/internal/errtree/diagnostic"
	"github.com/n-r-w/glyph/internal/operation"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestHostCanceledTransport checks that the extension Host encoder serializes canceled diagnostics.
func TestHostCanceledTransport(t *testing.T) {
	t.Parallel()
	// Arrange a Host operation with cancellation diagnostics and the production ordered writer.
	ctx, cancel := context.WithCancelCause(t.Context())
	messages := make(chan *extensionpb.OpenRequest, 1)
	writer, constructionErr := operation.NewWriter(func(message *extensionpb.OpenRequest) error {
		encoded, err := proto.Marshal(message)
		if err != nil {
			return err
		}
		received := new(extensionpb.OpenRequest)
		if err := proto.Unmarshal(encoded, received); err != nil {
			return err
		}
		messages <- received
		return nil
	})
	require.NoError(t, constructionErr)
	stopped := make(chan struct{})
	go func() { defer close(stopped); _ = writer.Run(ctx) }()
	defer func() { writer.Close(); cancel(nil); <-stopped }()
	connection := &Connection{
		ctx: ctx, cancel: cancel, stream: nil, writer: writer, tracker: nil, hostOwner: nil, hostService: nil,
		mutex: sync.Mutex{}, kinds: nil, err: nil, completionErr: nil, completionFailures: nil,
		writerDone: nil, receiveResolved: nil, receiveDone: nil, closeOnce: sync.Once{},
	}
	delivery := &hostDelivery{connection: connection}
	source := diagnostic.NewCurrentCause(
		context.Canceled,
		errors.New("Unicode: λ\n"+strings.Repeat("full diagnostic ", 512)+"  "),
	)
	// Act through the actual Host terminal encoder and delivery acknowledgement.
	acknowledgement, err := delivery.Terminal(
		"host-canceled",
		operation.CanceledWithSource[*extensionpb.HostCompleted](source),
	)
	require.NoError(t, err)
	require.NoError(t, acknowledgement.Wait(t.Context()))
	received := <-messages
	// Assert that the sender transmits the source's complete error text.
	require.True(t, received.GetEvent().HasCanceled())
	require.Equal(t, source.Error(), received.GetEvent().GetCanceled().GetMessage())
}
