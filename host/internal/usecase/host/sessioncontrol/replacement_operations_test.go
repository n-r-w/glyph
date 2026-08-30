package sessioncontrol

import (
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestForkAndCloneHoldTheOperationGate verifies replacement creation cannot overlap agent execution.
func TestForkAndCloneHoldTheOperationGate(t *testing.T) {
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
			gate := NewMockOperationGate(controller)
			released := false
			gate.EXPECT().TryAcquire().Return(func() { released = true }, true)
			test.expect(active, &released)
			service := New(active, NewMockNavigator(controller), gate)

			// Act through the replacement facade.
			err := test.invoke(service)

			// Assert success releases the operation gate after persistence returns.
			require.NoError(t, err)
			require.True(t, released)
		})
	}
}

// TestForkAndCloneReturnBusyWithoutReplacement verifies gate rejection publishes no replacement state.
func TestForkAndCloneReturnBusyWithoutReplacement(t *testing.T) {
	t.Parallel()

	for _, invoke := range []func(*Service) error{
		func(service *Service) error { _, _, err := service.Fork(t.Context(), "target"); return err },
		func(service *Service) error { _, err := service.Clone(t.Context()); return err },
	} {
		// Arrange strict dependencies with no active-session call expected.
		controller := gomock.NewController(t)
		gate := NewMockOperationGate(controller)
		gate.EXPECT().TryAcquire().Return(nil, false)
		service := New(NewMockActiveSessions(controller), NewMockNavigator(controller), gate)

		// Act while another operation owns the gate.
		err := invoke(service)

		// Assert the stable busy error is returned.
		require.ErrorIs(t, err, session.ErrBusy)
	}
}

// TestSetLabelDelegatesWithoutReplacementGate verifies label mutation follows session-name concurrency behavior.
func TestSetLabelDelegatesWithoutReplacementGate(t *testing.T) {
	t.Parallel()

	// Arrange strict dependencies and one committed label tree.
	controller := gomock.NewController(t)
	active := NewMockActiveSessions(controller)
	tree, err := session.NewTree(nil, mo.None[string](), nil)
	require.NoError(t, err)
	active.EXPECT().SetLabel(gomock.Any(), "entry", "label").Return(tree, nil)
	service := New(active, NewMockNavigator(controller), NewMockOperationGate(controller))

	// Act by setting one label.
	committed, err := service.SetLabel(t.Context(), "entry", "label")

	// Assert the committed snapshot is returned unchanged.
	require.NoError(t, err)
	require.Equal(t, tree, committed)
}

// TestForkReleasesGateOnFailure verifies failed durable creation still releases operation ownership.
func TestForkReleasesGateOnFailure(t *testing.T) {
	t.Parallel()

	// Arrange one failed active replacement under an acquired gate.
	controller := gomock.NewController(t)
	active := NewMockActiveSessions(controller)
	gate := NewMockOperationGate(controller)
	released := false
	gate.EXPECT().TryAcquire().Return(func() { released = true }, true)
	active.EXPECT().ForkActive(gomock.Any(), "target").Return(session.Replacement{}, "", errors.New("failed"))
	service := New(active, NewMockNavigator(controller), gate)

	// Act through the fork facade.
	_, _, err := service.Fork(t.Context(), "target")

	// Assert the failure is preserved and cleanup runs.
	require.Error(t, err)
	require.True(t, released)
}
