//go:build !integration

package plugin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestSelectionConnectionEventIsAuthoritativeOverCompletion verifies delayed completion cannot carry stale state.
func TestSelectionConnectionEventIsAuthoritativeOverCompletion(t *testing.T) {
	t.Parallel()
	// Arrange a newer connection event and an older operation completion.
	newer := new(uiv1.HostConnectionEvent)
	newer.SetModelSelectionChanged(uiv1.ModelSelectionChanged_builder{Selection: uiv1.ModelSelection_builder{
		ProviderId: new("provider"), ModelId: new("new"),
		ReasoningChoice: new(uiv1.ReasoningChoice_REASONING_CHOICE_HIGH),
	}.Build(), Issues: nil}.Build())
	older := new(uiv1.HostCompleted)
	older.SetModelSelection(uiv1.ModelSelectionChanged_builder{Selection: uiv1.ModelSelection_builder{
		ProviderId: new("provider"), ModelId: new("old"),
		ReasoningChoice: new(uiv1.ReasoningChoice_REASONING_CHOICE_LOW),
	}.Build(), Issues: nil}.Build())

	// Act by decoding the authoritative event and delayed completion.
	eventPayload, err := DecodeConnectionEvent(newer)
	require.NoError(t, err)
	_, handled, err := DecodeCompleted(older)

	// Assert only the connection event carries selection state and completion has no presentation payload.
	require.NoError(t, err)
	assert.False(t, handled)
	assert.Equal(t, PayloadSelection, eventPayload.Kind)
	assert.Equal(t, "new", eventPayload.Selection.ModelID)
}

// TestSelectionCompletionDoesNotRepeatReportedObserverIssue verifies connection issues have one display path.
func TestSelectionCompletionDoesNotRepeatReportedObserverIssue(t *testing.T) {
	t.Parallel()

	// Arrange one committed selection whose observer issue was already sent as a connection event.
	completed := new(uiv1.HostCompleted)
	completed.SetModelSelection(uiv1.ModelSelectionChanged_builder{
		Selection: uiv1.ModelSelection_builder{
			ProviderId: new("provider"), ModelId: new("model"),
			ReasoningChoice: new(uiv1.ReasoningChoice_REASONING_CHOICE_HIGH),
		}.Build(),
		Issues: []*uiv1.OperationIssue{uiv1.OperationIssue_builder{
			Code:        new(uiv1.OperationIssueCode_OPERATION_ISSUE_CODE_OBSERVER_ERROR),
			ExtensionId: new("extension"), HandlerId: new("observer"), Message: new("observer failed"),
		}.Build()},
	}.Build())

	// Act by decoding terminal selection diagnostics.
	_, present, err := DecodeCompleted(completed)

	// Assert completion settles the request without presenting the already reported issue again.
	require.NoError(t, err)
	assert.False(t, present)
}

// TestSelectionCompletionMapsUndeliveredDiagnostic verifies complete post-commit publication text reaches presentation.
func TestSelectionCompletionMapsUndeliveredDiagnostic(t *testing.T) {
	t.Parallel()
	// Arrange one committed selection with a delivery issue that could not use the connection stream.
	completed := new(uiv1.HostCompleted)
	completed.SetModelSelection(uiv1.ModelSelectionChanged_builder{
		Selection: uiv1.ModelSelection_builder{
			ProviderId: new("provider"), ModelId: new("model"),
			ReasoningChoice: new(uiv1.ReasoningChoice_REASONING_CHOICE_HIGH),
		}.Build(),
		Issues: []*uiv1.OperationIssue{uiv1.OperationIssue_builder{
			Code:        new(uiv1.OperationIssueCode_OPERATION_ISSUE_CODE_DELIVERY_FAILED),
			ExtensionId: new(""), HandlerId: new(""), Message: new("complete delivery cause"),
		}.Build()},
	}.Build())

	// Act by decoding terminal selection diagnostics.
	payload, present, err := DecodeCompleted(completed)

	// Assert presentation receives the exact complete cause without selection state.
	require.NoError(t, err)
	require.True(t, present)
	assert.Equal(t, PayloadText, payload.Kind)
	assert.Equal(t, TextError, payload.Text.Kind)
	assert.Equal(t, selectionDeliveryFailureCode, payload.Text.FailureCode)
	assert.Equal(t, "complete delivery cause", payload.Text.Text)
}

// TestMapConnectionEventRetainsAddedMessageState verifies hidden messages reach tree state without transcript lines.
func TestMapConnectionEventRetainsAddedMessageState(t *testing.T) {
	t.Parallel()

	// Arrange one complete hidden-client SessionEntryAdded connection event.
	entry := uiv1.SessionTreeEntry_builder{
		Id: new("message"), ParentId: new("parent"), CreatedTime: timestamppb.Now(), Label: new(""),
		User: nil, Model: nil, ToolResult: nil, Extension: nil, BranchSummary: nil,
		ExtensionMessage: uiv1.ExtensionMessage_builder{
			ExtensionId: new("example"), EntryType: new("note"), Text: new("exact text"),
			Visibility: new(uiv1.ClientVisibility_CLIENT_VISIBILITY_HIDDEN),
		}.Build(),
	}.Build()
	connection := new(uiv1.HostConnectionEvent)
	connection.SetSessionEntryAdded(uiv1.SessionEntryAdded_builder{Entry: entry}.Build())

	// Act by mapping the connection event to presentation state.
	event, err := DecodeConnectionEvent(connection)

	// Assert complete tree data remains and ordinary transcript projection is empty.
	require.NoError(t, err)
	require.Equal(t, TreeEntryAdded, event.Tree.Kind)
	treeEvent := event.Tree
	require.Empty(t, treeEvent.Transcript)
	require.Equal(t, "exact text", treeEvent.AddedEntry.MustGet().ExtensionMessage.MustGet().Text)
	require.Equal(t, ClientVisibilityHidden,
		treeEvent.AddedEntry.MustGet().ExtensionMessage.MustGet().Visibility)
}

// TestOperationMappersRequireSelectedPayloadPresence verifies required retained scalar presence.
func TestOperationMappersRequireSelectedPayloadPresence(t *testing.T) {
	t.Parallel()
	// Arrange authorization for the operation payload mappers to verify required retained scalar presence.

	authorization := new(uiv1.HostProgress)
	authorization.SetAuthorization(new(uiv1.AuthorizationRequest))
	// Act by invoking the operation payload mappers to exercise required retained scalar presence.
	_, authorizationErr := mapHostProgress(authorization)
	// Assert required retained scalar presence.
	require.Error(t, authorizationErr)

	connection := new(uiv1.HostConnectionEvent)
	connection.SetInformation(new(uiv1.Information))
	_, informationErr := DecodeConnectionEvent(connection)
	require.Error(t, informationErr)

	connectionError := new(uiv1.HostConnectionEvent)
	connectionError.SetError(new(uiv1.Error))
	_, errorErr := DecodeConnectionEvent(connectionError)
	require.Error(t, errorErr)
}

// TestOperationMappersPreservePresentEmptyText verifies empty present text remains distinct from absence.
func TestOperationMappersPreservePresentEmptyText(t *testing.T) {
	t.Parallel()
	// Arrange information for DecodeConnectionEvent to verify empty present text remains distinct from absence.

	information := new(uiv1.HostConnectionEvent)
	information.SetInformation(uiv1.Information_builder{Text: new("")}.Build())
	// Act by invoking DecodeConnectionEvent to exercise empty present text remains distinct from absence.
	informationEvent, err := DecodeConnectionEvent(information)
	// Assert empty present text remains distinct from absence.
	require.NoError(t, err)
	assert.Empty(t, informationEvent.Text.Text)

	connectionError := new(uiv1.HostConnectionEvent)
	connectionError.SetError(uiv1.Error_builder{Code: new("INTERNAL"), Text: new("")}.Build())
	_, err = DecodeConnectionEvent(connectionError)
	require.EqualError(t, err, "connection error category and text are required")
}
