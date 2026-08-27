package sessions

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

var persistenceLoggerTestMutex sync.Mutex

// TestLogPersistenceFailureUsesSafeStructuredContext verifies diagnostics contain classification and cause without payload fields.
func TestLogPersistenceFailureUsesSafeStructuredContext(t *testing.T) {
	t.Parallel()

	// Arrange a JSON logger and an infrastructure error without persisted session values.
	persistenceLoggerTestMutex.Lock()
	t.Cleanup(persistenceLoggerTestMutex.Unlock)
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	infrastructureErr := errors.New("disk sync failed")

	// Act by recording one classified active-history persistence failure.
	logPersistenceFailure(t.Context(), persistenceOperationHistory, session.ID("session-id"), infrastructureErr)

	// Assert structured safe context and the underlying cause are present without payload keys.
	logged := output.String()
	require.NotEmpty(t, logged)
	assert.Contains(t, logged, `"operation":"append_history"`)
	assert.Contains(t, logged, `"session_id":"session-id"`)
	assert.Contains(t, logged, `"error":"disk sync failed"`)
	assert.NotContains(t, logged, "content")
	assert.NotContains(t, logged, "provider_context")
	assert.NotContains(t, logged, "extension")
}

// TestResumeNonPersistenceFailuresDoNotLog verifies normal lookup and validation failures do not enter persistence diagnostics.
func TestResumeNonPersistenceFailuresDoNotLog(t *testing.T) {
	t.Parallel()

	// Arrange two non-persistence load failures and an isolated JSON logger.
	persistenceLoggerTestMutex.Lock()
	t.Cleanup(persistenceLoggerTestMutex.Unlock)
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	clientID := session.ID("/secret/path provider-context extension-json")
	repository.EXPECT().Load(gomock.Any(), clientID).Return(LoadedSession{}, os.ErrNotExist)
	repository.EXPECT().Load(gomock.Any(), session.ID("malformed-client-value")).Return(
		LoadedSession{}, session.ErrUnavailable,
	)
	service := New(repository, NewMockIDGenerator(controller), NewMockClock(controller), nil, "/project")

	// Act by attempting one unknown resume and one unavailable stored-session resume.
	_, notFoundErr := service.ResumeActive(t.Context(), clientID)
	_, unavailableErr := service.ResumeActive(t.Context(), session.ID("malformed-client-value"))

	// Assert both errors propagate without a persistence diagnostic or client-controlled value.
	require.ErrorIs(t, notFoundErr, os.ErrNotExist)
	require.ErrorIs(t, unavailableErr, session.ErrUnavailable)
	assert.Empty(t, output.String())
}

// TestResumePersistenceFailureLogsSafeOperation verifies failed recovery records safe operation and infrastructure cause only.
func TestResumePersistenceFailureLogsSafeOperation(t *testing.T) {
	t.Parallel()

	// Arrange a classified failed recovery, a prohibited client ID, and an isolated JSON logger.
	persistenceLoggerTestMutex.Lock()
	t.Cleanup(persistenceLoggerTestMutex.Unlock)
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	clientID := session.ID("/secret/path provider-context extension-json")
	infrastructureErr := errors.New("truncate failed")
	repository.EXPECT().Load(gomock.Any(), clientID).Return(
		LoadedSession{}, errors.Join(session.ErrPersistenceUnavailable, infrastructureErr),
	)
	service := New(repository, NewMockIDGenerator(controller), NewMockClock(controller), nil, "/project")

	// Act by attempting to resume the target whose interrupted-tail recovery cannot truncate storage.
	_, err := service.ResumeActive(t.Context(), clientID)

	// Assert the classified error logs safe operation and cause without client-controlled or payload data.
	require.ErrorIs(t, err, session.ErrPersistenceUnavailable)
	logged := output.String()
	require.NotEmpty(t, logged)
	assert.Contains(t, logged, `"operation":"resume"`)
	assert.Contains(t, logged, "truncate failed")
	assert.NotContains(t, logged, `"session_id"`)
	assert.NotContains(t, logged, string(clientID))
	assert.NotContains(t, logged, "content")
	assert.NotContains(t, logged, "provider-context")
	assert.NotContains(t, logged, "extension-json")
}
