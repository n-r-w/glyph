package extension

import (
	"errors"
	"fmt"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// CompactionRequestTrigger identifies the request entry point.
type CompactionRequestTrigger uint8

const (
	// CompactionRequestManual identifies explicit compaction.
	CompactionRequestManual CompactionRequestTrigger = iota + 1
	// CompactionRequestThreshold identifies automatic preparation.
	CompactionRequestThreshold
	// CompactionRequestOverflow identifies overflow recovery.
	CompactionRequestOverflow
)

// CompactionRequestChange identifies request preservation or replacement.
type CompactionRequestChange uint8

const (
	// CompactionRequestPreserve denotes preservation of the current request.
	CompactionRequestPreserve CompactionRequestChange = iota + 1
	// CompactionRequestReplace denotes replacement of the current request.
	CompactionRequestReplace
)

// CompactionResultChange identifies result preservation, replacement, or clearing.
type CompactionResultChange uint8

const (
	// CompactionResultPreserve denotes preservation of the optional current result.
	CompactionResultPreserve CompactionResultChange = iota + 1
	// CompactionResultReplace denotes replacement of the current result.
	CompactionResultReplace
	// CompactionResultClear denotes clearing of the optional result.
	CompactionResultClear
)

// CompactionParticipant identifies one captured compaction capability.
type CompactionParticipant struct {
	// ExtensionID identifies the producer.
	ExtensionID string
	// RuntimeID identifies the captured generation.
	RuntimeID string
	// HandlerID identifies the registered capability.
	HandlerID string
	// RegistrationOrder identifies the position in the captured capability sequence.
	RegistrationOrder int64
}

// CompactionRequestEntry contains one prepared record with supplied accounting.
type CompactionRequestEntry struct {
	// Entry contains prepared model content and its original source.
	Entry session.PreparedHistoryEntry
	// PrivateBytes contains the retained private replay size.
	PrivateBytes int64
	// EstimatedTokens contains the extension's estimate.
	EstimatedTokens int64
}

// decodePolicyRequestEntries supplies detached public entry values.
func decodePolicyRequestEntries(wires []*extensionpb.CompactionHistoryRecord) ([]CompactionRequestEntry, error) {
	entries := make([]CompactionRequestEntry, len(wires))
	for index, wire := range wires {
		if wire == nil {
			return nil, fmt.Errorf("request entry %d is missing", index)
		}
		entry, err := DecodeCompactionRequestEntry(wire)
		if err != nil {
			return nil, fmt.Errorf("request entry %d: %w", index, err)
		}
		entries[index] = CompactionRequestEntry{
			Entry:           entry,
			PrivateBytes:    wire.GetPrivatePayloadBytes(),
			EstimatedTokens: wire.GetEstimatedTokens(),
		}
	}
	return entries, nil
}

// MapCompactionRequestEntries returns public prepared records with source correspondence and supplied accounting.
func MapCompactionRequestEntries(entries []CompactionRequestEntry) ([]*extensionpb.CompactionHistoryRecord, error) {
	history := make(session.PreparedHistory, len(entries))
	for index := range entries {
		history[index] = entries[index].Entry
	}
	wires, err := MapPreparedCompactionHistory(history)
	if err != nil {
		return nil, err
	}
	for index := range wires {
		wires[index].SetPrivatePayloadBytes(entries[index].PrivateBytes)
		wires[index].SetEstimatedTokens(entries[index].EstimatedTokens)
	}
	return wires, nil
}

// CompactionRequestValue contains extension-owned request state for Host admission.
type CompactionRequestValue struct {
	// Trigger identifies the compaction entry point.
	Trigger CompactionRequestTrigger
	// RetryIntent identifies an overflow replacement attempt.
	RetryIntent bool
	// Instructions contains optional manual guidance.
	Instructions mo.Option[string]
	// Model contains the selected model descriptor.
	Model model.Descriptor
	// ReasoningChoice contains the selected reasoning choice.
	ReasoningChoice model.ReasoningChoice
	// Prefix contains the entries proposed for summarization.
	Prefix []CompactionRequestEntry
	// Suffix contains the proposed retained entries.
	Suffix []CompactionRequestEntry
	// Previous contains the preceding compaction entry.
	Previous mo.Option[session.CompactionEntry]
	// ContextTokens contains the supplied size estimate.
	ContextTokens int64
	// ContextTokensEstimated identifies an estimated count.
	ContextTokensEstimated bool
	// ContextWindow contains the extension-supplied context capacity for summary composition.
	ContextWindow int64
	// ResponseBudget contains the output reserve.
	ResponseBudget int64
	// RetainedBudget contains the extension-selected token budget for the retained suffix.
	RetainedBudget int64
}

// decodePolicyRequest maps one extension-owned request value.
func decodePolicyRequest(wire *extensionpb.CompactionRequest) (CompactionRequestValue, error) {
	if wire == nil {
		return CompactionRequestValue{}, errors.New("compaction request is required")
	}
	var trigger CompactionRequestTrigger
	switch wire.GetTrigger() {
	case extensionpb.CompactionTrigger_COMPACTION_TRIGGER_MANUAL:
		trigger = CompactionRequestManual
	case extensionpb.CompactionTrigger_COMPACTION_TRIGGER_THRESHOLD:
		trigger = CompactionRequestThreshold
	case extensionpb.CompactionTrigger_COMPACTION_TRIGGER_OVERFLOW:
		trigger = CompactionRequestOverflow
	case extensionpb.CompactionTrigger_COMPACTION_TRIGGER_UNSPECIFIED:
		return CompactionRequestValue{}, errors.New("compaction request trigger is unspecified")
	default:
		return CompactionRequestValue{}, fmt.Errorf("compaction request trigger %d is unknown", wire.GetTrigger())
	}
	prefix, err := decodePolicyRequestEntries(wire.GetPrefix())
	if err != nil {
		return CompactionRequestValue{}, err
	}
	suffix, err := decodePolicyRequestEntries(wire.GetSuffix())
	if err != nil {
		return CompactionRequestValue{}, err
	}
	previous := DecodeCompactionPrevious(wire.GetPrevious())
	return CompactionRequestValue{
		Trigger:      trigger,
		RetryIntent:  wire.GetRetryIntent(),
		Instructions: mo.TupleToOption(wire.GetInstructions(), wire.HasInstructions()),
		Model: DecodeCompactionRequestModel(
			wire.GetModel(),
		),
		ReasoningChoice:        model.ReasoningChoice(wire.GetReasoningChoice()),
		Prefix:                 prefix,
		Suffix:                 suffix,
		Previous:               previous,
		ContextTokens:          wire.GetContextTokens(),
		ContextTokensEstimated: wire.GetContextTokensEstimated(),
		ContextWindow:          wire.GetContextWindow(),
		ResponseBudget:         wire.GetResponseBudget(),
		RetainedBudget:         wire.GetRetainedBudget(),
	}, nil
}

// encodePolicyRequest projects one extension-owned current request.
func (value CompactionRequestValue) encodePolicyRequest() (*extensionpb.CompactionRequest, error) {
	prefix, err := MapCompactionRequestEntries(value.Prefix)
	if err != nil {
		return nil, err
	}
	suffix, err := MapCompactionRequestEntries(value.Suffix)
	if err != nil {
		return nil, err
	}
	wire := extensionpb.CompactionRequest_builder{
		Trigger: new(
			extensionpb.CompactionTrigger(value.Trigger),
		),
		RetryIntent:            new(value.RetryIntent),
		Instructions:           nil,
		Model:                  mapDescriptor(value.Model),
		ReasoningChoice:        new(string(value.ReasoningChoice)),
		Prefix:                 prefix,
		Suffix:                 suffix,
		Previous:               nil,
		ContextTokens:          new(value.ContextTokens),
		ContextTokensEstimated: new(value.ContextTokensEstimated),
		ContextWindow:          new(value.ContextWindow),
		ResponseBudget:         new(value.ResponseBudget),
		RetainedBudget:         new(value.RetainedBudget),
	}.Build()
	if instructions, present := value.Instructions.Get(); present {
		wire.SetInstructions(instructions)
	}
	if previous, present := value.Previous.Get(); present {
		wire.SetPrevious(MapCompactionMarker(previous))
	}
	return wire, nil
}

// CompactionRequestCommand contains the selected participant and extension-owned input values.
type CompactionRequestCommand struct {
	// OperationID identifies policy invocation authority.
	OperationID string
	// Participant identifies the captured target.
	Participant CompactionParticipant
	// Original contains the immutable initial request.
	Original CompactionRequestValue
	// Current contains the current request.
	Current CompactionRequestValue
	// CurrentResult contains the optional supplied result.
	CurrentResult mo.Option[session.CompactionEntry]
}

// CompactionRequestResponse contains independent action values from one handler.
type CompactionRequestResponse struct {
	// Cancel identifies exclusive terminal cancellation.
	Cancel bool
	// RequestAction identifies request preservation or replacement.
	RequestAction CompactionRequestChange
	// Request contains a replacement request.
	Request mo.Option[CompactionRequestValue]
	// ResultAction identifies result preservation, replacement, or clearing.
	ResultAction CompactionResultChange
	// Result contains a replacement result.
	Result mo.Option[session.CompactionEntry]
}

// encodePolicyRequestAction projects one independent handler action.
func (action CompactionRequestResponse) encodePolicyRequestAction() (*extensionpb.CompactionRequestAction, error) {
	wire := new(extensionpb.CompactionRequestAction)
	wire.SetCancel(action.Cancel)
	switch action.RequestAction {
	case CompactionRequestPreserve:
		wire.SetRequestAction(extensionpb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_PRESERVE)
	case CompactionRequestReplace:
		wire.SetRequestAction(extensionpb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_REPLACE)
	default:
		wire.SetRequestAction(extensionpb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_UNSPECIFIED)
	}
	switch action.ResultAction {
	case CompactionResultPreserve:
		wire.SetResultAction(extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_PRESERVE)
	case CompactionResultReplace:
		wire.SetResultAction(extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE)
	case CompactionResultClear:
		wire.SetResultAction(extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_CLEAR)
	default:
		wire.SetResultAction(extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_UNSPECIFIED)
	}
	if request, present := action.Request.Get(); present {
		replacement, err := request.encodePolicyRequest()
		if err != nil {
			return nil, err
		}
		wire.SetRequest(replacement)
	}
	if result, present := action.Result.Get(); present {
		wire.SetResult(encodePolicyRequestResult(result))
	}
	return wire, nil
}
