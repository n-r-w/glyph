//go:build !integration

package runtime

import (
	"testing"
	"time"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestMapReplacementAndLabelFramesPreservesCommittedState verifies dedicated Host frame variants.
func TestMapReplacementAndLabelFramesPreservesCommittedState(t *testing.T) {
	t.Parallel()
	// Arrange info, tree, and frames for mapFrame to verify dedicated Host frame variants.

	info := session.Info{
		ID: "replacement", Name: mo.None[string](), WorkingDirectory: "/project",
		StoragePath: mo.Some("/sessions/replacement.jsonl"), CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0),
	}
	tree := controllerui.SessionTree{Entries: nil, ActiveLeafID: mo.None[string]()}
	frames := []controllerui.Frame{
		replacementFrame(
			controllerui.FrameSessionForked,
			info,
			mo.Some("exact input"),
			mo.None[controllerui.SessionTree](),
		),
		replacementFrame(controllerui.FrameSessionCloned, info, mo.None[string](), mo.None[controllerui.SessionTree]()),
		replacementFrame(controllerui.FrameEntryLabelSet, session.Info{}, mo.None[string](), mo.Some(tree)),
	}

	// Act and assert each frame maps to its dedicated protobuf payload.
	// Act by invoking mapFrame to exercise dedicated Host frame variants.
	fork, err := mapFrame(frames[0])
	// Assert dedicated Host frame variants.
	require.NoError(t, err)
	require.Equal(t, "exact input", fork.GetEvent().GetCompleted().GetSessionForked().GetNextInput())
	require.Equal(t, "replacement", fork.GetEvent().GetCompleted().GetSessionForked().GetSession().GetInfo().GetId())
	clone, err := mapFrame(frames[1])
	require.NoError(t, err)
	require.Equal(t, "replacement", clone.GetEvent().GetCompleted().GetSessionCloned().GetSession().GetInfo().GetId())
	label, err := mapFrame(frames[2])
	require.NoError(t, err)
	require.NotNil(t, label.GetEvent().GetCompleted().GetEntryLabelSet().GetTree())
}

// replacementFrame creates one fully initialized public UI frame.
func replacementFrame(
	kind controllerui.FrameKind,
	info session.Info,
	nextInput mo.Option[string],
	tree mo.Option[controllerui.SessionTree],
) controllerui.Frame {
	return controllerui.Frame{
		Kind:      kind,
		NextInput: nextInput,

		Lifecycle:        mo.None[controllerui.Lifecycle](),
		AuthorizationURL: mo.None[string](),

		ModelSelection:    mo.None[model.Selection](),
		SessionInfo:       mo.Some(info),
		Sessions:          nil,
		SessionEntries:    nil,
		SessionStatistics: mo.None[session.Statistics](),
		SessionTree:       tree,
		TreeNavigation:    mo.None[controllerui.TreeNavigationResult](),
	}
}
