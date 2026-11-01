package runtime

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/middleware"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// HandleToolPolicy maps one tool-policy handler over the existing accepted operation stream.
func (r *Runtime) HandleToolPolicy(
	ctx context.Context,
	handlerID string,
	input middleware.Transformation[model.ToolCall],
) (middleware.ToolPolicyAction, error) {
	var action middleware.ToolPolicyAction
	err := r.handleMiddleware(
		ctx,
		mapToolPolicyRequest(handlerID, input),
		func(response *extensionpb.HandleResponse) error {
			action = mapToolPolicyAction(response)
			return nil
		},
	)
	return action, err
}

// mapToolPolicyRequest converts detached owner input into its public payload.
func mapToolPolicyRequest(
	handlerID string,
	input middleware.Transformation[model.ToolCall],
) *extensionpb.HandleRequest {
	request := new(extensionpb.HandleRequest)
	request.SetHandlerId(handlerID)
	request.SetContext(mapContext(input.Context))
	request.SetToolPolicy(extensionpb.ToolPolicyInvocation_builder{
		Original: mapPolicyToolCall(input.Original), Current: mapPolicyToolCall(input.Current),
	}.Build())
	return request
}

// mapPolicyToolCall converts one model tool call into its public form.
func mapPolicyToolCall(call model.ToolCall) *extensionpb.SessionTreeToolCall {
	return extensionpb.SessionTreeToolCall_builder{
		Id: new(call.ID), Name: new(call.Name), ArgumentsJson: call.Arguments.Bytes(),
	}.Build()
}

// mapToolPolicyAction decodes the selected action and leaves a missing or false action invalid for the owner.
func mapToolPolicyAction(response *extensionpb.HandleResponse) middleware.ToolPolicyAction {
	value := response.GetToolPolicy()
	switch {
	case value.GetAllow():
		return middleware.ToolPolicyAction{Kind: middleware.Preserve, Arguments: nil, Reason: ""}
	case value.HasReplaceArgumentsJson():
		return middleware.ToolPolicyAction{
			Kind: middleware.Replace, Arguments: value.GetReplaceArgumentsJson(), Reason: "",
		}
	case value.HasRejectReason():
		return middleware.ToolPolicyAction{Kind: middleware.Reject, Arguments: nil, Reason: value.GetRejectReason()}
	default:
		return middleware.ToolPolicyAction{}
	}
}
