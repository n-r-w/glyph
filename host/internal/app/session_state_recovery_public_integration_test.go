//go:build integration

package app

import (
	"encoding/json/v2"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestPublicExtensionRecoversHiddenStateAfterProcessRestart verifies public-only durable recovery after memory loss.
//
//nolint:paralleltest // This test replaces process-global provider HTTP transport.
func TestPublicExtensionRecoversHiddenStateAfterProcessRestart(t *testing.T) {
	// Arrange one external binary and deterministic provider tool requests.
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	directory := buildPublicExtensionFixture(t)
	var count atomic.Int32
	var body atomic.Value
	var extensionMode atomic.Value
	extensionMode.Store("ordinary")
	previous := http.DefaultTransport
	http.DefaultTransport = catalogueProviderTransport(t, &count, &body, func() string {
		return extensionMode.Load().(string)
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	first := startProgrammaticFixtureWithExtension(t, paths, directory)
	completeProgrammaticRequest(t, first, userRequest("prefix", "create branch prefix"))
	extensionMode.Store("session-state")
	completeProgrammaticRequest(t, first, userRequest("append", "store checkpoint"))
	firstReport := decodeSessionStateReport(t, externalToolOutput(t, body.Load().([]byte)))
	detailed := sendProgrammaticOperation(t, first, "entries", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	var detailedMessage *programmaticpb.ExtensionMessage
	for _, entry := range detailed {
		if entry.GetExtensionMessage() != nil {
			detailedMessage = entry.GetExtensionMessage()
		}
	}
	require.NotNil(t, detailedMessage)
	assert.Equal(t, "exact\nrestart message", detailedMessage.GetText())
	assert.Equal(t, programmaticpb.ClientVisibility_CLIENT_VISIBILITY_HIDDEN, detailedMessage.GetVisibility())
	messages := sendProgrammaticOperation(t, first, "messages", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetMessages(new(programmaticpb.GetMessages))
	}).GetMessages()
	assert.NotContains(t, messages.String(), "exact\\nrestart message")
	info := sendProgrammaticOperation(t, first, "info", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetSessionInfo(new(programmaticpb.GetSessionInfo))
	}).GetSessionInfo().GetInfo()
	sessionID := info.GetId()
	tree := sendProgrammaticOperation(t, first, "tree", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetSessionTree(new(programmaticpb.GetSessionTree))
	}).GetSessionTree().GetTree()
	require.NotEmpty(t, tree.GetEntries())
	var branchUserID string
	for _, entry := range tree.GetEntries() {
		if entry.GetUser() != nil {
			branchUserID = entry.GetId()
		}
	}
	require.NotEmpty(t, branchUserID)
	sendProgrammaticOperation(t, first, "navigate", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetNavigateSessionTree(programmaticpb.NavigateSessionTree_builder{
			TargetEntryId: new(branchUserID),
			SummaryMode:   new(programmaticpb.SummaryMode_SUMMARY_MODE_NO_SUMMARY), CustomFocus: nil,
		}.Build())
	})
	completeProgrammaticRequest(t, first, userRequest("active-branch", "store active checkpoint"))
	activeReport := decodeSessionStateReport(t, externalToolOutput(t, body.Load().([]byte)))
	first.closeOwner(t)

	// Act after a complete Host and extension process restart and public session resume.
	restarted := startProgrammaticFixtureWithExtension(t, paths, directory)
	defer restarted.closeOwner(t)
	sendProgrammaticOperation(t, restarted, "resume", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetResumeSession(
			programmaticpb.ResumeSession_builder{SessionId: new(sessionID)}.Build(),
		)
	})
	completeProgrammaticRequest(t, restarted, userRequest("recover", "recover checkpoint"))
	recovered := decodeSessionStateReport(t, externalToolOutput(t, body.Load().([]byte)))

	// Assert the fresh extension recovered exact bytes and durable identity through public APIs.
	assert.NotEmpty(t, firstReport.EntryID)
	assert.NotEqual(t, firstReport.EntryID, activeReport.EntryID)
	assert.Equal(t, activeReport.EntryID, recovered.EntryID)
	assert.Equal(t, activeReport.ParentID, recovered.ParentID)
	assert.Equal(t, activeReport.ExtensionID, recovered.ExtensionID)
	assert.Equal(t, activeReport.EntryType, recovered.EntryType)
	assert.Equal(t, activeReport.CreatedTime, recovered.CreatedTime)
	assert.NotEmpty(t, recovered.ParentID)
	assert.Equal(t, "external", recovered.ExtensionID)
	assert.Equal(t, "restart-checkpoint", recovered.EntryType)
	assert.NotEmpty(t, recovered.MessageID)
	assert.Equal(t, activeReport.MessageID, recovered.MessageID)
	assert.Equal(t, activeReport.MessageParentID, recovered.MessageParentID)
	assert.Equal(t, activeReport.MessageCreatedTime, recovered.MessageCreatedTime)
	assert.Equal(t, "external", recovered.MessageExtensionID)
	assert.Equal(t, "restart-message", recovered.MessageEntryType)
	assert.Equal(t, "exact\nrestart message", recovered.MessageText)
	assert.Equal(t, "CLIENT_VISIBILITY_HIDDEN", recovered.MessageVisibility)
	_, timestampErr := time.Parse(time.RFC3339Nano, recovered.CreatedTime)
	require.NoError(t, timestampErr)
	assert.True(t, firstReport.PayloadExact)
	assert.True(t, activeReport.PayloadExact)
	assert.True(t, recovered.PayloadExact)
	assert.True(t, activeReport.Appended)
	assert.False(t, recovered.Appended)
	assert.Equal(t, 2, recovered.EntryCount)
	assert.Equal(t, int32(8), count.Load(), "unexpected provider call count")
}

// sessionStateReportValue is the external fixture's public-only recovery evidence.
type sessionStateReportValue struct {
	// EntryID identifies the durable Host-owned entry.
	EntryID string `json:"entry_id"`
	// ParentID preserves the stored parent identifier.
	ParentID string `json:"parent_id"`
	// ExtensionID identifies the stored extension owner.
	ExtensionID string `json:"extension_id"`
	// EntryType identifies the stored extension-defined kind.
	EntryType string `json:"entry_type"`
	// CreatedTime contains the stored timestamp at nanosecond precision.
	CreatedTime string `json:"created_time"`
	// PayloadExact reports byte equality checked in the external process.
	PayloadExact bool `json:"payload_exact"`
	// EntryCount reports caller-filtered entries.
	EntryCount int `json:"entry_count"`
	// Appended reports whether the invocation created the recovered entries.
	Appended bool `json:"appended"`
	// MessageID identifies the durable model-visible message.
	MessageID string `json:"message_id"`
	// MessageParentID preserves the stored message parent.
	MessageParentID string `json:"message_parent_id"`
	// MessageExtensionID identifies the stored message owner.
	MessageExtensionID string `json:"message_extension_id"`
	// MessageEntryType identifies the extension-defined message kind.
	MessageEntryType string `json:"message_entry_type"`
	// MessageCreatedTime contains the stored message timestamp.
	MessageCreatedTime string `json:"message_created_time"`
	// MessageText contains exact recovered text.
	MessageText string `json:"message_text"`
	// MessageVisibility contains the public closed visibility name.
	MessageVisibility string `json:"message_visibility"`
}

// decodeSessionStateReport decodes the external tool result captured by the provider fixture.
func decodeSessionStateReport(t *testing.T, value string) sessionStateReportValue {
	t.Helper()
	var report sessionStateReportValue
	require.NoError(t, json.Unmarshal([]byte(value), &report))
	return report
}
