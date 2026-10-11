//go:build integration

// Package sessions owns persisted session branches and entries.
package sessions

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
)

// commitReplacementRecordsBeforePublicationSuite isolates receipt recording and failed publication.
type commitReplacementRecordsBeforePublicationSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestCommitReplacementRecordsBeforePublication checks receipt recording before publication
// and retention of its failure cause.
func TestCommitReplacementRecordsBeforePublication(t *testing.T) {
	// Arrange parallel execution of the publication-failure suite.
	t.Parallel()
	// Act through persistence with a failed publication acknowledgement.
	// Assert receipt recording, runtime release, and preserved originals in TestScenario.
	suite.Run(t, &commitReplacementRecordsBeforePublicationSuite{Suite: suite.Suite{}})
}

// TestScenario checks the durable receipt and runtime release ordering.
func (s *commitReplacementRecordsBeforePublicationSuite) TestScenario() {
	t := s.T()
	// Arrange persistence with a failed publication acknowledgement.
	entries := []session.Entry{newCompactionUserEntry("kept", mo.None[string](), "retained")}
	publisher := NewMockEntryPublisher(gomock.NewController(t))
	service, identity := newCompactionBoundaryTestService(t, entries, "kept", mo.Some[EntryPublisher](publisher))
	snapshot := extensioncontext.CompactionSnapshot{
		Identity: identity, ActiveLeafID: mo.Some("kept"), Entries: entries,
		History: nil, Previous: mo.None[session.CompactionEntry](),
	}
	recorded := mo.None[session.Entry]()
	runtimeReleased := false
	cause := errors.New("delivery failed: complete writer source")
	publisher.EXPECT().PublishSessionEntry(gomock.Any()).DoAndReturn(
		func(entry session.Entry) (func(context.Context) error, error) {
			require.Equal(t, mo.Some(entry), recorded)
			return func(context.Context) error {
				require.True(t, runtimeReleased)
				return cause
			}, nil
		},
	)

	// Act by committing with a runtime guard and a Host receipt recorder.
	committed, err := service.CommitReplacement(t.Context(), snapshot, newCompactionBoundaryEntry("kept"),
		func() (func(), error) { return func() { runtimeReleased = true }, nil },
		func(entry session.Entry) { recorded = mo.Some(entry) },
	)

	// Assert that publication failure retains the entry and all original history.
	require.ErrorIs(t, err, cause)
	require.NotEmpty(t, committed.ID)
	require.Equal(t, mo.Some(committed), recorded)
	require.Len(t, service.ActiveEntries(), 2)
	require.Equal(t, "kept", service.ActiveEntries()[0].ID)
}
