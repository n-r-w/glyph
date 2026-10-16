package extension

import (
	"strings"

	"github.com/samber/mo"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// decodeInput maps captured policy input into policy data.
func decodeInput(value *pb.CompactionPolicyInvocation) compaction.Input {
	entries := make([]compaction.Entry, 0, len(value.GetEntries()))
	for _, snapshot := range value.GetEntries() {
		entry := decodeEntry(snapshot.GetEntry())
		entry.PrivateBytes = snapshot.GetPrivatePayloadBytes()
		entries = append(entries, entry)
	}
	return compaction.Input{
		Binding:      compaction.Binding{},
		Entries:      entries,
		History:      DecodeHistory(value.GetHistory()),
		Previous:     decodeMarker(value.GetPrevious()),
		Model:        decodeModel(value.GetModel()),
		Reasoning:    value.GetReasoningChoice(),
		Instructions: value.GetInstructions(),
		ToolText:     decodeToolText(value.GetTools()),
		ToolShape:    decodeToolShape(value.GetTools()),
		Guidance:     value.GetUserInstructions(),
		Purpose:      decodePurpose(value.GetPurpose()),
		Requests:     decodeParticipants(value.GetRequestParticipants()),
		Generators:   decodeParticipants(value.GetGeneratorParticipants()),
		Results:      decodeParticipants(value.GetResultParticipants()),
	}
}

// decodeOutgoing maps frozen outgoing conversation input into policy data.
func decodeOutgoing(value *pb.CompactionOutgoingInput) compaction.Input {
	return compaction.Input{
		Binding:      compaction.Binding{},
		Entries:      nil,
		History:      DecodeHistory(value.GetHistory()),
		Previous:     mo.None[compaction.Result](),
		Model:        decodeModel(value.GetModel()),
		Reasoning:    value.GetReasoningChoice(),
		Instructions: value.GetInstructions(),
		ToolText:     decodeToolText(value.GetTools()),
		ToolShape:    "",
		Guidance:     "",
		Purpose:      compaction.PurposeUnspecified,
		Requests:     nil,
		Generators:   nil,
		Results:      nil,
	}
}

// decodeToolText collects textual and JSON contributions of captured tools.
func decodeToolText(values []*pb.CompactionToolDefinition) string {
	var text strings.Builder
	for _, value := range values {
		descriptor := value.GetDescriptor()
		text.WriteString(descriptor.GetName())
		text.WriteByte('\n')
		text.WriteString(descriptor.GetDescription())
		text.WriteByte('\n')
		text.Write(descriptor.GetInputSchemaJson())
		text.WriteByte('\n')
		text.WriteString(value.GetGrammarInputProperty())
		constraint := descriptor.GetConstrainedSampling()
		if schema := constraint.GetJsonSchema(); schema != nil {
			text.WriteString(schema.GetStrictness().String())
		}
		if grammar := constraint.GetGrammar(); grammar != nil {
			text.WriteString(grammar.GetLark())
			text.WriteString(grammar.GetRegex())
		}
		text.WriteByte('\n')
	}
	return text.String()
}

// decodeParticipants maps captured capability identities into policy data.
func decodeParticipants(values []*pb.CompactionParticipant) []compaction.Participant {
	participants := make([]compaction.Participant, 0, len(values))
	for _, value := range values {
		participants = append(
			participants,
			compaction.Participant{
				Extension: value.GetExtensionId(),
				Runtime:   value.GetRuntimeId(),
				Handler:   value.GetHandlerId(),
				Order:     value.GetRegistrationOrder(),
			},
		)
	}
	return participants
}

// EncodeParticipant maps a captured capability identity.
func EncodeParticipant(value compaction.Participant) *pb.CompactionParticipant {
	return pb.CompactionParticipant_builder{
		ExtensionId:       new(value.Extension),
		RuntimeId:         new(value.Runtime),
		HandlerId:         new(value.Handler),
		RegistrationOrder: new(value.Order),
	}.Build()
}

// decodeRequest maps a composed compaction request into policy data.
func decodeRequest(value *pb.CompactionRequest) compaction.Request {
	return compaction.Request{
		Trigger:       decodeTrigger(value.GetTrigger()),
		Retry:         value.GetRetryIntent(),
		Guidance:      value.GetInstructions(),
		Model:         decodeModel(value.GetModel()),
		Reasoning:     value.GetReasoningChoice(),
		Prefix:        decodeInputEntries(value.GetPrefix()),
		Suffix:        decodeInputEntries(value.GetSuffix()),
		Previous:      decodeMarker(value.GetPrevious()),
		ContextTokens: value.GetContextTokens(),
		Estimated:     value.GetContextTokensEstimated(),
		Window:        value.GetContextWindow(),
		Reserve:       value.GetResponseBudget(),
		Retained:      value.GetRetainedBudget(),
	}
}

// decodeInputEntries maps original entries and producer estimates into policy data.
func decodeInputEntries(values []*pb.CompactionInputEntry) []compaction.Entry {
	entries := make([]compaction.Entry, 0, len(values))
	for _, value := range values {
		entry := decodeEntry(value.GetEntry())
		entry.Tokens = value.GetEstimatedTokens()
		entries = append(entries, entry)
	}
	return entries
}

// EncodeRequest maps a request to the public participant contract.
func EncodeRequest(value compaction.Request) *pb.CompactionRequest {
	request := pb.CompactionRequest_builder{
		Trigger:                new(encodeTrigger(value.Trigger)),
		RetryIntent:            new(value.Retry),
		Instructions:           new(value.Guidance),
		Model:                  encodeModel(value.Model),
		ReasoningChoice:        new(value.Reasoning),
		Prefix:                 encodeInputEntries(value.Prefix),
		Suffix:                 encodeInputEntries(value.Suffix),
		Previous:               nil,
		ContextTokens:          new(value.ContextTokens),
		ContextTokensEstimated: new(value.Estimated),
		ContextWindow:          new(value.Window),
		ResponseBudget:         new(value.Reserve),
		RetainedBudget:         new(value.Retained),
	}.Build()
	if previous, present := value.Previous.Get(); present {
		request.SetPrevious(encodeMarker(previous))
	}
	return request
}

// encodeInputEntries maps original entry identities and producer estimates.
func encodeInputEntries(values []compaction.Entry) []*pb.CompactionInputEntry {
	entries := make([]*pb.CompactionInputEntry, 0, len(values))
	for index := range values {
		value := values[index]
		entries = append(
			entries,
			pb.CompactionInputEntry_builder{Entry: encodeEntry(value), EstimatedTokens: new(value.Tokens)}.Build(),
		)
	}
	return entries
}

// EncodeInvocation maps original and current composition state to an invocation.
func EncodeInvocation(value compaction.State) *pb.CompactionRequestInvocation {
	invocation := pb.CompactionRequestInvocation_builder{
		Original:      EncodeRequest(value.Original),
		Current:       EncodeRequest(value.Current),
		CurrentResult: nil,
	}.Build()
	if result, present := value.Result.Get(); present {
		invocation.SetCurrentResult(EncodeResult(result))
	}
	return invocation
}

// encodeState maps acquired policy state for terminal reporting.
func encodeState(value compaction.State) *pb.CompactionPolicyState {
	state := pb.CompactionPolicyState_builder{
		OriginalRequest: EncodeRequest(value.Original),
		CurrentRequest:  EncodeRequest(value.Current),
		Result:          nil,
	}.Build()
	if result, present := value.Result.Get(); present {
		state.SetResult(EncodeResult(result))
	}
	return state
}

// DecodeRequestAction maps updates to the compaction request and result.
func DecodeRequestAction(value *pb.CompactionRequestAction) compaction.RequestAction {
	action := compaction.RequestAction{
		Cancel:             value.GetCancel(),
		RequestDisposition: decodeRequestDisposition(value.GetRequestAction()),
		Request:            mo.None[compaction.Request](),
		ResultDisposition:  decodeResultDisposition(value.GetResultAction()),
		Result:             mo.None[compaction.Result](),
	}
	if request := value.GetRequest(); request != nil {
		action.Request = mo.Some(decodeRequest(request))
	}
	if result := value.GetResult(); result != nil {
		action.Result = mo.Some(DecodeResult(result))
	}
	return action
}

// DecodeResultAction maps an update to an existing compaction result.
func DecodeResultAction(value *pb.CompactionResultAction) compaction.ResultAction {
	action := compaction.ResultAction{
		Cancel:      value.GetCancel(),
		Disposition: decodeResultDisposition(value.GetResultAction()),
		Result:      mo.None[compaction.Result](),
	}
	if result := value.GetResult(); result != nil {
		action.Result = mo.Some(DecodeResult(result))
	}
	return action
}
