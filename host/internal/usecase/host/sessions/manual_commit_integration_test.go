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

// commitReplacementRecordsBeforePublicationSuite owns an independent scenario context.
type commitReplacementRecordsBeforePublicationSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestCommitReplacementRecordsBeforePublication checks receipt recording before publication
// and retention of its failure cause.
func TestCommitReplacementRecordsBeforePublication(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Parallel()
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &commitReplacementRecordsBeforePublicationSuite{Suite: suite.Suite{}})
}

// TestScenario checks the durable receipt and runtime release ordering.
func (s *commitReplacementRecordsBeforePublicationSuite) TestScenario() {
	t := s.T()
	// Arrange persistence with a failed publication acknowledgement.
	// entries contains the original branch before replacement.
	entries := []session.Entry{newCompactionUserEntry("kept", mo.None[string](), "retained")}
	// publisher supplies the failed delivery acknowledgement.
	publisher := NewMockEntryPublisher(gomock.NewController(t))
	// service and identity identify the session owner and its commit authority.
	service, identity := newCompactionBoundaryTestService(t, entries, "kept", mo.Some[EntryPublisher](publisher))
	// snapshot contains the captured branch for replacement.
	snapshot := extensioncontext.CompactionSnapshot{
		Identity: identity, ActiveLeafID: mo.Some("kept"), Entries: entries,
		History: nil, Previous: mo.None[session.CompactionEntry](),
	}
	// recorded contains the receipt observed before publication.
	recorded := mo.None[session.Entry]()
	// runtimeReleased indicates release of the commit runtime guard.
	runtimeReleased := false
	// cause contains the publication delivery failure.
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
	// committed and err contain the durable replacement receipt and delivery failure.
	committed, err := service.CommitReplacement(t.Context(), snapshot, boundaryTestCompaction("kept"),
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
