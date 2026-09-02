//go:build !integration

package sessioncontrol

import (
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestForkAndCloneUseCallerReservation verifies mutation methods do not reacquire the shared gate.
func TestForkAndCloneUseCallerReservation(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		invoke func(*Service) error
		expect func(*MockActiveSessions, *bool)
	}{
		{
			name: "fork",
			invoke: func(service *Service) error {
				_, _, err := service.Fork(t.Context(), "target")
				return err
			},
			expect: func(active *MockActiveSessions, released *bool) {
				active.EXPECT().
					ForkActive(gomock.Any(), "target").
					DoAndReturn(func(any, string) (session.Replacement, string, error) {
						require.False(t, *released)
						return session.Replacement{}, "input", nil
					})
			},
		},
		{
			name: "clone",
			invoke: func(service *Service) error {
				_, err := service.Clone(t.Context())
				return err
			},
			expect: func(active *MockActiveSessions, released *bool) {
				active.EXPECT().CloneActive(gomock.Any()).DoAndReturn(func(any) (session.Replacement, error) {
					require.False(t, *released)
					return session.Replacement{}, nil
				})
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange an acquired gate and an active replacement that observes ownership.
			controller := gomock.NewController(t)
			active := NewMockActiveSessions(controller)
			released := false
			tryAcquire := func() (func(), bool) { return func() { released = true }, true }
			test.expect(active, &released)
			service := New(active, NewMockNavigator(controller), tryAcquire)

			// Act after the caller acquires mutation ownership.
			release, acquired := service.TryAcquire()
			require.True(t, acquired)
			err := test.invoke(service)

			// Assert the mutation retains ownership until the caller releases it.
			require.NoError(t, err)
			require.False(t, released)
			release()
			require.True(t, released)
		})
	}
}

// TestReplacementCallerRejectsBusyWithoutMutation verifies admission publishes no replacement state.
func TestReplacementCallerRejectsBusyWithoutMutation(t *testing.T) {
	t.Parallel()

	// Arrange strict dependencies with no active-session call expected.
	controller := gomock.NewController(t)
	service := New(
		NewMockActiveSessions(controller),
		NewMockNavigator(controller),
		func() (func(), bool) { return nil, false },
	)

	// Act while another operation owns the gate.
	_, acquired := service.TryAcquire()

	// Assert the caller observes busy without invoking a mutation.
	require.False(t, acquired)
}

// TestSetLabelUsesCallerReservation verifies label mutation does not reacquire the gate.
func TestSetLabelUsesCallerReservation(t *testing.T) {
	t.Parallel()

	// Arrange strict dependencies and one committed label tree.
	controller := gomock.NewController(t)
	active := NewMockActiveSessions(controller)
	tree, err := session.NewTree(nil, mo.None[string](), nil)
	require.NoError(t, err)
	active.EXPECT().SetLabel(gomock.Any(), "entry", "label").Return(tree, nil)
	released := false
	tryAcquire := func() (func(), bool) { return func() { released = true }, true }
	service := New(active, NewMockNavigator(controller), tryAcquire)

	// Act after the caller acquires label ownership.
	release, acquired := service.TryAcquire()
	require.True(t, acquired)
	committed, err := service.SetLabel(t.Context(), "entry", "label")

	// Assert the committed snapshot is returned before caller cleanup.
	require.NoError(t, err)
	require.Equal(t, tree, committed)
	require.False(t, released)
	release()
	require.True(t, released)
}

// TestForkCallerReleasesGateOnFailure verifies caller cleanup after a mutation error.
func TestForkCallerReleasesGateOnFailure(t *testing.T) {
	t.Parallel()

	// Arrange one failed active replacement under an acquired gate.
	controller := gomock.NewController(t)
	active := NewMockActiveSessions(controller)
	released := false
	tryAcquire := func() (func(), bool) { return func() { released = true }, true }
	active.EXPECT().ForkActive(gomock.Any(), "target").Return(session.Replacement{}, "", errors.New("failed"))
	service := New(active, NewMockNavigator(controller), tryAcquire)

	// Act after the caller acquires mutation ownership.
	release, acquired := service.TryAcquire()
	require.True(t, acquired)
	_, _, err := service.Fork(t.Context(), "target")

	// Assert the error is preserved and ownership remains until caller cleanup.
	require.Error(t, err)
	require.False(t, released)
	release()
	require.True(t, released)
}
