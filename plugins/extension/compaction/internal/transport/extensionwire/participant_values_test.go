//go:build !integration

package extensionwire

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// TestParticipantMessagesDetachAcceptedValues checks mutable participant payload ownership.
func TestParticipantMessagesDetachAcceptedValues(t *testing.T) {
	t.Parallel()
	// Arrange captured request and accepted result values with mutable collections.
	request := DecodeRequest(newMappingRequest())
	request.Model.Modalities = []compaction.Modality{compaction.ModalityText}
	request.Model.Reasoning = mo.Some(compaction.Reasoning{Supported: true, Choices: []string{"off"}, Default: "off"})
	request.Model.Pricing = mo.Some(
		compaction.Pricing{
			Rates: [4]float64{1, 2, 3, 4},
			Tiers: []compaction.Tier{{Above: 10, Rates: [4]float64{1, 2, 3, 4}}},
		},
	)
	request.Prefix[0].Kind = compaction.User
	request.Prefix[0].Blocks = []compaction.Block{newImageBlock("image/png", []byte{1, 2})}
	candidate := compaction.Result{
		Summary:   "accepted summary",
		FirstKept: "suffix",
		Extension: mo.None[string](),
		Requests: []compaction.ModelRequest{
			{
				Provider:  "provider",
				Model:     "model",
				Reasoning: "off",
				Usage:     mo.Some(compaction.Usage{Counts: [6]int64{1, 1, 0, 0, 0, 2}}),
				Cost:      mo.None[[5]float64](),
			},
		},
		Details: mo.Some([]byte{3, 4}),
	}
	state := compaction.State{Original: request, Current: request, Result: mo.Some(candidate)}
	// Act by mutating detached outbound and inbound participant messages.
	invocation := EncodeInvocation(state)
	invocation.GetOriginal().GetModel().GetReasoning().GetChoices()[0] = "participant mutation"
	invocation.GetCurrent().GetModel().GetInputModalities()[0] = pb.InputModality_INPUT_MODALITY_IMAGE
	invocation.GetOriginal().GetModel().GetPricing().GetTiers()[0].SetInput(99)
	invocation.GetCurrent().GetPrefix()[0].GetEntry().GetUser().GetContent()[0].GetImage().GetData()[0] = 99
	invocation.GetCurrentResult().GetDetails()[0] = 99
	invocation.GetCurrentResult().GetSource().GetModelRequests().GetRequests()[0].GetSelection().
		SetProviderId("participant mutation")
	wireResult := EncodeResult(candidate)
	decoded := DecodeResult(wireResult)
	wireResult.GetDetails()[0] = 99
	wireResult.GetSource().GetModelRequests().GetRequests()[0].GetUsage().SetInputTokens(99)
	// Assert that after transport mutations, captured input and the accepted candidate remain unchanged.
	require.Equal(t, []string{"off"}, state.Original.Model.Reasoning.MustGet().Choices)
	require.Equal(t, []compaction.Modality{compaction.ModalityText}, state.Current.Model.Modalities)
	require.InDelta(t, float64(1), state.Original.Model.Pricing.MustGet().Tiers[0].Rates[0], 0)
	require.Equal(t, []byte{1, 2}, state.Current.Prefix[0].Blocks[0].Data)
	require.Equal(t, []byte{3, 4}, candidate.Details.MustGet())
	require.Equal(t, "provider", candidate.Requests[0].Provider)
	require.Equal(t, []byte{3, 4}, decoded.Details.MustGet())
	require.Equal(t, int64(1), decoded.Requests[0].Usage.MustGet().Counts[0])
}
