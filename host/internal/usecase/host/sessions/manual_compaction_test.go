//go:build !integration

// Package sessions owns session history and compaction state.
package sessions

import (
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
)

// TestScenario checks complete validation errors before mutation.
func (s *projectReplacementPreservesBoundaryCauseSuite) TestScenario() {
	t := s.T()
	// Arrange one captured branch and an outside boundary.
	// entries contains the captured branch with one retained user entry.
	entries := []session.Entry{newCompactionUserEntry("kept", mo.None[string](), "retained")}
	// service and identity identify the session owner and its commit authority.
	service, identity := newCompactionHistoryTestService(t, entries, "kept")
	// snapshot contains the captured branch for candidate projection.
	snapshot := extensioncontext.CompactionSnapshot{
		Identity: identity, ActiveLeafID: mo.Some("kept"), Entries: entries,
		History: nil, Previous: mo.None[session.CompactionEntry](),
	}

	// Act by requesting the candidate projection.
	// history and err contain the candidate model input and projection failure.
	history, err := service.ProjectReplacement(snapshot, newCompactionBoundaryEntry("outside"))

	// Assert that the caller receives the session boundary cause rather than an empty projection.
	require.ErrorContains(t, err, "compaction boundary is outside its branch")
	require.Nil(t, history)
	require.Len(t, service.ActiveEntries(), 1)
}
