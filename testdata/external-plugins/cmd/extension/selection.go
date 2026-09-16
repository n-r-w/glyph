package main

import (
	"errors"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	// selectionCompositionEnvironment enables ordered selection transformations in the external fixture.
	selectionCompositionEnvironment = "GLYPH_EXTERNAL_SELECTION_COMPOSITION"
	// modelSelectionFirstHandlerID identifies the first composing model handler.
	modelSelectionFirstHandlerID = "compose-model-first"
	// modelSelectionSecondHandlerID identifies the second composing model handler.
	modelSelectionSecondHandlerID = "compose-model-second"
	// reasoningSelectionFirstHandlerID identifies the first composing reasoning handler.
	reasoningSelectionFirstHandlerID = "compose-reasoning-first"
	// reasoningSelectionSecondHandlerID identifies the second composing reasoning handler.
	reasoningSelectionSecondHandlerID = "compose-reasoning-second"
	// selectionProviderID is the provider expected by public composition scenarios.
	selectionProviderID = "openai-codex"
	// selectionModelID is the model expected by public composition scenarios.
	selectionModelID = "gpt-test"
	// selectionOriginalReasoning is the immutable requested reasoning target.
	selectionOriginalReasoning = "off"
	// selectionIntermediateReasoning is the target produced by the first handler.
	selectionIntermediateReasoning = "low"
	// selectionFinalReasoning is the target produced by the second handler.
	selectionFinalReasoning = "high"
)

// selectionHandlerDescriptors returns preserve handlers or the enabled composing handler chains.
func selectionHandlerDescriptors(composition bool) []*extensionv1.HandlerDescriptor {
	if !composition {
		return []*extensionv1.HandlerDescriptor{
			extensionv1.HandlerDescriptor_builder{
				Id: new(modelSelectionHandlerID), Kind: new(extensionv1.HandlerKind_HANDLER_KIND_MODEL_SELECTION),
			}.Build(),
			extensionv1.HandlerDescriptor_builder{
				Id:   new(reasoningSelectionHandlerID),
				Kind: new(extensionv1.HandlerKind_HANDLER_KIND_REASONING_SELECTION),
			}.Build(),
		}
	}
	return []*extensionv1.HandlerDescriptor{
		extensionv1.HandlerDescriptor_builder{
			Id: new(modelSelectionFirstHandlerID), Kind: new(extensionv1.HandlerKind_HANDLER_KIND_MODEL_SELECTION),
		}.Build(),
		extensionv1.HandlerDescriptor_builder{
			Id: new(modelSelectionSecondHandlerID), Kind: new(extensionv1.HandlerKind_HANDLER_KIND_MODEL_SELECTION),
		}.Build(),
		extensionv1.HandlerDescriptor_builder{
			Id:   new(reasoningSelectionFirstHandlerID),
			Kind: new(extensionv1.HandlerKind_HANDLER_KIND_REASONING_SELECTION),
		}.Build(),
		extensionv1.HandlerDescriptor_builder{
			Id:   new(reasoningSelectionSecondHandlerID),
			Kind: new(extensionv1.HandlerKind_HANDLER_KIND_REASONING_SELECTION),
		}.Build(),
	}
}

// isSelectionHandlerID reports whether an identifier belongs to the fixture's selection handlers.
func isSelectionHandlerID(handlerID string) bool {
	switch handlerID {
	case modelSelectionHandlerID, reasoningSelectionHandlerID,
		modelSelectionFirstHandlerID, modelSelectionSecondHandlerID,
		reasoningSelectionFirstHandlerID, reasoningSelectionSecondHandlerID:
		return true
	default:
		return false
	}
}

// runSelectionHandler validates original/current composition and returns the next complete target.
func (operation *handleOperation) runSelectionHandler() *extensionv1.HandleResponse {
	invocation := operation.request.GetModelSelection()
	modelHandler := invocation != nil
	if invocation == nil {
		invocation = operation.request.GetReasoningSelection()
	}
	handlerID := operation.request.GetHandlerId()
	if handlerID == modelSelectionHandlerID || handlerID == reasoningSelectionHandlerID {
		return selectionActionResponse(modelHandler, extensionv1.SelectionHandlerAction_builder{
			Preserve: new(extensionv1.PreserveSelection), Replace: nil, Reject: nil,
		}.Build())
	}
	currentReasoning := selectionOriginalReasoning
	replacementReasoning := selectionIntermediateReasoning
	if handlerID == modelSelectionSecondHandlerID || handlerID == reasoningSelectionSecondHandlerID {
		currentReasoning = selectionIntermediateReasoning
		replacementReasoning = selectionFinalReasoning
	}
	if err := validateSelectionInvocation(invocation, currentReasoning); err != nil {
		response := new(extensionv1.HandleResponse)
		response.SetError(extensionv1.HandlerError_builder{Message: new(err.Error())}.Build())
		return response
	}
	action := extensionv1.SelectionHandlerAction_builder{
		Preserve: nil,
		Replace: extensionv1.ModelSelection_builder{
			ProviderId: new(selectionProviderID), ModelId: new(selectionModelID),
			ReasoningChoice: new(replacementReasoning),
		}.Build(),
		Reject: nil,
	}.Build()
	return selectionActionResponse(modelHandler, action)
}

// validateSelectionInvocation verifies immutable original and the expected composed current target.
func validateSelectionInvocation(invocation *extensionv1.SelectionHandlerInvocation, currentReasoning string) error {
	if invocation == nil || invocation.GetOriginal() == nil || invocation.GetCurrent() == nil {
		return errors.New("selection handler invocation is incomplete")
	}
	original := invocation.GetOriginal()
	current := invocation.GetCurrent()
	if original.GetProviderId() != selectionProviderID || original.GetModelId() != selectionModelID ||
		original.GetReasoningChoice() != selectionOriginalReasoning {
		return errors.New("selection handler original target is unexpected")
	}
	if current.GetProviderId() != selectionProviderID || current.GetModelId() != selectionModelID ||
		current.GetReasoningChoice() != currentReasoning {
		return errors.New("selection handler current target is unexpected")
	}
	return nil
}

// selectionActionResponse sets the action variant matching the invoked handler kind.
func selectionActionResponse(
	modelHandler bool,
	action *extensionv1.SelectionHandlerAction,
) *extensionv1.HandleResponse {
	response := new(extensionv1.HandleResponse)
	if modelHandler {
		response.SetModelSelection(action)
	} else {
		response.SetReasoningSelection(action)
	}
	return response
}
