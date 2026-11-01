// Package extensionwire maps public Extension messages and compaction domain values.
package extensionwire

import (
	"fmt"
	"strings"

	"github.com/samber/mo"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// DecodeInput maps captured policy input into policy data.
func DecodeInput(value *pb.CompactionPolicyInvocation) (compaction.Input, error) {
	history, err := DecodeHistory(value.GetHistory())
	if err != nil {
		return compaction.Input{}, err
	}
	return compaction.Input{
		Binding:      compaction.Binding{},
		History:      history,
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
	}, nil
}

// DecodeOutgoing maps frozen outgoing conversation input into policy data.
func DecodeOutgoing(value *pb.CompactionOutgoingInput) (compaction.Input, error) {
	history, err := DecodeHistory(value.GetHistory())
	if err != nil {
		return compaction.Input{}, err
	}
	return compaction.Input{
		Binding:      compaction.Binding{},
		History:      history,
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
	}, nil
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

// DecodeRequest maps a composed compaction request into policy data.
func DecodeRequest(value *pb.CompactionRequest) (compaction.Request, error) {
	prefix, err := DecodeHistory(value.GetPrefix())
	if err != nil {
		return compaction.Request{}, fmt.Errorf("decode request prefix: %w", err)
	}
	suffix, err := DecodeHistory(value.GetSuffix())
	if err != nil {
		return compaction.Request{}, fmt.Errorf("decode request suffix: %w", err)
	}
	return compaction.Request{
		Trigger:       decodeTrigger(value.GetTrigger()),
		Retry:         value.GetRetryIntent(),
		Guidance:      value.GetInstructions(),
		Model:         decodeModel(value.GetModel()),
		Reasoning:     value.GetReasoningChoice(),
		Prefix:        prefix,
		Suffix:        suffix,
		Previous:      decodeMarker(value.GetPrevious()),
		ContextTokens: value.GetContextTokens(),
		Estimated:     value.GetContextTokensEstimated(),
		Window:        value.GetContextWindow(),
		Reserve:       value.GetResponseBudget(),
		Retained:      value.GetRetainedBudget(),
	}, nil
}

// EncodeRequest maps a request to the public participant contract.
func EncodeRequest(value compaction.Request) (*pb.CompactionRequest, error) {
	prefix, err := encodeHistory(value.Prefix)
	if err != nil {
		return nil, fmt.Errorf("encode request prefix: %w", err)
	}
	suffix, err := encodeHistory(value.Suffix)
	if err != nil {
		return nil, fmt.Errorf("encode request suffix: %w", err)
	}
	request := pb.CompactionRequest_builder{
		Trigger:                new(encodeTrigger(value.Trigger)),
		RetryIntent:            new(value.Retry),
		Instructions:           new(value.Guidance),
		Model:                  encodeModel(value.Model),
		ReasoningChoice:        new(value.Reasoning),
		Prefix:                 prefix,
		Suffix:                 suffix,
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
	return request, nil
}

// encodeRequests maps the original and current requests of a composition state.
func encodeRequests(value compaction.State) (original, current *pb.CompactionRequest, err error) {
	original, err = EncodeRequest(value.Original)
	if err != nil {
		return nil, nil, fmt.Errorf("encode original request: %w", err)
	}
	current, err = EncodeRequest(value.Current)
	if err != nil {
		return nil, nil, fmt.Errorf("encode current request: %w", err)
	}
	return original, current, nil
}

// EncodeInvocation maps original and current composition state to an invocation.
func EncodeInvocation(value compaction.State) (*pb.CompactionRequestInvocation, error) {
	original, current, err := encodeRequests(value)
	if err != nil {
		return nil, err
	}
	invocation := pb.CompactionRequestInvocation_builder{
		Original:      original,
		Current:       current,
		CurrentResult: nil,
	}.Build()
	if result, present := value.Result.Get(); present {
		invocation.SetCurrentResult(EncodeResult(result))
	}
	return invocation, nil
}

// EncodeState maps acquired policy state for terminal reporting.
func EncodeState(value compaction.State) (*pb.CompactionPolicyState, error) {
	original, current, err := encodeRequests(value)
	if err != nil {
		return nil, err
	}
	state := pb.CompactionPolicyState_builder{
		OriginalRequest: original,
		CurrentRequest:  current,
		Result:          nil,
	}.Build()
	if result, present := value.Result.Get(); present {
		state.SetResult(EncodeResult(result))
	}
	return state, nil
}

// DecodeRequestAction maps updates to the compaction request and result.
func DecodeRequestAction(value *pb.CompactionRequestAction) (compaction.RequestAction, error) {
	action := compaction.RequestAction{
		Cancel:             value.GetCancel(),
		RequestDisposition: decodeRequestDisposition(value.GetRequestAction()),
		Request:            mo.None[compaction.Request](),
		ResultDisposition:  decodeResultDisposition(value.GetResultAction()),
		Result:             mo.None[compaction.Result](),
	}
	if request := value.GetRequest(); request != nil {
		decoded, err := DecodeRequest(request)
		if err != nil {
			return compaction.RequestAction{}, err
		}
		action.Request = mo.Some(decoded)
	}
	if result := value.GetResult(); result != nil {
		action.Result = mo.Some(DecodeResult(result))
	}
	return action, nil
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
