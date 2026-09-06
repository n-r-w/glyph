//go:build !integration

package programmatic

import (
	"context"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestPublishSessionEntryUsesOrderedConnectionWriter verifies exact committed messages have no operation identifier.
func TestPublishSessionEntryUsesOrderedConnectionWriter(t *testing.T) {
	t.Parallel()

	// Arrange one active writer and exact hidden-client extension message.
	delivered := make(chan *programmaticv1.OpenResponse, 1)
	writer := operation.NewWriter(func(response *programmaticv1.OpenResponse) error {
		delivered <- response
		return nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- writer.Run(ctx) }()
	service := New(t.Context(), nil)
	service.writerMutex.Lock()
	service.writer = writer
	service.writerMutex.Unlock()
	entry := SessionTreeEntry{
		ID: "message", ParentID: mo.Some("parent"), CreatedAt: time.Unix(1, 0).UTC(), Label: "",
		Kind: SessionTreeEntryExtensionMessage, User: mo.None[model.Message](), Model: mo.None[ModelResponse](),
		EstimatedCost: mo.None[session.EstimatedCost](), ToolResult: mo.None[ToolResult](),
		Extension: mo.None[ExtensionEntry](), BranchSummary: mo.None[BranchSummary](),
		ExtensionMessage: mo.Some(ExtensionMessage{
			ExtensionID: "example", EntryType: "note", Text: "exact text", Visibility: session.ClientVisibilityHidden,
		}),
	}

	// Act by publishing outside an operation lifecycle.
	wait, err := service.PublishSessionEntry(entry)
	require.NoError(t, err)
	require.NoError(t, wait(t.Context()))
	response := <-delivered

	// Assert exact data and empty operation identity cross the same writer.
	require.Empty(t, response.GetOperationId())
	message := response.GetConnectionEvent().GetSessionEntryAdded().GetEntry().GetExtensionMessage()
	require.Equal(t, "exact text", message.GetText())
	require.Equal(t, programmaticv1.ClientVisibility_CLIENT_VISIBILITY_HIDDEN, message.GetVisibility())

	// Assert writer cleanup completes.
	writer.Close()
	require.NoError(t, <-done)
}
