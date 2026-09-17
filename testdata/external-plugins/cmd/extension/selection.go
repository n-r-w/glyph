package main

import (
	"context"
	"errors"
	"fmt"

	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	// selectionCompositionEnvironment enables ordered selection transformations in the external fixture.
	selectionCompositionEnvironment = "GLYPH_EXTERNAL_SELECTION_COMPOSITION"
	// selectionNestedEnvironment enables nested public operations from a selection handler.
	selectionNestedEnvironment = "GLYPH_EXTERNAL_SELECTION_NESTED"
	// selectionObserversEnvironment enables committed selection lifecycle observers.
	selectionObserversEnvironment = "GLYPH_EXTERNAL_SELECTION_OBSERVERS"
	// selectionObserverNestedEnvironment enables nested public operations from a selection observer.
	selectionObserverNestedEnvironment = "GLYPH_EXTERNAL_SELECTION_OBSERVER_NESTED"
	// selectionObserverErrorEnvironment makes the first reasoning observer fail ordinarily.
	selectionObserverErrorEnvironment = "GLYPH_EXTERNAL_SELECTION_OBSERVER_ERROR"
	// nestedSelectionCompleteSignal marks successful nested operation checks.
	nestedSelectionCompleteSignal = "nested-selection-complete"
	// nestedSelectionBusyCode is the expected recursive selection rejection.
	nestedSelectionBusyCode = "BUSY"
	// nestedSelectionObserverCompleteSignal marks successful nested observer operation checks.
	nestedSelectionObserverCompleteSignal = "nested-selection-observer-complete"
	// reasoningSelectionObservedSignal marks one completed reasoning selection observation.
	reasoningSelectionObservedSignal = "reasoning-selection-observed"
	// laterReasoningSelectionObservedSignal marks continuation after the first reasoning observer.
	laterReasoningSelectionObservedSignal = "reasoning-selection-later-observed"
	// nestedSelectionEntryType identifies the nested handler append.
	nestedSelectionEntryType = "nested-selection"
	// nestedSelectionEntryText is the exact nested handler message.
	nestedSelectionEntryText = "nested selection handler append"
	// modelSelectionFirstHandlerID identifies the first composing model handler.
	modelSelectionFirstHandlerID = "compose-model-first"
	// modelSelectionSecondHandlerID identifies the second composing model handler.
	modelSelectionSecondHandlerID = "compose-model-second"
	// reasoningSelectionFirstHandlerID identifies the first composing reasoning handler.
	reasoningSelectionFirstHandlerID = "compose-reasoning-first"
	// reasoningSelectionSecondHandlerID identifies the second composing reasoning handler.
	reasoningSelectionSecondHandlerID = "compose-reasoning-second"
	// modelSelectionObserverID identifies the committed model-selection observer.
	modelSelectionObserverID = "observe-model-selection"
	// reasoningSelectionObserverID identifies the first committed reasoning-selection observer.
	reasoningSelectionObserverID = "observe-reasoning-selection"
	// reasoningSelectionLaterObserverID identifies the later reasoning-selection observer.
	reasoningSelectionLaterObserverID = "observe-reasoning-selection-later"
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

// selectionObserverDescriptors returns the committed selection lifecycle observer declarations.
func selectionObserverDescriptors() []*extensionv1.HandlerDescriptor {
	return []*extensionv1.HandlerDescriptor{
		extensionv1.HandlerDescriptor_builder{
			Id: new(modelSelectionObserverID), Kind: new(extensionv1.HandlerKind_HANDLER_KIND_MODEL_SELECTION_OBSERVER),
		}.Build(),
		extensionv1.HandlerDescriptor_builder{
			Id:   new(reasoningSelectionObserverID),
			Kind: new(extensionv1.HandlerKind_HANDLER_KIND_REASONING_SELECTION_OBSERVER),
		}.Build(),
		extensionv1.HandlerDescriptor_builder{
			Id:   new(reasoningSelectionLaterObserverID),
			Kind: new(extensionv1.HandlerKind_HANDLER_KIND_REASONING_SELECTION_OBSERVER),
		}.Build(),
	}
}

// isSelectionObserverID reports whether an identifier belongs to the fixture's selection observers.
func isSelectionObserverID(handlerID string) bool {
	return handlerID == modelSelectionObserverID || handlerID == reasoningSelectionObserverID ||
		handlerID == reasoningSelectionLaterObserverID
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
func (operation *handleOperation) runSelectionHandler(ctx context.Context) (*extensionv1.HandleResponse, error) {
	invocation := operation.request.GetModelSelection()
	modelHandler := invocation != nil
	if invocation == nil {
		invocation = operation.request.GetReasoningSelection()
	}
	handlerID := operation.request.GetHandlerId()
	if handlerID == modelSelectionHandlerID || handlerID == reasoningSelectionHandlerID {
		if operation.selectionNested && handlerID == modelSelectionHandlerID {
			if err := operation.runNestedSelectionOperations(ctx); err != nil {
				return nil, err
			}
			signal(operation.signals, nestedSelectionCompleteSignal)
		}
		return selectionActionResponse(modelHandler, extensionv1.SelectionHandlerAction_builder{
			Preserve: new(extensionv1.PreserveSelection), Replace: nil, Reject: nil,
		}.Build()), nil
	}
	currentReasoning := selectionOriginalReasoning
	replacementReasoning := selectionIntermediateReasoning
	if handlerID == modelSelectionSecondHandlerID || handlerID == reasoningSelectionSecondHandlerID {
		currentReasoning = selectionIntermediateReasoning
		replacementReasoning = selectionFinalReasoning
	}
	if response, invalid := selectionValidationResponse(invocation, currentReasoning); invalid {
		return response, nil
	}
	action := extensionv1.SelectionHandlerAction_builder{
		Preserve: nil,
		Replace: extensionv1.ModelSelection_builder{
			ProviderId: new(selectionProviderID), ModelId: new(selectionModelID),
			ReasoningChoice: new(replacementReasoning),
		}.Build(),
		Reject: nil,
	}.Build()
	return selectionActionResponse(modelHandler, action), nil
}

// runSelectionObserver validates detached commit values and optionally exercises nested Host operations.
func (operation *handleOperation) runSelectionObserver(ctx context.Context) (*extensionv1.HandleResponse, error) {
	signalName, err := selectionObserverSignal(operation.request)
	if err != nil {
		return nil, err
	}
	if operation.selectionObserverError && operation.request.GetHandlerId() == reasoningSelectionObserverID {
		response := new(extensionv1.HandleResponse)
		response.SetError(extensionv1.HandlerError_builder{Message: new("public reasoning observer failed")}.Build())
		return response, nil
	}
	if operation.selectionObserverNested && operation.request.GetHandlerId() == reasoningSelectionObserverID {
		if nestedErr := operation.runNestedSelectionOperations(ctx); nestedErr != nil {
			return nil, nestedErr
		}
		signal(operation.signals, nestedSelectionObserverCompleteSignal)
	}
	signal(operation.signals, signalName)
	response := new(extensionv1.HandleResponse)
	response.SetLifecycle(new(extensionv1.LifecycleAction))
	return response, nil
}

// selectionObserverSignal validates one typed selection lifecycle payload and returns its success signal.
func selectionObserverSignal(request *extensionv1.HandleRequest) (string, error) {
	invocation := request.GetLifecycle()
	if invocation == nil {
		return "", errors.New("selection lifecycle invocation is missing")
	}
	var preceding *extensionv1.ModelSelection
	var committed *extensionv1.ModelSelection
	signalName := "model-selection-observed"
	if request.GetHandlerId() == reasoningSelectionObserverID ||
		request.GetHandlerId() == reasoningSelectionLaterObserverID {
		change := invocation.GetReasoningSelection()
		if change == nil {
			return "", errors.New("reasoning selection lifecycle payload is missing")
		}
		preceding = change.GetPreceding()
		committed = change.GetCommitted()
		signalName = reasoningSelectionObservedSignal
	} else {
		change := invocation.GetModelSelection()
		if change == nil {
			return "", errors.New("model selection lifecycle payload is missing")
		}
		preceding = change.GetPreceding()
		committed = change.GetCommitted()
	}
	if preceding == nil || committed == nil || preceding.GetProviderId() == "" || committed.GetProviderId() == "" {
		return "", errors.New("selection lifecycle values are incomplete")
	}
	if request.GetHandlerId() == reasoningSelectionLaterObserverID {
		signalName = laterReasoningSelectionObservedSignal
	}
	return signalName, nil
}

// selectionValidationResponse maps an invalid invocation to an ordinary handler-error result.
func selectionValidationResponse(
	invocation *extensionv1.SelectionHandlerInvocation,
	currentReasoning string,
) (*extensionv1.HandleResponse, bool) {
	validationErr := validateSelectionInvocation(invocation, currentReasoning)
	if validationErr == nil {
		return nil, false
	}
	response := new(extensionv1.HandleResponse)
	response.SetError(extensionv1.HandlerError_builder{Message: new(validationErr.Error())}.Build())
	return response, true
}

// runNestedSelectionOperations verifies recursive BUSY and unrelated context-operation availability.
func (operation *handleOperation) runNestedSelectionOperations(ctx context.Context) error {
	nestedSelection, err := operation.context.StartReasoningSelection(ctx, extensionv1.SelectReasoningRequest_builder{
		Context: nil, ReasoningChoice: new(selectionFinalReasoning),
	}.Build())
	if err != nil {
		return err
	}
	_, err = nestedSelection.Wait(ctx)
	rejection, rejected := errors.AsType[*extensionsdk.RejectionError](err)
	if !rejected || rejection.Code() != nestedSelectionBusyCode {
		return fmt.Errorf("nested selection must reject BUSY: %w", err)
	}
	models, err := operation.context.StartGetModels(ctx)
	if err != nil {
		return err
	}
	if _, err = models.Wait(ctx); err != nil {
		return err
	}
	if _, err = requestConfiguredModel(ctx); err != nil {
		return err
	}
	appendOperation, err := operation.context.StartAppendExtensionMessage(
		ctx,
		extensionv1.AppendExtensionMessageRequest_builder{
			Context: nil, EntryType: new(nestedSelectionEntryType), Text: new(nestedSelectionEntryText),
			Visibility: new(extensionv1.ClientVisibility_CLIENT_VISIBILITY_VISIBLE),
		}.Build(),
	)
	if err != nil {
		return err
	}
	appended, err := appendOperation.Wait(ctx)
	if err != nil {
		return err
	}
	if len(appended.GetIssues()) != 0 {
		return errors.New("nested selection handler append returned a delivery issue")
	}
	return nil
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
