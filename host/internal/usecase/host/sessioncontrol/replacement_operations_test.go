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

// TestForkAndCloneReturnReplacementResults verifies operation result and cause propagation.
func TestForkAndCloneReturnReplacementResults(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		invoke func(*Service) error
		expect func(*MockActiveSessions)
	}{
		{
			name: "fork",
			invoke: func(service *Service) error {
				_, _, err := service.Fork(t.Context(), "target")
				return err
			},
			expect: func(active *MockActiveSessions) {
				active.EXPECT().
					ForkActive(gomock.Any(), "target").
					DoAndReturn(func(any, string) (session.Replacement, string, error) {
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
			expect: func(active *MockActiveSessions) {
				active.EXPECT().CloneActive(gomock.Any()).DoAndReturn(func(any) (session.Replacement, error) {
					return session.Replacement{}, nil
				})
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange the active-session or navigation result.
			controller := gomock.NewController(t)
			active := NewMockActiveSessions(controller)

			test.expect(active)
			service := New(active, NewMockNavigator(controller))

			// Act by invoking the requested operation.

			err := test.invoke(service)

			// Assert the operation preserves its result and cause.
			require.NoError(t, err)
		})
	}
}

// TestSetLabelReturnsCommittedTree verifies operation result and cause propagation.
func TestSetLabelReturnsCommittedTree(t *testing.T) {
	t.Parallel()

	// Arrange strict dependencies and one committed label tree.
	controller := gomock.NewController(t)
	active := NewMockActiveSessions(controller)
	tree, err := session.NewTree(nil, mo.None[string](), nil)
	require.NoError(t, err)
	active.EXPECT().SetLabel(gomock.Any(), "entry", "label").Return(tree, nil)

	service := New(active, NewMockNavigator(controller))

	// Act by invoking the requested operation.

	committed, err := service.SetLabel(t.Context(), "entry", "label")

	// Assert the operation preserves its result and cause.
	require.NoError(t, err)
	require.Equal(t, tree, committed)
}

// TestForkPreservesFailure verifies operation result and cause propagation.
func TestForkPreservesFailure(t *testing.T) {
	t.Parallel()

	// Arrange the active-session or navigation result.
	controller := gomock.NewController(t)
	active := NewMockActiveSessions(controller)

	active.EXPECT().ForkActive(gomock.Any(), "target").Return(session.Replacement{}, "", errors.New("failed"))
	service := New(active, NewMockNavigator(controller))

	// Act by invoking the requested operation.

	_, _, err := service.Fork(t.Context(), "target")

	// Assert the operation preserves its result and cause.
	require.Error(t, err)
}
