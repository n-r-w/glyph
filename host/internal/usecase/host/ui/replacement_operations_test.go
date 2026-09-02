//go:build !integration

package ui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
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
			control := NewMockSessionControl(controller)
			released := false
			control.EXPECT().TryAcquire().Return(func() { released = true }, test.acquired)
			if test.acquired {
				tree, err := session.NewTree(nil, mo.None[string](), nil)
				require.NoError(t, err)
				control.EXPECT().SetLabel(gomock.Any(), "target", "branch").Return(tree, test.mutationErr)
			}
			service := replacementService(controller, control)
			command := uiReplacementCommand(domainui.CommandSetEntryLabel, mo.Some("target"), mo.Some("branch"))

			// Act through bounded preparation and terminal execution.
			prepared, err := service.Prepare(t.Context(), command)
			if !test.acquired {
				require.Error(t, err)
				assert.False(t, released)
				return
			}
			require.NoError(t, err)
			outcome := prepared.Run(t.Context(), operation.Reporter[domainui.Frame]{})
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
		command      domainui.Command
		expect       func(*MockSessionControl)
		expectedKind domainui.FrameKind
		assert       func(*testing.T, domainui.Frame)
	}{
		{
			name: "fork", command: uiReplacementCommand(domainui.CommandForkSession, mo.Some("target"), mo.None[string]()),
			expect: func(control *MockSessionControl) {
				control.EXPECT().Fork(gomock.Any(), "target").Return(replacementResult(), "exact input", nil)
			},
			expectedKind: domainui.FrameSessionForked,
			assert: func(t *testing.T, frame domainui.Frame) {
				assert.Equal(t, mo.Some("exact input"), frame.Text)
			},
		},
		{
			name: "clone", command: uiReplacementCommand(domainui.CommandCloneSession, mo.None[string](), mo.None[string]()),
			expect: func(control *MockSessionControl) {
				control.EXPECT().Clone(gomock.Any()).Return(replacementResult(), nil)
			},
			expectedKind: domainui.FrameSessionCloned,
			assert: func(t *testing.T, frame domainui.Frame) {
				assert.Equal(t, session.ID("replacement"), frame.SessionInfo.MustGet().ID)
			},
		},
		{
			name: "label", command: uiReplacementCommand(domainui.CommandSetEntryLabel, mo.Some("target"), mo.Some("branch")),
			expect: func(control *MockSessionControl) {
				tree, err := session.NewTree(nil, mo.None[string](), nil)
				require.NoError(t, err)
				control.EXPECT().SetLabel(gomock.Any(), "target", "branch").Return(tree, nil)
			},
			expectedKind: domainui.FrameEntryLabelSet,
			assert:       func(t *testing.T, frame domainui.Frame) { assert.True(t, frame.SessionTree.IsSome()) },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange SessionControl for the case-specific replacement or label command.
			controller := gomock.NewController(t)
			control := NewMockSessionControl(controller)
			expectSessionMutationGate(control, 1)
			test.expect(control)

			// Act by running the prepared replacement or label command.
			frame, err := runPreparedCommand(t, replacementService(controller, control), test.command)

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
	// Arrange SessionControl to reject one fork with ErrInvalidForkTarget.
	controller := gomock.NewController(t)
	control := NewMockSessionControl(controller)
	expectSessionMutationGate(control, 1)
	control.EXPECT().Fork(gomock.Any(), "model").Return(session.Replacement{}, "", session.ErrInvalidForkTarget)

	// Act by running the prepared fork command against the rejected target.
	_, err := runPreparedCommand(t, replacementService(controller, control), uiReplacementCommand(
		domainui.CommandForkSession, mo.Some("model"), mo.None[string](),
	))

	// Assert the operation error preserves the original session cause.
	require.ErrorIs(t, err, session.ErrInvalidForkTarget)
}

// replacementService creates one session service for prepared replacement operations.
func replacementService(controller *gomock.Controller, control *MockSessionControl) *Session {
	service := NewSession(
		NewMockChannel(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller),
		NewMockModelCatalog(controller), control, func(context.Context) {},
	)
	service.setOperationAvailability(domainui.AvailabilityIdle)
	return service
}

// uiReplacementCommand creates one fully initialized replacement or label command.
func uiReplacementCommand(kind domainui.CommandKind, target, label mo.Option[string]) domainui.Command {
	command := newCommandForPreparedTest(kind)
	command.TargetEntryID = target
	command.EntryLabel = label
	return command
}

// replacementResult returns one committed replacement fixture.
func replacementResult() session.Replacement {
	return session.Replacement{Info: session.Info{
		ID: "replacement", Name: mo.None[string](), WorkingDirectory: "/project",
		StoragePath: mo.Some("/sessions/replacement.jsonl"), CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0),
	}, Entries: nil}
}
