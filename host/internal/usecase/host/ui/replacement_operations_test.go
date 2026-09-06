//go:build !integration

package ui

import (
	"errors"
	"testing"
	"time"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestUISessionMutationOwnsGate verifies preparation reserves and releases the session mutation gate.
func TestUISessionMutationOwnsGate(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		acquired    bool
		mutationErr error
		expected    operation.TerminalState
	}{
		{name: "success", acquired: true, mutationErr: nil, expected: operation.TerminalStateCompleted},
		{name: "error", acquired: true, mutationErr: errors.New("label failed"), expected: operation.TerminalStateFailed},
		{name: "busy", acquired: false, mutationErr: nil, expected: operation.TerminalState(0)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one label mutation and observable gate ownership.
			controller := gomock.NewController(t)
			control := NewMockActiveSessions(controller)
			gate := NewMockGate(controller)
			released := false
			gate.EXPECT().TryAcquire().Return(func() { released = true }, test.acquired)
			if test.acquired {
				tree, err := session.NewTree(nil, mo.None[string](), nil)
				require.NoError(t, err)
				control.EXPECT().SetLabel(gomock.Any(), "target", "branch").Return(tree, test.mutationErr)
			}
			service := replacementService(controller, control, gate)
			command := uiReplacementCommand(controllerui.CommandSetEntryLabel, mo.Some("target"), mo.Some("branch"))

			// Act through bounded preparation and terminal execution.
			prepared, err := service.Prepare(t.Context(), command)
			if !test.acquired {
				require.Error(t, err)
				assert.False(t, released)
				return
			}
			require.NoError(t, err)
			outcome := prepared.Run(t.Context(), operation.Reporter[controllerui.Frame]{})
			prepared.Release()

			// Assert terminal state and reservation release match the operation result.
			assert.Equal(t, test.expected, outcome.State())
			assert.True(t, released)
		})
	}
}

// TestApplyReplacementAndLabelCommandsReturnsCommittedFrames verifies typed durable operation results.
func TestApplyReplacementAndLabelCommandsReturnsCommittedFrames(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		command      controllerui.Command
		expect       func(*MockActiveSessions)
		expectedKind controllerui.FrameKind
		assert       func(*testing.T, controllerui.Frame)
	}{
		{
			name: "fork", command: uiReplacementCommand(controllerui.CommandForkSession, mo.Some("target"), mo.None[string]()),
			expect: func(control *MockActiveSessions) {
				control.EXPECT().ForkActive(gomock.Any(), "target").Return(replacementResult(), nil, "exact input", nil)
			},
			expectedKind: controllerui.FrameSessionForked,
			assert: func(t *testing.T, frame controllerui.Frame) {
				assert.Equal(t, mo.Some("exact input"), frame.NextInput)
			},
		},
		{
			name: "clone", command: uiReplacementCommand(controllerui.CommandCloneSession, mo.None[string](), mo.None[string]()),
			expect: func(control *MockActiveSessions) {
				control.EXPECT().CloneActive(gomock.Any()).Return(replacementResult(), nil, nil)
			},
			expectedKind: controllerui.FrameSessionCloned,
			assert: func(t *testing.T, frame controllerui.Frame) {
				assert.Equal(t, session.ID("replacement"), frame.SessionInfo.MustGet().ID)
			},
		},
		{
			name: "label", command: uiReplacementCommand(controllerui.CommandSetEntryLabel, mo.Some("target"), mo.Some("branch")),
			expect: func(control *MockActiveSessions) {
				tree, err := session.NewTree(nil, mo.None[string](), nil)
				require.NoError(t, err)
				control.EXPECT().SetLabel(gomock.Any(), "target", "branch").Return(tree, nil)
			},
			expectedKind: controllerui.FrameEntryLabelSet,
			assert:       func(t *testing.T, frame controllerui.Frame) { assert.True(t, frame.SessionTree.IsSome()) },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange ActiveSessions for the case-specific replacement or label command.
			controller := gomock.NewController(t)
			control := NewMockActiveSessions(controller)
			gate := NewMockGate(controller)
			expectSessionMutationGate(gate, 1)
			test.expect(control)

			// Act by running the prepared replacement or label command.
			frame, err := runPreparedCommand(t, replacementService(controller, control, gate), test.command)

			// Assert the completed frame has the expected durable result kind and payload.
			require.NoError(t, err)
			assert.Equal(t, test.expectedKind, frame.Kind)
			test.assert(t, frame)
		})
	}
}

// TestForkFailurePreservesSessionCause verifies failed replacement operations expose the original cause.
func TestForkFailurePreservesSessionCause(t *testing.T) {
	t.Parallel()
	// Arrange ActiveSessions to reject one fork with ErrInvalidForkTarget.
	controller := gomock.NewController(t)
	control := NewMockActiveSessions(controller)
	gate := NewMockGate(controller)
	expectSessionMutationGate(gate, 1)
	control.EXPECT().ForkActive(gomock.Any(), "model").Return(session.Info{}, nil, "", session.ErrInvalidForkTarget)

	// Act by running the prepared fork command against the rejected target.
	_, err := runPreparedCommand(t, replacementService(controller, control, gate), uiReplacementCommand(
		controllerui.CommandForkSession, mo.Some("model"), mo.None[string](),
	))

	// Assert the operation error preserves the original session cause.
	require.ErrorIs(t, err, session.ErrInvalidForkTarget)
}

// replacementService creates one session service for prepared replacement operations.
func replacementService(controller *gomock.Controller, control *MockActiveSessions, gate *MockGate) *Session {
	service := NewSession(
		NewMockOutput(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller),
		NewMockModelCatalog(controller), control, nil, gate, nil,
	)
	service.setOperationAvailability(AvailabilityIdle)
	return service
}

// uiReplacementCommand creates one fully initialized replacement or label command.
func uiReplacementCommand(kind controllerui.CommandKind, target, label mo.Option[string]) controllerui.Command {
	command := newCommandForPreparedTest(kind)
	command.TargetEntryID = target
	command.EntryLabel = label
	return command
}

// replacementResult returns one committed replacement fixture.
func replacementResult() session.Info {
	return session.Info{
		ID: "replacement", Name: mo.None[string](), WorkingDirectory: "/project",
		StoragePath: mo.Some("/sessions/replacement.jsonl"), CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0),
	}
}
