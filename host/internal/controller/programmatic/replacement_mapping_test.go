package programmatic

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestMapReplacementAndLabelResponsesPreservesTypedCommittedState verifies public result variants.
func TestMapReplacementAndLabelResponsesPreservesTypedCommittedState(t *testing.T) {
	t.Parallel()

	tree, err := session.NewTree(nil, mo.None[string](), nil)
	require.NoError(t, err)
	replacement := session.Replacement{Info: session.Info{
		ID: "replacement", Name: mo.None[string](), WorkingDirectory: "/project",
		StoragePath: mo.Some("/sessions/replacement.jsonl"), CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0),
	}, Entries: nil}
	for _, test := range []struct {
		name     string
		response Response
		assert   func(*testing.T, any)
	}{
		{
			name: "fork", response: replacementMappingResponse(ResponseForkSession, mo.Some(SessionReplacement{Info: replacement.Info, ActiveBranch: nil, NextInput: mo.Some("exact input")}), mo.None[SessionTree]()),
			assert: func(t *testing.T, value any) {
				wire := value.(*ResponseWire)
				require.Equal(t, "exact input", wire.ForkInput)
				require.Equal(t, "replacement", wire.SessionID)
			},
		},
		{
			name: "clone", response: replacementMappingResponse(ResponseCloneSession, mo.Some(SessionReplacement{Info: replacement.Info, ActiveBranch: nil, NextInput: mo.None[string]()}), mo.None[SessionTree]()),
			assert: func(t *testing.T, value any) {
				wire := value.(*ResponseWire)
				require.Equal(t, "replacement", wire.SessionID)
			},
		},
		{
			name: "label", response: replacementMappingResponse(ResponseSetEntryLabel, mo.None[SessionReplacement](), mo.Some(SessionTree{Entries: nil, ActiveLeafID: tree.ActiveLeafID()})),
			assert: func(t *testing.T, value any) {
				wire := value.(*ResponseWire)
				require.True(t, wire.HasLabelTree)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Act by mapping one committed controller response.
			wire, err := mapResponse(test.response)

			// Assert the dedicated protobuf variant contains committed state.
			require.NoError(t, err)
			view := &ResponseWire{}
			switch test.name {
			case "fork":
				view.SessionID = wire.GetCommandResponse().GetForkSession().GetInfo().GetId()
				view.ForkInput = wire.GetCommandResponse().GetForkSession().GetNextInput()
			case "clone":
				view.SessionID = wire.GetCommandResponse().GetCloneSession().GetInfo().GetId()
			case "label":
				view.HasLabelTree = wire.GetCommandResponse().GetSetEntryLabel().HasTree()
			default:
				t.Fatalf("unexpected test case %q", test.name)
			}
			test.assert(t, view)
		})
	}
}

// ResponseWire contains only assertions shared by public replacement response variants.
type ResponseWire struct {
	// SessionID contains the mapped replacement identity.
	SessionID string
	// ForkInput contains exact editable input for fork results.
	ForkInput string
	// HasLabelTree reports whether the label result contains committed tree state.
	HasLabelTree bool
}

// replacementMappingResponse creates one fully initialized controller response.
func replacementMappingResponse(
	kind ResponseKind,
	replacement mo.Option[SessionReplacement],
	tree mo.Option[SessionTree],
) Response {
	return Response{
		CorrelationID: "correlation", Kind: kind, State: mo.None[RunStateResult](), Messages: nil,
		Models: mo.None[ModelsResult](), Selection: mo.None[model.Selection](), SessionInfo: mo.None[session.Info](),
		Sessions: nil, SessionEntries: nil, SessionStatistics: mo.None[session.Statistics](), SessionTree: tree,
		TreeNavigation: mo.None[TreeNavigationResult](), Replacement: replacement,
		Rejection: mo.None[Rejection](),
	}
}
