//go:build !integration

package programmatic

import (
	"errors"
	"fmt"
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
		expect  func(*MockActiveSessions)
		assert  func(*testing.T, controller.Response)
	}{
		{
			name: "fork",
			command: replacementCommand(
				"fork", controller.CommandForkSession, mo.Some("target"), mo.None[string](),
			),
			expect: func(control *MockActiveSessions) {
				control.EXPECT().ForkActive(gomock.Any(), "target").Return(replacementResult(), nil, "exact input", nil)
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
			expect: func(control *MockActiveSessions) {
				control.EXPECT().CloneActive(gomock.Any()).Return(replacementResult(), nil, nil)
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
			expect: func(control *MockActiveSessions) {
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
			control := NewMockActiveSessions(controllerMock)
			gate := NewMockGate(controllerMock)
			test.expect(control)
			service := New(
				NewMockCoordinator(controllerMock),
				NewMockModelCatalog(controllerMock),
				testStateQuery(t, false),
				control, nil,
				gate, testRunOutput(t),
			)

			// Act through Programmatic Control.
			response, operation, err := service.handle(t.Context(), test.command)

			// Assert only committed state is returned.
			require.NoError(t, err)
			require.Nil(t, operation)
			test.assert(t, response)
		})
	}
}

// TestReplacementFailuresReturnClassifiedStateFreeRejections verifies replacement errors retain categories and causes.
func TestReplacementFailuresReturnClassifiedStateFreeRejections(t *testing.T) {
	t.Parallel()

	// labelCause distinguishes the original validation error from its public category.
	labelCause := errors.New("label target does not exist")
	for _, test := range []struct {
		// name identifies the rejected mutation.
		name string
		// command carries the mutation through the Programmatic input contract.
		command controller.Command
		// expect configures the consumed active-session operation.
		expect func(*MockActiveSessions)
		// expectedCode is the public rejection classification.
		expectedCode controller.RejectionCode
		// expectedErr is the source cause that must survive mapping.
		expectedErr error
	}{
		{
			name: "fork target", command: replacementCommand(
				"fork", controller.CommandForkSession, mo.Some("model"), mo.None[string](),
			),
			expect: func(control *MockActiveSessions) {
				control.EXPECT().ForkActive(gomock.Any(), "model").Return(
					session.Info{}, nil, "", session.ErrInvalidForkTarget,
				)
			},
			expectedCode: controller.RejectionInvalidArgument,
			expectedErr:  session.ErrInvalidForkTarget,
		},
		{
			name: "label target", command: replacementCommand(
				"label", controller.CommandSetEntryLabel, mo.Some("missing"), mo.Some("branch"),
			),
			expect: func(control *MockActiveSessions) {
				control.EXPECT().SetLabel(gomock.Any(), "missing", "branch").Return(
					session.Tree{}, fmt.Errorf("%w: set session entry label: %w", session.ErrEntryNotFound, labelCause),
				)
			},
			expectedCode: controller.RejectionNotFound,
			expectedErr:  labelCause,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange a replacement request rejected by the Host domain.
			controllerMock := gomock.NewController(t)
			control := NewMockActiveSessions(controllerMock)
			gate := NewMockGate(controllerMock)
			test.expect(control)
			service := New(
				NewMockCoordinator(controllerMock),
				NewMockModelCatalog(controllerMock),
				testStateQuery(t, false),
				control, nil,
				gate, testRunOutput(t),
			)

			// Act through Programmatic Control.
			response, operation, err := service.handle(t.Context(), test.command)

			// Assert classification and complete source cause without speculative committed state.
			require.NoError(t, err)
			require.Nil(t, operation)
			require.Equal(t, controller.ResponseRejected, response.Kind)
			rejection := response.Rejection.MustGet()
			require.Equal(t, test.expectedCode, rejection.Code)
			require.ErrorIs(t, rejection.Cause, test.expectedErr)
			require.ErrorContains(t, rejection.Cause, test.expectedErr.Error())
			require.True(t, response.Replacement.IsNone())
			require.True(t, response.SessionTree.IsNone())
		})
	}
}

// replacementCommand creates one fully initialized fork, clone, or label command.
func replacementCommand(
	operationID string,
	kind controller.CommandKind,
	target, label mo.Option[string],
) controller.Command {
	command := testProgrammaticCommand(operationID, kind)
	command.TargetEntryID = target
	command.EntryLabel = label
	return command
}

// replacementResult creates one active-session replacement for public mapping tests.
func replacementResult() session.Info {
	return session.Info{
		ID: "replacement", Name: mo.None[string](), WorkingDirectory: "/project",
		StoragePath: mo.Some("/sessions/replacement.jsonl"), CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0),
	}
}
