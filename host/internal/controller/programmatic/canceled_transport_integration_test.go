//go:build integration

package programmatic

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/n-r-w/glyph/internal/errtree/diagnostic"
	"github.com/n-r-w/glyph/internal/operation"
	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestProgrammaticCanceledTransport checks that the controller serializes complete canceled diagnostics.
func TestProgrammaticCanceledTransport(t *testing.T) {
	t.Parallel()
	// Arrange a diagnosed controller operation and the production ordered writer.
	ctx, cancel := context.WithCancel(t.Context())
	messages := make(chan *programmaticpb.OpenResponse, 1)
	writer := operation.NewWriter(func(message *programmaticpb.OpenResponse) error {
		encoded, err := proto.Marshal(message)
		if err != nil {
			return err
		}
		received := new(programmaticpb.OpenResponse)
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
	delivery := &streamDelivery{
		context:  ctx,
		writer:   writer,
		registry: newTargetRegistry(),
		fail:     func(cause error) { failures <- cause },
	}
	source := diagnostic.NewCurrentCause(
		context.Canceled,
		errors.New("Unicode: λ\n"+strings.Repeat("full diagnostic ", 512)+"  "),
	)
	// Act through the actual controller terminal encoder and delivery acknowledgement.
	acknowledgement, err := delivery.Terminal("controller-canceled", operation.CanceledWithSource[Response](source))
	require.NoError(t, err)
	require.NoError(t, acknowledgement.Wait(t.Context()))
	received := <-messages
	// Assert that the sender transmits the canceled source's complete error text.
	require.True(t, received.GetEvent().HasCanceled())
	require.Equal(t, source.Error(), received.GetEvent().GetCanceled().GetMessage())
	require.Empty(t, failures)
}
