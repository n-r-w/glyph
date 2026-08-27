package sessioncontrol

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestCreateReturnsBusyAndPreservesActiveSession verifies gate rejection prevents active-session replacement.
func TestCreateReturnsBusyAndPreservesActiveSession(t *testing.T) {
	t.Parallel()

	// Arrange session control with a gate that rejects acquisition and no active-session expectation.
	controller := gomock.NewController(t)
	active := NewMockActiveSessions(controller)
	gate := NewMockOperationGate(controller)
	gate.EXPECT().TryAcquire().Return(nil, false)
	service := New(active, gate)

	// Act by requesting creation while the gate is owned.
	_, err := service.Create(t.Context())

	// Assert the busy error is returned without invoking active replacement.
	require.ErrorIs(t, err, session.ErrBusy)
}

// TestResumeHoldsGateThroughActiveReplacement verifies resume releases the gate only after active replacement returns.
func TestResumeHoldsGateThroughActiveReplacement(t *testing.T) {
	t.Parallel()

	// Arrange a release observer and an active-session replacement that checks gate ownership.
	controller := gomock.NewController(t)
	active := NewMockActiveSessions(controller)
	gate := NewMockOperationGate(controller)
	released := false
	gate.EXPECT().TryAcquire().Return(func() { released = true }, true)
	service := New(active, gate)
	active.EXPECT().ResumeActive(gomock.Any(), session.ID("stored")).DoAndReturn(
		func(_ any, _ session.ID) (session.Replacement, error) {
			require.False(t, released)
			return session.Replacement{Info: session.Info{
				ID: "stored", Name: mo.None[string](), WorkingDirectory: "",
				StoragePath: mo.None[string](), CreatedAt: time.Time{}, UpdatedAt: time.Time{},
			}, Entries: nil}, nil
		},
	)

	// Act by resuming the stored session through session control.
	replacement, err := service.Resume(t.Context(), "stored")

	// Assert replacement runs before release and the gate is released after the result returns.
	require.NoError(t, err)
	require.Equal(t, session.ID("stored"), replacement.Info.ID)
	require.True(t, released)
}
