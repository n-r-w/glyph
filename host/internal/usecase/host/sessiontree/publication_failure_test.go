//go:build !integration

package sessiontree

import (
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// TestNavigationPublicationFailureStillObservesCommit verifies mandatory observation and joined original causes.
func TestNavigationPublicationFailureStillObservesCommit(t *testing.T) {
	t.Parallel()
	// Arrange a persisted root navigation whose publication and first observer both fail.
	control := gomock.NewController(t)
	active := NewMockActiveSession(control)
	runtime := NewMockRuntime(control)
	selection := NewMockModelSelection(control)
	service := New(active, selection, NewMockModelRequester(control), runtime)
	tree := navigationTree(t, time.Unix(1, 0).UTC())
	active.EXPECT().Tree().Return(tree, nil)
	active.EXPECT().SessionID().Return("session")
	selection.EXPECT().ActiveSelection().Return(model.Selection{})
	committed, err := tree.Clone()
	require.NoError(t, err)
	require.NoError(t, committed.SetActiveLeaf(mo.None[string]()))
	publication := errors.New("original publication failure: delivery closed")
	observer := errors.New("original postcommit observer failure: nested child cause")
	first := Handler{ExtensionID: "extension", HandlerID: "first"}
	later := Handler{ExtensionID: "extension", HandlerID: "later"}
	registerTestHandlers(service, runtime, HandlerKindObserver, []Handler{first, later})
	gomock.InOrder(
		active.EXPECT().
			CommitNavigation(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(NavigationCommit{Committed: true, Tree: committed, CreatedSummary: mo.None[session.Entry]()}, publication),
		expectObserver(runtime, first, gomock.Any(), observer),
	)

	// Act through the public navigation producer after persistence succeeds.
	result, failure := service.NavigateUI(
		t.Context(),
		ui.NavigationIntent{
			TargetEntryID: "root",
			SummaryMode:   controllerui.SummaryModeNoSummary,
			CustomFocus:   mo.None[string](),
		},
		func(session.Tree) error { return nil },
		nil,
	)

	// Assert mandatory observation runs, later observation stops, and both causes retain identity with root metadata.
	require.ErrorIs(t, failure, publication)
	require.ErrorIs(t, failure, observer)
	require.ErrorContains(t, failure, publication.Error())
	require.ErrorContains(t, failure, observer.Error())
	require.True(t, result.Committed.IsSome())
	require.True(t, result.Committed.MustGet().ActiveLeafID.IsNone())
	require.Equal(t, mo.Some("root input"), result.Committed.MustGet().NextInput)
}
