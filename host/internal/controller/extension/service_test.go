//go:build !integration

package extension

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestModelCatalogueMapsCompleteDescriptor verifies full neutral capability and active-selection projection.
func TestModelCatalogueMapsCompleteDescriptor(t *testing.T) {
	t.Parallel()

	// Arrange: provide all neutral descriptor fields through generated consumer-interface mocks.
	controller := gomock.NewController(t)
	contexts := NewMockContextOperations(controller)
	runtime := NewMockRuntimeOperations(controller)
	reference := extensiondomain.ContextRef{ID: "context", RuntimeInstanceID: "runtime", SessionID: "session"}
	contexts.EXPECT().ValidateContext("extension", "runtime", reference).Return(nil)
	released := false
	runtime.EXPECT().BeginContextOperation(gomock.Any(), "extension", "runtime").Return(func() { released = true }, nil)
	descriptor := model.Descriptor{
		Provider:      "provider",
		Model:         "model",
		Input:         []model.InputModality{model.InputModalityText, model.InputModalityImage},
		ContextWindow: 200000,
		MaxTokens:     32000,
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: true,
			Choices:   []model.ReasoningChoice{model.ReasoningChoiceOff, model.ReasoningChoiceHigh},
			Default:   model.ReasoningChoiceHigh,
		},
		ToolCapabilities: model.ToolCapabilities{
			StrictJSONSchema: true,
			Grammar:          model.GrammarCapabilities{Lark: true, Regex: true},
		},
		Pricing: mo.Some(
			model.Pricing{Input: 1.5, Output: 6, CacheRead: 0.25, CacheWrite: 2, Tiers: []model.PricingTier{
				{InputTokensAbove: 100000, Input: 3, Output: 12, CacheRead: 0.5, CacheWrite: 4},
			}},
		),
	}
	contexts.EXPECT().ReadModels(gomock.Any(), "extension", "runtime", reference).Return(ModelCatalog{
		Models: []model.Descriptor{
			descriptor,
		},
		Selection: model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh},
	}, nil)
	service := New(contexts, runtime, "extension", "runtime")
	request := new(extensionpb.ExtensionRequest)
	request.SetGetModels(extensionpb.GetModelsRequest_builder{Context: extensionpb.ExtensionContextRef_builder{
		ContextId: new("context"), RuntimeInstanceId: new("runtime"), SessionId: new("session"),
	}.Build()}.Build())

	// Act: admit, run, and release one catalog read.
	prepared, err := service.Prepare(t.Context(), "operation", request)
	require.NoError(t, err)
	result, err := prepared.Run(t.Context())
	require.NoError(t, err)
	prepared.Release()

	// Assert: every descriptor field and selected value crosses the public contract.
	require.True(t, released)
	catalog := result.GetGetModels()
	require.Len(t, catalog.GetModels(), 1)
	mapped := catalog.GetModels()[0]
	assert.Equal(t, "provider", mapped.GetProviderId())
	assert.Equal(t, "model", mapped.GetModelId())
	assert.Equal(
		t,
		[]extensionpb.InputModality{
			extensionpb.InputModality_INPUT_MODALITY_TEXT,
			extensionpb.InputModality_INPUT_MODALITY_IMAGE,
		},
		mapped.GetInputModalities(),
	)
	assert.Equal(t, int64(200000), mapped.GetContextWindow())
	assert.Equal(t, int64(32000), mapped.GetMaxTokens())
	assert.True(t, mapped.GetReasoning().GetSupported())
	assert.Equal(t, []string{"off", "high"}, mapped.GetReasoning().GetChoices())
	assert.Equal(t, "high", mapped.GetReasoning().GetDefaultChoice())
	assert.True(t, mapped.GetTools().GetStrictJsonSchema())
	assert.True(t, mapped.GetTools().GetLark())
	assert.True(t, mapped.GetTools().GetRegex())
	assert.Equal(t, 1.5, mapped.GetPricing().GetInput())
	assert.Equal(t, 6.0, mapped.GetPricing().GetOutput())
	assert.Equal(t, 0.25, mapped.GetPricing().GetCacheRead())
	assert.Equal(t, 2.0, mapped.GetPricing().GetCacheWrite())
	require.Len(t, mapped.GetPricing().GetTiers(), 1)
	tier := mapped.GetPricing().GetTiers()[0]
	assert.Equal(t, int64(100000), tier.GetInputTokensAbove())
	assert.Equal(t, 3.0, tier.GetInput())
	assert.Equal(t, 12.0, tier.GetOutput())
	assert.Equal(t, 0.5, tier.GetCacheRead())
	assert.Equal(t, 4.0, tier.GetCacheWrite())
	assert.Equal(t, "provider", catalog.GetActiveSelection().GetProviderId())
	assert.Equal(t, "model", catalog.GetActiveSelection().GetModelId())
	assert.Equal(t, "high", catalog.GetActiveSelection().GetReasoningChoice())
}
