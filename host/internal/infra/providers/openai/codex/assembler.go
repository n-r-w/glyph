package codex

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/openai/openai-go/v3/responses"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

type outputKey struct {
	outputIndex  int64
	contentIndex int64
}

type outputSlot struct {
	kind     model.ContentKind
	position int
	active   bool
}

type functionOutputSlot struct {
	itemID        string
	callID        string
	name          string
	position      int
	preview       *functionPreviewAssembler
	custom        bool
	inputProperty string
	customInput   string
}

// pendingFunctionOutput retains only provider identity needed to validate later authoritative output.
type pendingFunctionOutput struct {
	itemID string
	custom bool
}

// finalizedFunctionOutput prevents duplicate lifecycle events and validates repeated authoritative output.
type finalizedFunctionOutput struct {
	itemID      string
	callID      string
	name        string
	arguments   map[string]any
	custom      bool
	customInput string
}

// semanticAssembler converts Codex output indexes into provider-neutral content positions.
type semanticAssembler struct {
	handle                    run.StreamHandler
	completedOutputByPosition map[int64]responses.ResponseOutputItemUnion
	slots                     map[outputKey]outputSlot
	functionCalls             map[int64]*functionOutputSlot
	pendingFunctionCalls      map[int64]pendingFunctionOutput
	finalizedFunctionCalls    map[int64]finalizedFunctionOutput
	grammarInputProperties    map[string]string
	next                      int
}

func newSemanticAssembler(
	handle run.StreamHandler,
	grammarProperties map[string]string,
) *semanticAssembler {
	return &semanticAssembler{
		handle:                    handle,
		completedOutputByPosition: make(map[int64]responses.ResponseOutputItemUnion),
		slots:                     make(map[outputKey]outputSlot),
		functionCalls:             make(map[int64]*functionOutputSlot),
		pendingFunctionCalls:      make(map[int64]pendingFunctionOutput),
		finalizedFunctionCalls:    make(map[int64]finalizedFunctionOutput),
		grammarInputProperties:    grammarProperties,
		next:                      0,
	}
}

// consume applies one decoded SDK event and returns a terminal response when present.
//
//nolint:gocyclo,gocognit // The flat switch converts the closed Codex stream event union.
func (a *semanticAssembler) consume(event responses.ResponseStreamEventUnion) (model.Response, bool, error) {
	switch event.Type {
	case "response.output_item.added":
		added := event.AsResponseOutputItemAdded()
		switch added.Item.Type {
		case "reasoning":
			if _, err := a.start(added.OutputIndex, -1, model.ContentReasoning); err != nil {
				return model.Response{}, true, err
			}
		case "function_call":
			call := added.Item.AsFunctionCall()
			if err := a.startFunction(added.OutputIndex, call.ID, call.CallID, call.Name, call.Arguments); err != nil {
				return failedModelResponse(requestFailedMessage), true, err
			}
		case "custom_tool_call":
			call := added.Item.AsCustomToolCall()
			if err := a.startCustom(added.OutputIndex, call.ID, call.CallID, call.Name, call.Input); err != nil {
				return failedModelResponse(requestFailedMessage), true, err
			}
		}
	case "response.function_call_arguments.delta":
		delta := event.AsResponseFunctionCallArgumentsDelta()
		if err := a.deltaFunction(delta.OutputIndex, delta.ItemID, delta.Delta); err != nil {
			return failedModelResponse(requestFailedMessage), true, err
		}
	case "response.function_call_arguments.done":
		done := event.AsResponseFunctionCallArgumentsDone()
		if err := a.endFunction(done.OutputIndex, done.ItemID, done.Name, done.Arguments); err != nil {
			return failedModelResponse(requestFailedMessage), true, err
		}
	case "response.custom_tool_call_input.delta":
		delta := event.AsResponseCustomToolCallInputDelta()
		if err := a.deltaCustom(delta.OutputIndex, delta.ItemID, delta.Delta); err != nil {
			return failedModelResponse(requestFailedMessage), true, err
		}
	case "response.custom_tool_call_input.done":
		done := event.AsResponseCustomToolCallInputDone()
		if err := a.endCustom(done.OutputIndex, done.ItemID, done.Input); err != nil {
			return failedModelResponse(requestFailedMessage), true, err
		}
	case "response.reasoning_summary_text.delta":
		delta := event.AsResponseReasoningSummaryTextDelta()
		if err := a.delta(delta.OutputIndex, -1, model.ContentReasoning, delta.Delta); err != nil {
			return model.Response{}, true, err
		}
	case "response.output_text.delta":
		delta := event.AsResponseOutputTextDelta()
		if err := a.delta(delta.OutputIndex, delta.ContentIndex, model.ContentText, delta.Delta); err != nil {
			return model.Response{}, true, err
		}
	case "response.refusal.delta":
		delta := event.AsResponseRefusalDelta()
		if err := a.delta(delta.OutputIndex, delta.ContentIndex, model.ContentRefusal, delta.Delta); err != nil {
			return model.Response{}, true, err
		}
	case "response.output_item.done":
		done := event.AsResponseOutputItemDone()
		if err := a.reconcileFunctionOutput(done.OutputIndex, done.Item); err != nil {
			return failedModelResponse(requestFailedMessage), true, err
		}
		if err := a.endOutput(done.OutputIndex); err != nil {
			return model.Response{}, true, err
		}
		if done.OutputIndex < 0 || done.OutputIndex > int64(^uint(0)>>1) {
			return failedModelResponse(requestFailedMessage), true, safeError(requestFailedMessage)
		}
		a.completedOutputByPosition[done.OutputIndex] = done.Item
	case "response.completed":
		return a.complete(event.AsResponseCompleted().Response, model.OutcomeStop)
	case "response.incomplete":
		incomplete := event.AsResponseIncomplete().Response
		if incomplete.IncompleteDetails.Reason == "max_output_tokens" {
			return a.complete(incomplete, model.OutcomeLength)
		}
		if err := a.finish(); err != nil {
			return model.Response{}, true, err
		}
		message := providerFailureMessage(incomplete.Error.Message)
		terminalResponse, recovered, mergeErr := a.mergeTerminalOutput(incomplete)
		if mergeErr != nil {
			return failedModelResponse(requestFailedMessage), true, mergeErr
		}
		response, err := failedResponseFromSDK(terminalResponse, message, a.grammarInputProperties)
		if recovered {
			response = addRecoveryDiagnostic(response)
		}
		return response, true, err
	case "response.failed":
		if err := a.finish(); err != nil {
			return model.Response{}, true, err
		}
		failed := event.AsResponseFailed().Response
		message := providerFailureMessage(failed.Error.Message)
		terminalResponse, recovered, mergeErr := a.mergeTerminalOutput(failed)
		if mergeErr != nil {
			return failedModelResponse(requestFailedMessage), true, mergeErr
		}
		response, err := failedResponseFromSDK(terminalResponse, message, a.grammarInputProperties)
		if recovered {
			response = addRecoveryDiagnostic(response)
		}
		return response, true, err
	case "error":
		if err := a.finish(); err != nil {
			return model.Response{}, true, err
		}
		providerEvent := event.AsError()
		message := providerFailureMessage(providerEvent.Message)
		return failedModelResponse(message), true, safeError(message)
	}
	return model.Response{}, false, nil
}

func (a *semanticAssembler) complete(
	response responses.Response,
	outcome model.Outcome,
) (model.Response, bool, error) {
	response, recovered, mergeErr := a.mergeTerminalOutput(response)
	if mergeErr != nil {
		return failedModelResponse(requestFailedMessage), true, mergeErr
	}
	if err := a.reconcileTerminalFunctionOutputs(response.Output); err != nil {
		return failedModelResponse(requestFailedMessage), true, err
	}
	if err := a.finish(); err != nil {
		return model.Response{}, true, err
	}
	converted, err := modelResponse(response, outcome, a.grammarInputProperties)
	if recovered {
		converted = addRecoveryDiagnostic(converted)
	}
	return converted, true, err
}

// mergeTerminalOutput fills terminal omissions from completed stream items in provider order.
func (a *semanticAssembler) mergeTerminalOutput(
	response responses.Response,
) (responses.Response, bool, error) {
	recovered := len(response.Output) == 0 && len(a.completedOutputByPosition) > 0
	highestPosition := len(response.Output) - 1
	for position := range a.completedOutputByPosition {
		if int(position) > highestPosition {
			highestPosition = int(position)
		}
	}
	if highestPosition < 0 {
		return response, false, nil
	}
	output := make([]responses.ResponseOutputItemUnion, 0, highestPosition+1)
	for position := range highestPosition + 1 {
		if position < len(response.Output) {
			output = append(output, response.Output[position])
			continue
		}
		item, ok := a.completedOutputByPosition[int64(position)]
		if !ok {
			return responses.Response{}, false, fmt.Errorf(
				"OpenAI Codex returned noncontiguous completed output at position %d", position,
			)
		}
		output = append(output, item)
	}
	response.Output = output
	return response, recovered, nil
}

// reconcileTerminalFunctionOutputs finalizes calls omitted from earlier stream lifecycle events.
func (a *semanticAssembler) reconcileTerminalFunctionOutputs(
	output []responses.ResponseOutputItemUnion,
) error {
	for outputIndex := range output {
		if err := a.reconcileFunctionOutput(int64(outputIndex), output[outputIndex]); err != nil {
			return err
		}
	}
	return nil
}

// reconcileFunctionOutput uses a complete provider item to create or close one tool-call lifecycle.
func (a *semanticAssembler) reconcileFunctionOutput(
	outputIndex int64,
	item responses.ResponseOutputItemUnion,
) error {
	switch item.Type {
	case "function_call":
		call := item.AsFunctionCall()
		if finalized, ok := a.finalizedFunctionCalls[outputIndex]; ok {
			arguments, err := decodeFunctionArguments(call.Arguments)
			if err != nil {
				return err
			}
			return validateFinalizedFunctionOutput(finalized, call.ID, call.CallID, call.Name, arguments, false, "")
		}
		if _, active := a.functionCalls[outputIndex]; !active {
			if err := a.startFunction(outputIndex, call.ID, call.CallID, call.Name, ""); err != nil {
				return err
			}
		}
		return a.endFunction(outputIndex, call.ID, call.Name, call.Arguments)
	case "custom_tool_call":
		call := item.AsCustomToolCall()
		if finalized, ok := a.finalizedFunctionCalls[outputIndex]; ok {
			return validateFinalizedFunctionOutput(finalized, call.ID, call.CallID, call.Name, nil, true, call.Input)
		}
		if _, active := a.functionCalls[outputIndex]; !active {
			if err := a.startCustom(outputIndex, call.ID, call.CallID, call.Name, ""); err != nil {
				return err
			}
		}
		return a.endCustom(outputIndex, call.ID, call.Input)
	default:
		return nil
	}
}

// validateFinalizedFunctionOutput rejects contradictory repeated provider completion data.
func validateFinalizedFunctionOutput(
	finalized finalizedFunctionOutput,
	itemID string,
	callID string,
	name string,
	arguments map[string]any,
	custom bool,
	customInput string,
) error {
	if finalized.itemID != itemID || finalized.callID != callID || finalized.name != name || finalized.custom != custom {
		return safeError(requestFailedMessage)
	}
	if custom {
		if finalized.customInput != customInput {
			return safeError(requestFailedMessage)
		}
		return nil
	}
	if !reflect.DeepEqual(finalized.arguments, arguments) {
		return safeError(requestFailedMessage)
	}
	return nil
}

func addRecoveryDiagnostic(response model.Response) model.Response {
	response.Diagnostics = append(response.Diagnostics, model.Diagnostic{
		Code: "recovered_finalized_output", Message: "Recovered finalized provider output omitted from the terminal event.",
	})
	return response
}

func (a *semanticAssembler) delta(
	outputIndex int64,
	contentIndex int64,
	kind model.ContentKind,
	delta string,
) error {
	slot, err := a.start(outputIndex, contentIndex, kind)
	if err != nil {
		return err
	}
	return a.handle(semanticStreamEvent(run.StreamEventTextDelta, slot.position, kind, delta))
}

func (a *semanticAssembler) start(
	outputIndex int64,
	contentIndex int64,
	kind model.ContentKind,
) (outputSlot, error) {
	if outputIndex < 0 || outputIndex > int64(^uint(0)>>1) || contentIndex < -1 {
		return outputSlot{}, safeError(requestFailedMessage)
	}
	key := outputKey{outputIndex: outputIndex, contentIndex: contentIndex}
	if slot, ok := a.slots[key]; ok {
		if slot.kind != kind {
			return outputSlot{}, fmt.Errorf("codex output %d changed content kind", outputIndex)
		}
		return slot, nil
	}
	width := 1
	if kind == model.ContentReasoning {
		width = 2
	}
	position, allocationErr := a.allocatePosition(outputIndex, width)
	if allocationErr != nil {
		return outputSlot{}, allocationErr
	}
	slot := outputSlot{kind: kind, position: position, active: true}
	a.slots[key] = slot
	if handleErr := a.handle(semanticStreamEvent(run.StreamEventContentStart, position, kind, "")); handleErr != nil {
		return outputSlot{}, handleErr
	}
	return slot, nil
}

func (a *semanticAssembler) end(key outputKey) error {
	slot, ok := a.slots[key]
	if !ok || !slot.active {
		return nil
	}
	if err := a.handle(semanticStreamEvent(run.StreamEventContentEnd, slot.position, slot.kind, "")); err != nil {
		return err
	}
	slot.active = false
	a.slots[key] = slot
	return nil
}

func semanticStreamEvent(
	kind run.StreamEventKind,
	position int,
	contentKind model.ContentKind,
	delta string,
) run.StreamEvent {
	event := run.StreamEvent{
		Kind:     kind,
		Position: mo.None[int](),
		Content:  mo.None[model.Content](),
		Delta:    mo.None[string](),
		Preview:  mo.None[model.ToolCallPreview](),
		ToolCall: mo.None[model.ToolCall](),
		Response: mo.None[model.Response](),
	}
	if kind != run.StreamEventDone && kind != run.StreamEventError {
		event.Position = mo.Some(position)
	}
	if kind == run.StreamEventContentStart || kind == run.StreamEventTextDelta ||
		kind == run.StreamEventContentEnd {
		text := mo.None[string]()
		if contentKind == model.ContentText || contentKind == model.ContentRefusal ||
			contentKind == model.ContentReasoning {
			text = mo.Some(delta)
		}
		event.Content = mo.Some(model.Content{
			Kind:            contentKind,
			Text:            text,
			Final:           false,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
		})
	}
	if kind == run.StreamEventTextDelta {
		event.Delta = mo.Some(delta)
	}
	return event
}

func (a *semanticAssembler) allocatePosition(outputIndex int64, width int) (int, error) {
	if outputIndex < 0 || outputIndex > int64(^uint(0)>>1) {
		return 0, safeError(requestFailedMessage)
	}
	position := max(int(outputIndex), a.next)
	a.next = position + width
	return position, nil
}

func (a *semanticAssembler) startFunction(
	outputIndex int64,
	itemID string,
	callID string,
	name string,
	arguments string,
) error {
	if itemID == "" || callID == "" || name == "" {
		return safeError(requestFailedMessage)
	}
	if _, exists := a.functionCalls[outputIndex]; exists {
		return fmt.Errorf("codex function output %d already started", outputIndex)
	}
	if err := a.validatePendingFunction(outputIndex, itemID, false); err != nil {
		return err
	}
	position, allocationErr := a.allocatePosition(outputIndex, 1)
	if allocationErr != nil {
		return allocationErr
	}
	slot := &functionOutputSlot{
		itemID: itemID, callID: callID, name: name, position: position,
		preview: newFunctionPreviewAssembler(), custom: false, inputProperty: "", customInput: "",
	}
	a.functionCalls[outputIndex] = slot
	delete(a.pendingFunctionCalls, outputIndex)
	preview := model.ToolCallPreview{
		CallID: callID, Name: name, Position: position, Provisional: true, Fields: nil,
	}
	event := semanticStreamEvent(run.StreamEventToolCallStart, position, 0, "")
	event.Preview = mo.Some(preview)
	if handleErr := a.handle(event); handleErr != nil {
		return handleErr
	}
	if arguments != "" {
		return a.deltaFunction(outputIndex, itemID, arguments)
	}
	return nil
}

func (a *semanticAssembler) deltaFunction(outputIndex int64, itemID, delta string) error {
	slot, ok := a.functionCalls[outputIndex]
	if !ok {
		return a.recordPendingFunction(outputIndex, itemID, false)
	}
	if slot.custom || identitiesConflict(slot.itemID, itemID) {
		return fmt.Errorf("codex function output %d is not active", outputIndex)
	}
	fields, err := slot.preview.appendFragment(delta)
	if err != nil {
		return err
	}
	event := semanticStreamEvent(run.StreamEventToolCallDelta, slot.position, 0, "")
	event.Preview = mo.Some(model.ToolCallPreview{
		CallID: slot.callID, Name: slot.name, Position: slot.position,
		Provisional: true, Fields: fields,
	})
	return a.handle(event)
}

// startCustom allocates one custom call and exposes its provider-neutral provisional identity.
func (a *semanticAssembler) startCustom(
	outputIndex int64,
	itemID string,
	callID string,
	name string,
	input string,
) error {
	property, ok := a.grammarInputProperties[name]
	if !ok || property == "" || itemID == "" || callID == "" || name == "" {
		return safeError(requestFailedMessage)
	}
	if _, exists := a.functionCalls[outputIndex]; exists {
		return fmt.Errorf("codex custom output %d already started", outputIndex)
	}
	if err := a.validatePendingFunction(outputIndex, itemID, true); err != nil {
		return err
	}
	position, err := a.allocatePosition(outputIndex, 1)
	if err != nil {
		return err
	}
	a.functionCalls[outputIndex] = &functionOutputSlot{
		itemID: itemID, callID: callID, name: name, position: position, preview: nil,
		custom: true, inputProperty: property, customInput: input,
	}
	delete(a.pendingFunctionCalls, outputIndex)
	event := semanticStreamEvent(run.StreamEventToolCallStart, position, 0, "")
	event.Preview = mo.Some(model.ToolCallPreview{
		CallID: callID, Name: name, Position: position, Provisional: true, Fields: nil,
	})
	if handleErr := a.handle(event); handleErr != nil {
		return handleErr
	}
	if input != "" {
		return a.publishCustomPreview(a.functionCalls[outputIndex])
	}
	return nil
}

// deltaCustom appends exact provider input and publishes it through the shared preview state.
func (a *semanticAssembler) deltaCustom(outputIndex int64, itemID, delta string) error {
	slot, ok := a.functionCalls[outputIndex]
	if !ok {
		return a.recordPendingFunction(outputIndex, itemID, true)
	}
	if !slot.custom || identitiesConflict(slot.itemID, itemID) {
		return fmt.Errorf("codex custom output %d is not active", outputIndex)
	}
	slot.customInput += delta
	return a.publishCustomPreview(slot)
}

// identitiesConflict permits omitted event identity but rejects contradictory nonempty values.
func identitiesConflict(expected, actual string) bool {
	return expected != "" && actual != "" && expected != actual
}

// recordPendingFunction records delta identity without creating a provider-neutral lifecycle.
func (a *semanticAssembler) recordPendingFunction(outputIndex int64, itemID string, custom bool) error {
	if outputIndex < 0 || outputIndex > int64(^uint(0)>>1) {
		return safeError(requestFailedMessage)
	}
	pending, ok := a.pendingFunctionCalls[outputIndex]
	if ok && (pending.custom != custom || identitiesConflict(pending.itemID, itemID)) {
		return safeError(requestFailedMessage)
	}
	if !ok || pending.itemID == "" {
		a.pendingFunctionCalls[outputIndex] = pendingFunctionOutput{itemID: itemID, custom: custom}
	}
	return nil
}

// validatePendingFunction checks authoritative identity against any earlier identity-bearing delta.
func (a *semanticAssembler) validatePendingFunction(outputIndex int64, itemID string, custom bool) error {
	pending, ok := a.pendingFunctionCalls[outputIndex]
	if !ok {
		return nil
	}
	if pending.custom != custom || identitiesConflict(pending.itemID, itemID) {
		return safeError(requestFailedMessage)
	}
	return nil
}

// publishCustomPreview exposes only the exact received custom input prefix.
func (a *semanticAssembler) publishCustomPreview(slot *functionOutputSlot) error {
	event := semanticStreamEvent(run.StreamEventToolCallDelta, slot.position, 0, "")
	event.Preview = mo.Some(model.ToolCallPreview{
		CallID: slot.callID, Name: slot.name, Position: slot.position, Provisional: true,
		Fields: []model.ToolCallPreviewField{{
			Name: slot.inputProperty, Kind: model.ToolCallPreviewFieldPrefix,
			Value: mo.None[any](), Prefix: mo.Some(slot.customInput),
		}},
	})
	return a.handle(event)
}

// endCustom maps terminal custom input under the Host-validated string property.
func (a *semanticAssembler) endCustom(outputIndex int64, itemID, input string) error {
	slot, ok := a.functionCalls[outputIndex]
	if !ok {
		return a.recordPendingFunction(outputIndex, itemID, true)
	}
	if !slot.custom || identitiesConflict(slot.itemID, itemID) {
		return fmt.Errorf("codex custom output %d is not active", outputIndex)
	}
	event := semanticStreamEvent(run.StreamEventToolCallEnd, slot.position, 0, "")
	event.ToolCall = mo.Some(model.ToolCall{
		ID: slot.callID, Name: slot.name, Arguments: map[string]any{slot.inputProperty: input},
	})
	if err := a.handle(event); err != nil {
		return err
	}
	delete(a.functionCalls, outputIndex)
	a.finalizedFunctionCalls[outputIndex] = finalizedFunctionOutput{
		itemID: slot.itemID, callID: slot.callID, name: slot.name, arguments: nil,
		custom: true, customInput: input,
	}
	return nil
}

func (a *semanticAssembler) endFunction(
	outputIndex int64,
	itemID string,
	name string,
	arguments string,
) error {
	slot, ok := a.functionCalls[outputIndex]
	if !ok {
		return a.recordPendingFunction(outputIndex, itemID, false)
	}
	if slot.custom || identitiesConflict(slot.itemID, itemID) || identitiesConflict(slot.name, name) {
		return fmt.Errorf("codex function output %d is not active", outputIndex)
	}
	decoded, err := decodeFunctionArguments(arguments)
	if err != nil {
		slot.preview.close()
		delete(a.functionCalls, outputIndex)
		return err
	}
	finalizedArguments, err := decodeFunctionArguments(arguments)
	if err != nil {
		return err
	}
	slot.preview.close()
	event := semanticStreamEvent(run.StreamEventToolCallEnd, slot.position, 0, "")
	event.ToolCall = mo.Some(model.ToolCall{ID: slot.callID, Name: slot.name, Arguments: decoded})
	if handleErr := a.handle(event); handleErr != nil {
		return handleErr
	}
	delete(a.functionCalls, outputIndex)
	a.finalizedFunctionCalls[outputIndex] = finalizedFunctionOutput{
		itemID: slot.itemID, callID: slot.callID, name: slot.name, arguments: finalizedArguments,
		custom: false, customInput: "",
	}
	return nil
}

func decodeFunctionArguments(arguments string) (map[string]any, error) {
	var decoded map[string]any
	if err := json.Unmarshal([]byte(arguments), &decoded); err != nil {
		return nil, errors.New("OpenAI Codex returned invalid tool-call arguments")
	}
	return decoded, nil
}

func (a *semanticAssembler) endOutput(outputIndex int64) error {
	keys := make([]outputKey, 0)
	for key, slot := range a.slots {
		if key.outputIndex == outputIndex && slot.active {
			keys = append(keys, key)
		}
	}
	slices.SortFunc(keys, func(left, right outputKey) int {
		return cmp.Compare(a.slots[left].position, a.slots[right].position)
	})
	for _, key := range keys {
		if err := a.end(key); err != nil {
			return err
		}
	}
	return nil
}

func (a *semanticAssembler) finish() error {
	keys := make([]outputKey, 0, len(a.slots))
	for key, slot := range a.slots {
		if slot.active {
			keys = append(keys, key)
		}
	}
	slices.SortFunc(keys, func(left, right outputKey) int {
		return cmp.Compare(a.slots[left].position, a.slots[right].position)
	})
	for _, key := range keys {
		if err := a.end(key); err != nil {
			return err
		}
	}
	for outputIndex, slot := range a.functionCalls {
		if slot.preview != nil {
			slot.preview.close()
		}
		delete(a.functionCalls, outputIndex)
	}
	return nil
}
