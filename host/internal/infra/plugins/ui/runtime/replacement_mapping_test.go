//go:build !integration

package runtime

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestMapReplacementAndLabelCommandsPreservesTypedArguments verifies all public UI command variants.
func TestMapReplacementAndLabelCommandsPreservesTypedArguments(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name           string
		set            func(*uipb.OpenResponse)
		expectedKind   domainui.CommandKind
		expectedTarget mo.Option[string]
		expectedLabel  mo.Option[string]
	}{
		{name: "fork", set: func(response *uipb.OpenResponse) {
			response.SetForkSession(uipb.ForkSessionCommand_builder{TargetEntryId: new("entry")}.Build())
		}, expectedKind: domainui.CommandForkSession, expectedTarget: mo.Some("entry"), expectedLabel: mo.None[string]()},
		{name: "clone", set: func(response *uipb.OpenResponse) {
			response.SetCloneSession(new(uipb.CloneSessionCommand))
		}, expectedKind: domainui.CommandCloneSession, expectedTarget: mo.None[string](), expectedLabel: mo.None[string]()},
		{name: "clear label", set: func(response *uipb.OpenResponse) {
			response.SetSetEntryLabel(uipb.SetEntryLabelCommand_builder{TargetEntryId: new("entry"), Label: new("")}.Build())
		}, expectedKind: domainui.CommandSetEntryLabel, expectedTarget: mo.Some("entry"), expectedLabel: mo.Some("")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one typed protobuf command.
			response := new(uipb.OpenResponse)
			test.set(response)

			// Act at the UI process boundary.
			command, err := mapCommand(response)

			// Assert optional values preserve presence and exact content.
			require.NoError(t, err)
			require.Equal(t, test.expectedKind, command.Kind)
			require.Equal(t, test.expectedTarget, command.TargetEntryID)
			require.Equal(t, test.expectedLabel, command.EntryLabel)
		})
	}
}

// TestMapReplacementAndLabelFramesPreservesCommittedState verifies dedicated Host frame variants.
func TestMapReplacementAndLabelFramesPreservesCommittedState(t *testing.T) {
	t.Parallel()

	info := session.Info{
		ID: "replacement", Name: mo.None[string](), WorkingDirectory: "/project",
		StoragePath: mo.Some("/sessions/replacement.jsonl"), CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0),
	}
	tree := domainui.SessionTree{Entries: nil, ActiveLeafID: mo.None[string]()}
	frames := []domainui.Frame{
		replacementFrame(domainui.FrameSessionForked, info, mo.Some("exact input"), mo.None[domainui.SessionTree]()),
		replacementFrame(domainui.FrameSessionCloned, info, mo.None[string](), mo.None[domainui.SessionTree]()),
		replacementFrame(domainui.FrameEntryLabelSet, session.Info{}, mo.None[string](), mo.Some(tree)),
	}

	// Act and assert each frame maps to its dedicated protobuf payload.
	fork, err := mapFrame(frames[0])
	require.NoError(t, err)
	require.Equal(t, "exact input", fork.GetSessionForked().GetNextInput())
	require.Equal(t, "replacement", fork.GetSessionForked().GetSession().GetInfo().GetId())
	clone, err := mapFrame(frames[1])
	require.NoError(t, err)
	require.Equal(t, "replacement", clone.GetSessionCloned().GetSession().GetInfo().GetId())
	label, err := mapFrame(frames[2])
	require.NoError(t, err)
	require.NotNil(t, label.GetEntryLabelSet().GetTree())
}

// replacementFrame creates one fully initialized public UI frame.
func replacementFrame(
	kind domainui.FrameKind,
	info session.Info,
	nextInput mo.Option[string],
	tree mo.Option[domainui.SessionTree],
) domainui.Frame {
	return domainui.Frame{
		Kind: kind, Initialization: mo.None[domainui.Initialization](), Lifecycle: mo.None[domainui.Lifecycle](),
		AuthorizationURL: mo.None[string](), Text: nextInput, RetryAuthentication: mo.None[bool](),
		ModelSelection: mo.None[domainui.ModelSelection](), SessionInfo: mo.Some(info), Sessions: nil,
		SessionEntries: nil, SessionStatistics: mo.None[session.Statistics](), SessionTree: tree,
		TreeNavigation: mo.None[domainui.TreeNavigationResult](), TreeFailure: mo.None[domainui.TreeFailure](),
	}
}
