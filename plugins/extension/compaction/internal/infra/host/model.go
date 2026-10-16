package host

import (
	"context"
	"errors"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	controller "github.com/n-r-w/glyph/plugins/extension/compaction/internal/controller/extension"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/usecase/generation"
	sdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// Model supplies invocation-scoped configured-model access.
type Model struct{}

var _ generation.Model = (*Model)(nil)

// Request makes a configured-model request.
func (m *Model) Request(ctx context.Context, input generation.Input) (generation.Response, error) {
	binding, err := sdk.ContextFrom(ctx)
	if err != nil {
		return generation.Response{}, err
	}
	content := new(pb.ConfiguredModelInputContent)
	content.SetText(input.Source)
	request := pb.ConfiguredModelRequest_builder{
		Context: nil,
		Selection: pb.ModelSelection_builder{
			ProviderId:      new(input.Model.Provider),
			ModelId:         new(input.Model.ID),
			ReasoningChoice: new(input.Reasoning),
		}.Build(),
		Instructions: new(input.Instructions),
		Messages: []*pb.ConfiguredModelMessage{
			pb.ConfiguredModelMessage_builder{
				Role:    new(pb.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_USER),
				Content: []*pb.ConfiguredModelInputContent{content},
			}.Build(),
		},
	}.Build()
	operation, err := binding.StartConfiguredModel(ctx, request)
	if err != nil {
		return generation.Response{}, err
	}
	response, err := operation.Wait(ctx)
	if err != nil {
		return generation.Response{}, err
	}
	if response == nil {
		return generation.Response{}, errors.New("configured summary response is required")
	}
	return decodeModelResponse(response), nil
}

// decodeModelResponse converts a configured-model result to a generation response.
func decodeModelResponse(response *pb.ConfiguredModelResult) generation.Response {
	mapped := generation.Response{
		Outcome: "",
		Blocks:  make([]compaction.Block, 0, len(response.GetContent())),
		Error:   response.GetErrorMessage(),
		Usage:   controller.DecodeUsage(response.GetUsage()),
	}
	switch response.GetOutcome() {
	case pb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_UNSPECIFIED:
	case pb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_STOP:
		mapped.Outcome = generation.OutcomeStop
	case pb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_LENGTH:
		mapped.Outcome = generation.OutcomeLength
	case pb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_TOOL_USE:
		mapped.Outcome = generation.OutcomeToolUse
	case pb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_ABORTED:
		mapped.Outcome = generation.OutcomeAborted
	case pb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_FAILED:
		mapped.Outcome = generation.OutcomeFailed
	}
	for _, content := range response.GetContent() {
		block := compaction.Block{Kind: "", Text: "", MediaType: "", Data: nil, CallID: "", Name: "", Arguments: nil}
		switch {
		case content.GetText() != nil:
			block.Kind = compaction.Text
			block.Text = content.GetText().GetText()
		case content.GetRefusal() != nil:
			block.Kind = compaction.Refusal
			block.Text = content.GetRefusal().GetText()
		case content.GetReasoning() != nil:
			block.Kind = compaction.Thought
			block.Text = content.GetReasoning().GetText()
		case content.GetToolCall() != nil:
			block.Kind = compaction.Call
		}
		mapped.Blocks = append(mapped.Blocks, block)
	}
	return mapped
}
