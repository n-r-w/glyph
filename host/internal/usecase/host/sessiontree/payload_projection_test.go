//go:build !integration

package sessiontree

import (
	"context"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

// TestNavigateProjectsExtensionEntriesWithoutPayload verifies handler requests expose extension identity only.
func TestNavigateProjectsExtensionEntriesWithoutPayload(t *testing.T) {
	t.Parallel()

	// Arrange an abandoned extension entry with opaque payload bytes and one request handler.
	controller := gomock.NewController(t)
	active := NewMockActiveSession(controller)
	models := NewMockModelRequester(controller)
	handlers := NewMockRuntime(controller)
	service := New(active, models, handlers)
	tree := navigationTree(t, time.Unix(1, 0).UTC())
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	handler := Handler{ExtensionID: "extension", HandlerID: "inspect"}
	active.EXPECT().Tree().Return(tree)
	active.EXPECT().SessionID().Return("session")
	models.EXPECT().ActiveSelection().Return(selection)
	registerTestHandlers(service, handlers, HandlerKindRequest, []Handler{handler})
	handlers.EXPECT().HandleHandler(
		gomock.Any(), handler.ExtensionID, handler.HandlerID, gomock.Any(),
	).DoAndReturn(func(
		_ context.Context,
		_, _ string,
		request HandlerRequest,
	) (HandlerResponse, error) {
		invocation := request.Request.OrEmpty()
		for _, entry := range invocation.Original.Preparation.AbandonedPath {
			if extension, present := entry.Extension.Get(); present {
				assert.Equal(t, "extension", extension.ExtensionID)
				assert.Equal(t, "state", extension.EntryType)
				assert.Empty(t, extension.Data)
			}
		}
		return HandlerResponse{
			Request: mo.Some(RequestHandlerAction{
				Cancel: false, RequestAction: RequestActionPreserve,
				Request: mo.None[HandlerNavigationRequest](), ResultAction: ResultActionPreserve,
				Result: mo.None[HandlerBranchSummaryResult](),
			}),
			Result: mo.None[ResultHandlerAction](), Observer: mo.None[ObserverAction](),
		}, nil
	})
	committed := tree.Clone()
	require.NoError(t, committed.SetActiveLeaf(mo.Some("root")))
	active.EXPECT().CommitNavigation(gomock.Any(), CommitCommand{
		ExpectedActiveLeafID: mo.Some("active"), DestinationID: mo.Some("root"),
		BranchSummary: mo.None[BranchSummaryDraft](),
	}).Return(committed, nil)

	// Act by navigating away from the entry that owns opaque extension data.
	_, err := service.NavigateTree(t.Context(), sessionnavigation.Request{
		TargetEntryID: "user", SummaryMode: sessionnavigation.SummaryModeNoSummary,
		CustomFocus: mo.None[string](),
	})

	// Assert handler projection strips payload bytes while navigation still commits.
	require.NoError(t, err)
	entries := tree.Entries()
	require.Len(t, entries, 4)
	assert.Equal(t, []byte(`{}`), entries[2].Extension.OrEmpty().Data)
}
