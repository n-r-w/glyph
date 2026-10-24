//go:build !integration

package sessions

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestBindPricingCatalogValidatesBeforeChangingBinding verifies failed binding preserves subsequent use.
func TestBindPricingCatalogValidatesBeforeChangingBinding(t *testing.T) {
	t.Parallel()
	// Arrange an unbound pricing owner and two catalogs.
	controller := gomock.NewController(t)
	catalog := NewMockPricingCatalog(controller)
	service := New(nil, nil, nil, nil, "")
	// Act with an absent catalog followed by valid initialization.
	require.Error(t, service.BindPricingCatalog(nil))
	require.NoError(t, service.BindPricingCatalog(catalog))
	// Assert repeated binding retains the original catalog.
	require.Error(t, service.BindPricingCatalog(NewMockPricingCatalog(controller)))
	catalog.EXPECT().Pricing(model.ProviderID("provider"), model.ID("model")).Return(mo.None[model.Pricing]())
	cost := service.estimatedUsageCost("provider", "model", mo.Some(session.TokenUsage{
		InputTokens: 1, OutputTokens: 0, CacheReadTokens: 0, CacheWriteTokens: 0, ReasoningTokens: 0, TotalTokens: 1,
	}))
	require.True(t, cost.IsNone())
}
