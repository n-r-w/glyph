//go:build integration

// Package sessions owns session history and compaction state.
package sessions

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// captureCompactionSuppliesPreparedHistorySuite owns an independent scenario context.
type captureCompactionSuppliesPreparedHistorySuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestCaptureCompactionSuppliesPreparedHistory checks detached original entries and prepared history in the snapshot.
func TestCaptureCompactionSuppliesPreparedHistory(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Parallel()
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &captureCompactionSuppliesPreparedHistorySuite{Suite: suite.Suite{}})
}

// TestScenario checks detached original and projected input.
func (s *captureCompactionSuppliesPreparedHistorySuite) TestScenario() {
	t := s.T()
	// Arrange a branch whose original prefix has a persisted replacement.
	// entries contains the original branch and its persisted replacement marker.
	entries := []session.Entry{
		newCompactionUserEntry("old", mo.None[string](), "original"),
		newCompactionUserEntry("kept", mo.Some("old"), "retained"),
		newCompactionMarkerEntry("marker", "kept", "replacement", "kept"),
	}
	// service and identity identify the active session owner and session identity.
	service, identity := newCompactionHistoryTestService(t, entries, "marker")
	service.contextHistory = storedCompactedHistoryFromEntries(entries)

	// Act by capturing the active session once.
	// captured contains detached original entries and prepared model history.
	captured := service.CaptureCompaction()

	// Assert that original entries and prepared replacement history are both available.
	require.Equal(t, identity, captured.Identity)
	require.Equal(t, mo.Some("marker"), captured.ActiveLeafID)
	require.Equal(t, entries, captured.Entries)
	require.True(t, captured.Previous.IsSome())
	require.Len(t, captured.History, 2)
	captured.Entries[0].ID = "changed by caller"
	require.Equal(t, "old", service.ActiveEntries()[0].ID)
}
