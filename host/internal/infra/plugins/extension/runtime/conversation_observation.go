package runtime

import (
	"context"
	"errors"
	"fmt"

	controllerextension "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// ObserveConversation delivers the completed attempt to the captured owner.
func (r *Runtime) ObserveConversation(
	ctx context.Context,
	handlerID string,
	binding extension.Context,
	invocation extensioncontext.ConversationObservation,
) error {
	history, err := controllerextension.MapCompactionHistory(invocation.Input.History)
	if err != nil {
		return fmt.Errorf("map conversation observation history: %w", err)
	}
	tools, err := mapPolicyTools(invocation.Input.Tools)
	if err != nil {
		return fmt.Errorf("map conversation observation tools: %w", err)
	}
	result, err := controllerextension.MapModelResponse(invocation.Response)
	if err != nil {
		return fmt.Errorf("map conversation observation response: %w", err)
	}
	input := extensionpb.CompletedConversationObservation_builder{
		Model: mapCompactionModel(
			invocation.Input.Model,
		),
		ReasoningChoice:             new(string(invocation.Input.ReasoningChoice)),
		Instructions:                new(invocation.Input.Instructions),
		History:                     history,
		Tools:                       tools,
		Response:                    result,
		ResponsePrivatePayloadBytes: new(controllerextension.CountCompactionPrivateBytes(invocation.Response)),
	}.Build()
	request := new(extensionpb.HandleRequest)
	request.SetHandlerId(handlerID)
	request.SetContext(mapContext(binding))
	request.SetParentCompactionId(invocation.LifecycleID)
	request.SetCompletedConversation(input)
	response, err := r.runCompactionHandler(ctx, handlerID, request)
	if response == nil || response.GetError() != nil {
		return err
	}
	if response.GetCompletedConversation() == nil {
		return errors.Join(err, r.protocolViolation(errors.New("completed conversation acknowledgement is missing")))
	}
	return err
}
