//go:build integration

package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
	"github.com/n-r-w/glyph/internal/operation"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// captureConnection records a real output owner's ordered publication.
func captureConnection(t *testing.T, publish func(*Service) error) *uiv1.OpenRequest {
	t.Helper()
	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	messages := make(chan *uiv1.OpenRequest, 1)
	writer := operation.NewWriter(func(request *uiv1.OpenRequest) error { messages <- request; return nil })
	service := New()
	service.writer = writer
	done := make(chan error, 1)
	go func() { done <- writer.Run(ctx) }()
	t.Cleanup(func() {
		defer cancel()
		writer.Close()
		require.NoError(t, <-done)
	})
	require.NoError(t, publish(service))
	return <-messages
}

// TestMapSessionEntryAddedUsesConnectionEvent checks exact hidden-client committed message output.
func TestMapSessionEntryAddedUsesConnectionEvent(t *testing.T) {
	t.Parallel()
	// Arrange one committed extension message with all unrelated entry payloads absent.
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
	// Act by enqueuing the snapshot and waiting for its writer acknowledgement.
	mapped := captureConnection(t, func(output *Service) error {
		wait, err := output.PublishSessionEntry(entry)
		if err != nil {
			return err
		}
		return wait(t.Context())
	})
	// Assert the connection event preserves its payload without an operation identifier.
	require.Empty(t, mapped.GetOperationId())
	message := mapped.GetConnectionEvent().GetSessionEntryAdded().GetEntry()
	require.Equal(t, "message", message.GetId())
	require.Equal(t, "exact text", message.GetExtensionMessage().GetText())
	require.Equal(t, uiv1.ClientVisibility_CLIENT_VISIBILITY_HIDDEN, message.GetExtensionMessage().GetVisibility())
}

// TestMapExtensionIssueUsesTypedConnectionEvent checks identity and complete observer error text.
func TestMapExtensionIssueUsesTypedConnectionEvent(t *testing.T) {
	t.Parallel()
	// Arrange an observer issue with its complete cause.
	issue := lifecycle.Issue{
		ExtensionID: "example",
		HandlerID:   "observer",
		Code:        "OBSERVER_ERROR",
		Err:         errors.New("complete cause"),
	}
	// Act through the real output owner and writer.
	mapped := captureConnection(
		t,
		func(output *Service) error { return output.DeliverExtensionIssue(t.Context(), issue) },
	)
	// Assert each public diagnostic field survives projection.
	require.Empty(t, mapped.GetOperationId())
	wire := mapped.GetConnectionEvent().GetExtensionIssue()
	require.Equal(t, "example", wire.GetExtensionId())
	require.Equal(t, "observer", wire.GetHandlerId())
	require.Equal(t, "OBSERVER_ERROR", wire.GetCode())
	require.Equal(t, "complete cause", wire.GetText())
}

// TestMapExtensionRuntimeFailureUsesConnectionCategory checks classified idle output.
func TestMapExtensionRuntimeFailureUsesConnectionCategory(t *testing.T) {
	t.Parallel()
	// Arrange a safe connection error with a stable category.
	const text = "extension tools unavailable: process exited"
	// Act through typed connection output rather than a mixed frame union.
	mapped := captureConnection(
		t,
		func(output *Service) error { return output.ReportError("EXTENSION_UNAVAILABLE", text) },
	)
	// Assert classification and text remain separate and complete.
	require.Empty(t, mapped.GetOperationId())
	require.Equal(t, "EXTENSION_UNAVAILABLE", mapped.GetConnectionEvent().GetError().GetCode())
	require.Equal(t, text, mapped.GetConnectionEvent().GetError().GetText())
}

// TestDeliveryReportsRuntimeFailure checks the source failure reaches connection output.
func TestDeliveryReportsRuntimeFailure(t *testing.T) {
	t.Parallel()
	// Arrange a process-exit failure for the selected extension identity.
	failure := extension.RuntimeFailure{
		PluginID:  "crashed-plugin",
		Condition: extension.RuntimeUnavailableProcessExited,
	}
	// Act through source failure formatting and the real writer.
	mapped := captureConnection(
		t,
		func(output *Service) error { return output.ReportRuntimeFailure(t.Context(), failure) },
	)
	// Assert the source identity, category, and full message survive.
	require.Equal(t, "EXTENSION_UNAVAILABLE", mapped.GetConnectionEvent().GetError().GetCode())
	require.Equal(
		t,
		"extension crashed-plugin unavailable: extension process exited",
		mapped.GetConnectionEvent().GetError().GetText(),
	)
}
