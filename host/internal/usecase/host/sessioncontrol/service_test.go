package sessioncontrol

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

func TestCreateReturnsBusyAndPreservesActiveSession(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	active := NewMockActiveSessions(controller)
	gate := NewMockOperationGate(controller)
	gate.EXPECT().TryAcquire().Return(nil, false)
	service := New(active, gate)

	_, err := service.Create(t.Context())
	require.ErrorIs(t, err, session.ErrBusy)
}

func TestResumeHoldsGateThroughActiveReplacement(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	active := NewMockActiveSessions(controller)
	gate := NewMockOperationGate(controller)
	released := false
	gate.EXPECT().TryAcquire().Return(func() { released = true }, true)
	service := New(active, gate)
	active.EXPECT().ResumeActive(gomock.Any(), session.ID("stored")).DoAndReturn(
		func(_ any, _ session.ID) (session.Info, error) {
			require.False(t, released)
			return session.Info{
				ID: "stored", Name: mo.None[string](), WorkingDirectory: "",
				StoragePath: mo.None[string](), CreatedAt: time.Time{}, UpdatedAt: time.Time{},
			}, nil
		},
	)

	info, err := service.Resume(t.Context(), "stored")
	require.NoError(t, err)
	require.Equal(t, session.ID("stored"), info.ID)
	require.True(t, released)
}
