//go:build integration

package output

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestPublishConnectionEventsUsesOrderedWriter verifies committed messages and issues have no operation identifier.
func TestPublishConnectionEventsUsesOrderedWriter(t *testing.T) {
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
	service := New()
	unbind := service.BindWriter(writer)
	defer unbind()
	entry := session.Entry{
		ID: "message", ParentID: mo.Some("parent"), CreatedAt: time.Unix(1, 0).UTC(),
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		BranchSummary: mo.None[session.BranchSummaryEntry](),
		ExtensionMessage: mo.Some(session.ExtensionMessage{
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

	// Act by publishing one typed nonterminal extension issue on the same writer.
	require.NoError(t, service.DeliverExtensionIssue(t.Context(), lifecycle.Issue{
		ExtensionID: "example", HandlerID: "observer", Code: "OBSERVER_ERROR", Err: errors.New("complete cause"),
	}))
	issueResponse := <-delivered

	// Assert issue identity, code, and complete text cross the public contract.
	require.Empty(t, issueResponse.GetOperationId())
	issue := issueResponse.GetConnectionEvent().GetExtensionIssue()
	require.Equal(t, "example", issue.GetExtensionId())
	require.Equal(t, "observer", issue.GetHandlerId())
	require.Equal(t, "OBSERVER_ERROR", issue.GetCode())
	require.Equal(t, "complete cause", issue.GetText())

	// Assert writer cleanup completes.
	writer.Close()
	require.NoError(t, <-done)
}
