//go:build !integration

package ui

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestSessionListNormalizesStoredPreview verifies public line-break normalization and absent preview preservation.
func TestSessionListNormalizesStoredPreview(t *testing.T) {
	t.Parallel()

	// Arrange raw stored text and an unnamed row through the client-owned query contract.
	controller := gomock.NewController(t)
	active := NewMockActiveSessions(controller)
	active.EXPECT().ListUISessions(gomock.Any()).Return([]StoredSession{
		{Info: session.Info{}, FirstUserText: mo.Some(" \r\nfirst\r\n\nrequest\t "), TotalMessages: 1},
		{Info: session.Info{}, FirstUserText: mo.None[string](), TotalMessages: 0},
	}, nil)
	service := replacementService(controller, active, NewMockGate(controller))

	// Act through the prepared UI list operation.
	frame, err := runPreparedCommand(t, service, newCommandForPreparedTest(controllerui.CommandListSessions))

	// Assert the public preview has one space per line-break run and preserves absent text and counts.
	require.NoError(t, err)
	require.Len(t, frame.Sessions, 2)
	assert.Equal(t, mo.Some("first request"), frame.Sessions[0].FirstUserText)
	assert.Equal(t, 1, frame.Sessions[0].TotalMessages)
	assert.True(t, frame.Sessions[1].FirstUserText.IsNone())
}
