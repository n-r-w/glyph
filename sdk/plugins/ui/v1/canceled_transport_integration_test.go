//go:build integration

package uiv1

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
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestUICanceledTransport checks that the UI encoder serializes complete canceled diagnostics.
func TestUICanceledTransport(t *testing.T) {
	t.Parallel()
	// Arrange a UI operation with cancellation diagnostics and the production ordered writer.
	ctx, cancel := context.WithCancel(t.Context())
	messages := make(chan *uipb.OpenResponse, 1)
	writer := operation.NewWriter(func(message *uipb.OpenResponse) error {
		encoded, err := proto.Marshal(message)
		if err != nil {
			return err
		}
		received := new(uipb.OpenResponse)
		if err := proto.Unmarshal(encoded, received); err != nil {
			return err
		}
		messages <- received
		return nil
	})
	stopped := make(chan struct{})
	go func() { defer close(stopped); _ = writer.Run(ctx) }()
	defer func() { writer.Close(); cancel(); <-stopped }()
	failures := make(chan error, 1)
	delivery := &uiDelivery{
		ctx:    ctx,
		writer: writer,
		fail:   func(cause error) { failures <- cause },
		mutex:  sync.Mutex{},
		kinds:  nil,
	}
	source := diagnostic.NewCurrentCause(
		context.Canceled,
		errors.New("Unicode: λ\n"+strings.Repeat("full diagnostic ", 512)+"  "),
	)
	// Act through the actual UI terminal encoder and delivery acknowledgement.
	acknowledgement, err := delivery.Terminal("ui-canceled", operation.CanceledWithSource[*uipb.UICompleted](source))
	require.NoError(t, err)
	require.NoError(t, acknowledgement.Wait(t.Context()))
	received := <-messages
	// Assert that the sender transmits the source's complete error text.
	require.True(t, received.GetEvent().HasCanceled())
	require.Equal(t, source.Error(), received.GetEvent().GetCanceled().GetMessage())
	require.Empty(t, failures)
}
