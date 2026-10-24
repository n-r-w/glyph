//go:build integration

package uiv1

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/internal/operation"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestHostOutboundQueueOverflowReportsConnectionFailure verifies SDK producer backpressure ownership.
func TestHostOutboundQueueOverflowReportsConnectionFailure(t *testing.T) {
	t.Parallel()

	// Arrange one SDK Host with a production writer whose transport loop is blocked.
	writer, constructionErr := operation.NewWriter(func(*uiv1.OpenResponse) error { return nil })
	require.NoError(t, constructionErr)
	tracker := operation.NewTracker[*uiv1.HostProgress, *uiv1.HostCompleted]()
	failure := make(chan error, 1)
	host := newHost(t.Context(), writer, tracker, func(err error) { failure <- err }, func() {})
	request := new(uiv1.UIRequest)
	request.SetSubmit(uiv1.SubmitCommand_builder{Text: new("work")}.Build())
	var overflowErr error

	// Act by filling the exact production queue without starting its transport loop.
	for index := range 100 {
		_, overflowErr = host.Start(t.Context(), fmt.Sprintf("operation-%d", index), request)
		if overflowErr != nil {
			break
		}
	}

	// Assert queue overflow reaches the connection failure callback with its source cause.
	require.Error(t, overflowErr)
	require.ErrorIs(t, overflowErr, operation.ErrQueueFull)
	select {
	case reported := <-failure:
		require.ErrorIs(t, reported, operation.ErrQueueFull)
		require.ErrorContains(t, reported, operation.ErrQueueFull.Error())
	default:
		t.Fatal("SDK queue overflow did not report connection failure")
	}
	tracker.Close()
	writer.Close()
	host.closeEvents()
}
