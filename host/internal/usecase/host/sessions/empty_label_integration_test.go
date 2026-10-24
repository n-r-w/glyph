//go:build integration

package sessions

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestEmptyLabelDeletesPersistedLabel verifies persisted deletion removes active and returned labels.
func TestEmptyLabelDeletesPersistedLabel(t *testing.T) {
	t.Parallel()
	// Arrange a real labeled session tree and a successful persistence boundary.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	service := New(
		repository,
		NewMockIDGenerator(controller),
		NewMockClock(controller),
		NewMockPricingCatalog(controller),
		"/project",
	)
	entry := session.Entry{
		ID:               "target",
		ParentID:         mo.None[string](),
		CreatedAt:        time.Time{},
		Information:      mo.None[session.Information](),
		User:             mo.Some(model.Message{}),
		Model:            mo.None[session.ModelResponse](),
		ToolResult:       mo.None[session.ToolResult](),
		Extension:        mo.None[session.ExtensionEnvelope](),
		EstimatedCost:    mo.None[session.EstimatedCost](),
		BranchSummary:    mo.None[session.BranchSummaryEntry](),
		Compaction:       mo.None[session.CompactionEntry](),
		ExtensionMessage: mo.None[session.ExtensionMessage](),
	}
	tree, err := session.NewTree([]session.Entry{entry}, mo.Some("target"), map[string]string{"target": "branch"})
	require.NoError(t, err)
	service.active = LoadedSession{
		Header:               session.Header{},
		StoragePath:          "/sessions/source.jsonl",
		Tree:                 tree,
		Information:          mo.None[session.Information](),
		InformationUpdatedAt: mo.None[time.Time](),
	}
	repository.EXPECT().
		Apply(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, command ApplyCommand) (ApplyResult, error) {
			require.Equal(t, LabelMutation{TargetID: "target", Label: ""}, command.Mutation.Label.MustGet())
			return ApplyResult{StoragePath: "/sessions/source.jsonl"}, nil
		})

	// Act through the session label mutation with a present empty label value.
	result, err := service.SetLabel(t.Context(), "target", "")

	// Assert the persisted deletion is reflected in both the caller snapshot and active state.
	require.NoError(t, err)
	require.Empty(t, result.Labels())
	require.Empty(t, service.active.Tree.Labels())
}
