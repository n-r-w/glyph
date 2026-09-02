//go:build !integration

package sessions

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestResumePropagatesNonPersistenceFailures verifies lookup and validation failures reach the caller.
func TestResumePropagatesNonPersistenceFailures(t *testing.T) {
	t.Parallel()

	// Arrange two non-persistence load failures.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	clientID := session.ID("unknown-session")
	repository.EXPECT().Load(gomock.Any(), clientID).Return(LoadedSession{}, os.ErrNotExist)
	repository.EXPECT().Load(gomock.Any(), session.ID("malformed-client-value")).Return(
		LoadedSession{}, session.ErrUnavailable,
	)
	service := New(repository, NewMockIDGenerator(controller), NewMockClock(controller), nil, "/project")

	// Act by attempting one unknown resume and one unavailable stored-session resume.
	_, notFoundErr := service.ResumeActive(t.Context(), clientID)
	_, unavailableErr := service.ResumeActive(t.Context(), session.ID("malformed-client-value"))

	// Assert both errors propagate.
	require.ErrorIs(t, notFoundErr, os.ErrNotExist)
	require.ErrorIs(t, unavailableErr, session.ErrUnavailable)
}

// TestResumePropagatesPersistenceFailure verifies failed recovery remains classified for the caller.
func TestResumePropagatesPersistenceFailure(t *testing.T) {
	t.Parallel()

	// Arrange a classified recovery failure.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	clientID := session.ID("failed-recovery")
	infrastructureErr := errors.New("truncate failed")
	repository.EXPECT().Load(gomock.Any(), clientID).Return(
		LoadedSession{}, errors.Join(session.ErrPersistenceUnavailable, infrastructureErr),
	)
	service := New(repository, NewMockIDGenerator(controller), NewMockClock(controller), nil, "/project")

	// Act by attempting to resume a session whose recovery cannot truncate storage.
	_, err := service.ResumeActive(t.Context(), clientID)

	// Assert the persistence classification propagates.
	require.ErrorIs(t, err, session.ErrPersistenceUnavailable)
}
