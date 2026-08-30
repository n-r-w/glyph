package ui

import (
	"context"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// TestApplyReplacementAndLabelCommandsSendsCommittedFrames verifies typed UI results after durable operations.
func TestApplyReplacementAndLabelCommandsSendsCommittedFrames(t *testing.T) {
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
				require.Equal(t, mo.Some("exact input"), frame.Text)
				require.Equal(t, session.ID("replacement"), frame.SessionInfo.MustGet().ID)
			},
		},
		{
			name: "clone", command: uiReplacementCommand(domainui.CommandCloneSession, mo.None[string](), mo.None[string]()),
			expect: func(control *MockSessionControl) {
				control.EXPECT().Clone(gomock.Any()).Return(replacementResult(), nil)
			},
			expectedKind: domainui.FrameSessionCloned,
			assert: func(t *testing.T, frame domainui.Frame) {
				require.True(t, frame.Text.IsNone())
				require.Equal(t, session.ID("replacement"), frame.SessionInfo.MustGet().ID)
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
			assert: func(t *testing.T, frame domainui.Frame) {
				require.True(t, frame.SessionTree.IsSome())
				require.True(t, frame.SessionInfo.IsNone())
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange strict dependencies and one expected committed frame.
			controller := gomock.NewController(t)
			channel := NewMockChannel(controller)
			control := NewMockSessionControl(controller)
			test.expect(control)
			channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
				require.Equal(t, test.expectedKind, frame.Kind)
				test.assert(t, frame)
				return nil
			})
			service := NewSession(channel, NewMockAgentRunner(controller), NewMockAuthenticator(controller), NewMockModelCatalog(controller), control, func(context.Context) {})

			// Act through the UI session command path.
			handled, err := service.applySessionCommand(t.Context(), test.command)

			// Assert the operation is handled without speculative state.
			require.NoError(t, err)
			require.True(t, handled)
		})
	}
}

// TestForkFailureUsesExistingSessionFailureFrame verifies failed fork sends no replacement frame.
func TestForkFailureUsesExistingSessionFailureFrame(t *testing.T) {
	t.Parallel()

	// Arrange a failed fork and one existing information failure frame.
	controller := gomock.NewController(t)
	channel := NewMockChannel(controller)
	control := NewMockSessionControl(controller)
	control.EXPECT().Fork(gomock.Any(), "model").Return(session.Replacement{}, "", session.ErrInvalidForkTarget)
	channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
		require.Equal(t, domainui.FrameInformation, frame.Kind)
		require.True(t, frame.SessionInfo.IsNone())
		require.True(t, frame.Text.IsSome())
		return nil
	})
	service := NewSession(channel, NewMockAgentRunner(controller), NewMockAuthenticator(controller), NewMockModelCatalog(controller), control, func(context.Context) {})

	// Act by forking a non-user entry.
	handled, err := service.applySessionCommand(t.Context(), uiReplacementCommand(domainui.CommandForkSession, mo.Some("model"), mo.None[string]()))

	// Assert the failure is handled through the existing session failure path.
	require.NoError(t, err)
	require.True(t, handled)
}

// uiReplacementCommand creates one fully initialized UI replacement or label command.
func uiReplacementCommand(kind domainui.CommandKind, target, label mo.Option[string]) domainui.Command {
	command := uiTreeCommand(kind)
	command.TargetEntryID = target
	command.EntryLabel = label
	return command
}

// replacementResult creates one active-session replacement for UI tests.
func replacementResult() session.Replacement {
	return session.Replacement{Info: session.Info{
		ID: "replacement", Name: mo.None[string](), WorkingDirectory: "/project",
		StoragePath: mo.Some("/sessions/replacement.jsonl"), CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0),
	}, Entries: nil}
}
