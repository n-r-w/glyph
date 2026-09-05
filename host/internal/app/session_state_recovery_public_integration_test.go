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
	_, timestampErr := time.Parse(time.RFC3339Nano, recovered.CreatedTime)
	require.NoError(t, timestampErr)
	assert.True(t, firstReport.PayloadExact)
	assert.True(t, activeReport.PayloadExact)
	assert.True(t, recovered.PayloadExact)
	assert.True(t, activeReport.Appended)
	assert.False(t, recovered.Appended)
	assert.Equal(t, 1, recovered.EntryCount)
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
	// Appended reports whether the invocation created the recovered entry.
	Appended bool `json:"appended"`
}

// decodeSessionStateReport decodes the external tool result captured by the provider fixture.
func decodeSessionStateReport(t *testing.T, value string) sessionStateReportValue {
	t.Helper()
	var report sessionStateReportValue
	require.NoError(t, json.Unmarshal([]byte(value), &report))
	return report
}
