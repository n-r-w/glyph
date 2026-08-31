//go:build !integration

package programmatic

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestReplacementAndLabelCommandsReturnCommittedState verifies typed fork, clone, and label results.
func TestReplacementAndLabelCommandsReturnCommittedState(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		command controller.Command
		expect  func(*MockSessionControl)
		assert  func(*testing.T, controller.Response)
	}{
		{
			name: "fork",
			command: replacementCommand(
				"fork", controller.CommandForkSession, mo.Some("target"), mo.None[string](),
			),
			expect: func(control *MockSessionControl) {
				control.EXPECT().Fork(gomock.Any(), "target").Return(replacementResult(), "exact input", nil)
			},
			assert: func(t *testing.T, response controller.Response) {
				require.Equal(t, controller.ResponseForkSession, response.Kind)
				require.Equal(t, session.ID("replacement"), response.Replacement.MustGet().Info.ID)
				require.Equal(t, mo.Some("exact input"), response.Replacement.MustGet().NextInput)
			},
		},
		{
			name: "clone",
			command: replacementCommand(
				"clone", controller.CommandCloneSession, mo.None[string](), mo.None[string](),
			),
			expect: func(control *MockSessionControl) {
				control.EXPECT().Clone(gomock.Any()).Return(replacementResult(), nil)
			},
			assert: func(t *testing.T, response controller.Response) {
				require.Equal(t, controller.ResponseCloneSession, response.Kind)
				require.Equal(t, session.ID("replacement"), response.Replacement.MustGet().Info.ID)
				require.True(t, response.Replacement.MustGet().NextInput.IsNone())
			},
		},
		{
			name: "label",
			command: replacementCommand(
				"label", controller.CommandSetEntryLabel, mo.Some("target"), mo.Some("branch"),
			),
			expect: func(control *MockSessionControl) {
				tree, err := session.NewTree(nil, mo.None[string](), nil)
				require.NoError(t, err)
				control.EXPECT().SetLabel(gomock.Any(), "target", "branch").Return(tree, nil)
			},
			assert: func(t *testing.T, response controller.Response) {
				require.Equal(t, controller.ResponseSetEntryLabel, response.Kind)
				require.True(t, response.SessionTree.IsSome())
				require.True(t, response.Replacement.IsNone())
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange strict dependencies for one public session operation.
			controllerMock := gomock.NewController(t)
			control := NewMockSessionControl(controllerMock)
			test.expect(control)
			service := New(
				NewMockCoordinator(controllerMock),
				NewMockModelCatalog(controllerMock),
				idleStateSnapshot,
				emptyHistorySnapshot,
				control,
				NewDelivery(),
			)

			// Act through Programmatic Control.
			response, operation, err := service.Handle(t.Context(), test.command)

			// Assert only committed state is returned.
			require.NoError(t, err)
			require.Nil(t, operation)
			test.assert(t, response)
		})
	}
}

// TestForkFailureReturnsClassifiedStateFreeRejection verifies invalid targets publish no replacement data.
func TestForkFailureReturnsClassifiedStateFreeRejection(t *testing.T) {
	t.Parallel()

	// Arrange a fork target rejected by the Host domain.
	controllerMock := gomock.NewController(t)
	control := NewMockSessionControl(controllerMock)
	control.EXPECT().Fork(gomock.Any(), "model").Return(session.Replacement{}, "", session.ErrInvalidForkTarget)
	service := New(
		NewMockCoordinator(controllerMock),
		NewMockModelCatalog(controllerMock),
		idleStateSnapshot,
		emptyHistorySnapshot,
		control,
		NewDelivery(),
	)

	// Act by forking a non-user entry.
	response, operation, err := service.Handle(
		t.Context(),
		replacementCommand("fork", controller.CommandForkSession, mo.Some("model"), mo.None[string]()),
	)

	// Assert the existing invalid-argument mapping contains no speculative replacement or input.
	require.NoError(t, err)
	require.Nil(t, operation)
	require.Equal(t, controller.ResponseRejected, response.Kind)
	require.Equal(t, controller.RejectionInvalidArgument, response.Rejection.MustGet().Code)
	require.True(t, response.Replacement.IsNone())
}

// replacementCommand creates one fully initialized fork, clone, or label command.
func replacementCommand(
	correlationID string,
	kind controller.CommandKind,
	target, label mo.Option[string],
) controller.Command {
	command := testProgrammaticCommand(correlationID, kind)
	command.TargetEntryID = target
	command.EntryLabel = label
	return command
}

// replacementResult creates one active-session replacement for public mapping tests.
func replacementResult() session.Replacement {
	return session.Replacement{Info: session.Info{
		ID: "replacement", Name: mo.None[string](), WorkingDirectory: "/project",
		StoragePath: mo.Some("/sessions/replacement.jsonl"), CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0),
	}, Entries: nil}
}
