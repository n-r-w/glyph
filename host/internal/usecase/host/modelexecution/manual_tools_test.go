//go:build !integration

package modelexecution

import (
	"context"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// TestManualCompactionUsesCurrentToolSnapshot verifies that manual preparation captures the current filtered tool list.
func TestManualCompactionUsesCurrentToolSnapshot(t *testing.T) {
	t.Parallel()
	// Arrange the same tool port used for conversation tool selection, returning successive snapshots.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	tools := NewMockManualCompactionTools(controller)
	preparation := NewMockManualCompactionPreparation(controller)
	service := New(catalog, RetryPolicy{}, nil, nil, nil)
	service.BindManualCompaction(preparation, tools, "")
	catalog.EXPECT().ActiveBinding().Return(CatalogBinding{}).Times(2)
	selected := []tool.Descriptor{
		{
			Name:                "read",
			Description:         "Read.",
			InputSchemaJSON:     nil,
			ConstrainedSampling: mo.None[tool.ConstrainedSampling](),
			ReplacesTool:        false, Concurrent: false,
		},
	}
	gomock.InOrder(tools.EXPECT().Tools().Return(selected), tools.EXPECT().Tools().Return(nil))
	var snapshots [][]tool.Descriptor
	preparation.EXPECT().CompactManualPolicy(gomock.Any(), gomock.Any(), mo.None[string]()).DoAndReturn(
		func(_ context.Context, input ProviderRequest, _ mo.Option[string]) (ManualCompactionResult, error) {
			snapshots = append(snapshots, input.Tools)
			return ManualCompactionResult{}, nil
		}).Times(2)
	// Act by preparing manual compaction before and after selection changes.
	_, firstErr := service.compactManualPolicy(t.Context(), mo.None[string]())
	_, secondErr := service.compactManualPolicy(t.Context(), mo.None[string]())
	// Assert each request receives its own current tool snapshot, including an empty selection.
	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	require.Len(t, snapshots, 2)
	assert.Equal(t, selected, snapshots[0])
	assert.Empty(t, snapshots[1])
}
