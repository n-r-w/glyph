package programmatic

import (
	"context"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

// TestSessionTreeQueryReturnsCompleteSnapshot verifies the query returns the session-control tree unchanged.
func TestSessionTreeQueryReturnsCompleteSnapshot(t *testing.T) {
	t.Parallel()

	// Arrange strict dependencies and one tree with parent, label, extension metadata, and active leaf.
	mockController := gomock.NewController(t)
	coordinator := NewMockCoordinator(mockController)
	catalog := NewMockModelCatalog(mockController)
	control := NewMockSessionControl(mockController)
	tree := programmaticTree(t)
	control.EXPECT().Tree().Return(tree)
	service := New(coordinator, catalog, idleStateSnapshot, emptyHistorySnapshot, control, NewDelivery())
	command := treeCommand("tree", controller.CommandGetSessionTree)

	// Act by requesting the active-session tree.
	response, operation, err := service.Handle(t.Context(), command)

	// Assert the complete snapshot is returned without starting a run.
	require.NoError(t, err)
	require.Nil(t, operation)
	require.Equal(t, controller.ResponseSessionTree, response.Kind)
	mapped := response.SessionTree.MustGet()
	require.Equal(t, mo.Some("extension"), mapped.ActiveLeafID)
	require.Len(t, mapped.Entries, 3)
	require.Equal(t, mo.Some("root"), mapped.Entries[1].ParentID)
	require.Equal(t, "branch", mapped.Entries[1].Label)
	require.Equal(t, controller.ExtensionEntry{ExtensionID: "example", EntryType: "checkpoint"}, mapped.Entries[2].Extension.MustGet())
}

// TestNoSummaryNavigationReturnsCommittedState verifies navigation returns committed tree, transcript, and exact next input.
func TestNoSummaryNavigationReturnsCommittedState(t *testing.T) {
	t.Parallel()

	// Arrange strict dependencies and one committed user-target navigation result.
	mockController := gomock.NewController(t)
	coordinator := NewMockCoordinator(mockController)
	catalog := NewMockModelCatalog(mockController)
	control := NewMockSessionControl(mockController)
	tree := programmaticTree(t)
	committed := sessionnavigation.Result{
		Tree: tree, ActiveLeafID: mo.Some("root"), ActiveBranch: tree.ActiveBranch(), NextInput: mo.Some("exact input"),
	}
	control.EXPECT().Navigate(gomock.Any(), "user").Return(committed, nil)
	service := New(coordinator, catalog, idleStateSnapshot, emptyHistorySnapshot, control, NewDelivery())
	command := treeCommand("navigate", controller.CommandNavigateSessionTree)
	command.TargetEntryID = mo.Some("user")

	// Act by requesting no-summary navigation.
	response, operation, err := service.Handle(t.Context(), command)

	// Assert the result is committed and contains no speculative substitutions.
	require.NoError(t, err)
	require.Nil(t, operation)
	require.Equal(t, controller.ResponseSessionTreeNavigation, response.Kind)
	result := response.TreeNavigation.MustGet()
	require.Equal(t, controller.TreeNavigationStatusCommitted, result.Status)
	mapped := result.Committed.MustGet()
	require.Equal(t, committed.NextInput, mapped.NextInput)
	require.Equal(t, mo.Some("extension"), mapped.Tree.ActiveLeafID)
}

// TestCanceledNavigationReturnsCanceledWithoutState verifies context cancellation has no committed state.
func TestCanceledNavigationReturnsCanceledWithoutState(t *testing.T) {
	t.Parallel()

	// Arrange strict dependencies and a facade that reports cancellation before commit.
	mockController := gomock.NewController(t)
	coordinator := NewMockCoordinator(mockController)
	catalog := NewMockModelCatalog(mockController)
	control := NewMockSessionControl(mockController)
	control.EXPECT().Navigate(gomock.Any(), "user").Return(sessionnavigation.Result{}, context.Canceled)
	service := New(coordinator, catalog, idleStateSnapshot, emptyHistorySnapshot, control, NewDelivery())
	command := treeCommand("canceled", controller.CommandNavigateSessionTree)
	command.TargetEntryID = mo.Some("user")

	// Act by navigating with a client operation that ends before commit.
	response, operation, err := service.Handle(t.Context(), command)

	// Assert cancellation is a typed result with no speculative tree or input state.
	require.NoError(t, err)
	require.Nil(t, operation)
	result := response.TreeNavigation.MustGet()
	require.Equal(t, controller.TreeNavigationStatusCanceled, result.Status)
	require.True(t, result.Committed.IsNone())
}

// TestNavigationFailuresUseClosedCodes verifies invalid, missing, busy, and persistence failures return stable rejections.
func TestNavigationFailuresUseClosedCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		target        mo.Option[string]
		navigationErr error
		expected      controller.RejectionCode
	}{
		{name: "invalid", target: mo.None[string](), navigationErr: nil, expected: controller.RejectionInvalidArgument},
		{name: "missing", target: mo.Some("target"), navigationErr: session.ErrEntryNotFound, expected: controller.RejectionNotFound},
		{name: "busy", target: mo.Some("target"), navigationErr: session.ErrBusy, expected: controller.RejectionBusy},
		{name: "persistence", target: mo.Some("target"), navigationErr: session.ErrPersistenceUnavailable, expected: controller.RejectionPersistenceUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange strict dependencies and one navigation terminal path.
			mockController := gomock.NewController(t)
			coordinator := NewMockCoordinator(mockController)
			catalog := NewMockModelCatalog(mockController)
			control := NewMockSessionControl(mockController)
			if target, present := test.target.Get(); present {
				control.EXPECT().Navigate(gomock.Any(), target).Return(sessionnavigation.Result{}, test.navigationErr)
			}
			service := New(coordinator, catalog, idleStateSnapshot, emptyHistorySnapshot, control, NewDelivery())
			command := treeCommand(test.name, controller.CommandNavigateSessionTree)
			command.TargetEntryID = test.target

			// Act by requesting the failing navigation.
			response, operation, err := service.Handle(t.Context(), command)

			// Assert the operation does not expose speculative state and uses the closed code.
			require.NoError(t, err)
			require.Nil(t, operation)
			require.Equal(t, controller.ResponseRejected, response.Kind)
			require.Equal(t, test.expected, response.Rejection.MustGet().Code)
			require.True(t, response.SessionTree.IsNone())
			require.True(t, response.TreeNavigation.IsNone())
		})
	}
}

// treeCommand creates one argument-free tree command for focused tests.
func treeCommand(correlationID string, kind controller.CommandKind) controller.Command {
	return controller.Command{
		CorrelationID: correlationID, Kind: kind, UserText: mo.None[string](),
		ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](),
		ReasoningChoice: mo.None[model.ReasoningChoice](), SessionID: mo.None[session.ID](),
		SessionName: mo.None[string](), TargetEntryID: mo.None[string](),
		SummaryMode: controller.SummaryModeNoSummary, CustomFocus: mo.None[string](),
	}
}

// programmaticTree creates a tree that proves parent, label, active-leaf, and opaque extension projection.
func programmaticTree(t *testing.T) session.Tree {
	t.Helper()
	createdAt := time.Unix(1, 0).UTC()
	entries := []session.Entry{
		{
			ID: "root", ParentID: mo.None[string](), CreatedAt: createdAt,
			Information: mo.None[session.Information](), User: mo.Some(model.TextMessage("root input")),
			Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
			ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
			BranchSummary: mo.None[session.BranchSummaryEntry](),
		},
		{
			ID: "user", ParentID: mo.Some("root"), CreatedAt: createdAt.Add(time.Second),
			Information: mo.None[session.Information](), User: mo.Some(model.TextMessage("exact input")),
			Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
			ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
			BranchSummary: mo.None[session.BranchSummaryEntry](),
		},
		{
			ID: "extension", ParentID: mo.Some("user"), CreatedAt: createdAt.Add(2 * time.Second),
			Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
			Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
			ToolResult:    mo.None[session.ToolResult](),
			Extension:     mo.Some(session.ExtensionEnvelope{ExtensionID: "example", EntryType: "checkpoint", Data: []byte("private")}),
			BranchSummary: mo.None[session.BranchSummaryEntry](),
		},
	}
	tree, err := session.NewTree(entries, mo.Some("extension"), map[string]string{"user": "branch"})
	require.NoError(t, err)
	return tree
}
