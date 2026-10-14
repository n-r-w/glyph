// Package runtime adapts Extension plugin invocations to Host contracts.
package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"

	controllerextension "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	extensionmanager "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// InvokeCompactionPolicy invokes the policy through the asynchronous handler transport.
func (r *Runtime) InvokeCompactionPolicy(
	ctx context.Context,
	handlerID string,
	binding extension.Context,
	invocation extensioncontext.PolicyInvocation,
) (bool, error) {
	input, err := mapPolicyInvocation(invocation, binding.RuntimeInstanceID)
	if err != nil {
		return false, fmt.Errorf("map compaction policy input: %w", err)
	}
	request := new(extensionpb.HandleRequest)
	request.SetHandlerId(handlerID)
	request.SetContext(mapContext(binding))
	request.SetParentCompactionId(invocation.LifecycleID)
	request.SetCompactionPolicy(input)
	response, err := r.runCompactionHandler(ctx, handlerID, request)
	if response == nil || response.GetError() != nil {
		return false, err
	}
	result := response.GetCompactionPolicy()
	if result == nil {
		return false, errors.Join(err, r.protocolViolation(errors.New("compaction policy response is missing")))
	}
	if result.GetError() != "" {
		err = errors.Join(err, newOrdinaryHandlerError(result.GetError()))
	}
	if rawState := result.GetState(); rawState != nil {
		acquired, stateErr := mapPolicyStateFromProto(rawState)
		if stateErr != nil {
			return result.GetCanceled(), errors.Join(err, stateErr)
		}
		invocation.RecordState(acquired)
	}
	if err != nil {
		return result.GetCanceled(), err
	}
	return result.GetCanceled(), nil
}

// mapPolicyInvocation encodes immutable input without private replay payloads.
func mapPolicyInvocation(
	invocation extensioncontext.PolicyInvocation,
	runtimeID string,
) (*extensionpb.CompactionPolicyInvocation, error) {
	snapshot := invocation.Snapshot
	if snapshot.Identity.Incarnation > math.MaxInt64 {
		return nil, errors.New("session incarnation exceeds the policy input range")
	}
	entries := make([]*extensionpb.CompactionSnapshotEntry, 0, len(snapshot.Entries))
	for index := range snapshot.Entries {
		entry := &snapshot.Entries[index]
		projected, err := mapSessionEntry(extensionmanager.ProjectTreeEntry(*entry))
		if err != nil {
			return nil, err
		}
		var privateBytes int64
		if response, present := entry.Model.Get(); present {
			privateBytes = controllerextension.CountCompactionPrivateBytes(response)
		}
		entries = append(
			entries,
			extensionpb.CompactionSnapshotEntry_builder{
				Entry:               projected,
				PrivatePayloadBytes: new(privateBytes),
			}.Build(),
		)
	}
	history, err := controllerextension.MapCompactionHistory(snapshot.History)
	if err != nil {
		return nil, err
	}
	var previous *extensionpb.SessionTreeCompaction
	if marker, present := snapshot.Previous.Get(); present {
		previous = controllerextension.MapCompactionMarker(marker)
	}
	tools, err := mapPolicyTools(invocation.Input.Tools)
	if err != nil {
		return nil, err
	}
	var leaf *string
	if value, present := snapshot.ActiveLeafID.Get(); present {
		leaf = new(value)
	}
	var instructions *string
	if value, present := invocation.Instructions.Get(); present {
		instructions = new(value)
	}
	participants := make([]*extensionpb.CompactionParticipant, len(invocation.Requests))
	for order, participant := range invocation.Requests {
		participants[order] = extensionpb.CompactionParticipant_builder{
			ExtensionId: new(participant.ExtensionID), RuntimeId: new(participant.RuntimeID),
			HandlerId: new(participant.HandlerID), RegistrationOrder: new(int64(order)),
		}.Build()
	}
	generators := make([]*extensionpb.CompactionParticipant, len(invocation.Generators))
	for order, participant := range invocation.Generators {
		generators[order] = extensionpb.CompactionParticipant_builder{
			ExtensionId: new(participant.ExtensionID), RuntimeId: new(participant.RuntimeID),
			HandlerId: new(participant.HandlerID), RegistrationOrder: new(int64(order)),
		}.Build()
	}
	results := make([]*extensionpb.CompactionParticipant, len(invocation.Results))
	for order, participant := range invocation.Results {
		results[order] = extensionpb.CompactionParticipant_builder{
			ExtensionId: new(participant.ExtensionID), RuntimeId: new(participant.RuntimeID),
			HandlerId: new(participant.HandlerID), RegistrationOrder: new(int64(order)),
		}.Build()
	}
	return extensionpb.CompactionPolicyInvocation_builder{
		OperationId: new(invocation.OperationID),
		SessionId:   new(snapshot.Identity.ID),
		SessionIncarnation: new(
			int64(snapshot.Identity.Incarnation),
		),
		ActiveLeafId:    leaf,
		Entries:         entries,
		History:         history,
		Previous:        previous,
		Model:           mapCompactionModel(invocation.Input.Model),
		ReasoningChoice: new(string(invocation.Input.ReasoningChoice)),
		Instructions: new(
			invocation.Input.Instructions,
		),
		Tools:                 tools,
		UserInstructions:      instructions,
		PolicyRuntimeId:       new(runtimeID),
		RequestParticipants:   participants,
		GeneratorParticipants: generators,
		ResultParticipants:    results,
	}.Build(), nil
}

// mapPolicyTools encodes the captured outbound tool definitions.
func mapPolicyTools(descriptors []tool.Descriptor) ([]*extensionpb.CompactionToolDefinition, error) {
	result := make([]*extensionpb.CompactionToolDefinition, 0, len(descriptors))
	for index := range descriptors {
		descriptor := &descriptors[index]
		sampling := (*extensionpb.ConstrainedSampling)(nil)
		var property *string
		if constraint, present := descriptor.ConstrainedSampling.Get(); present {
			mapped, err := mapPolicySampling(constraint)
			if err != nil {
				return nil, err
			}
			sampling = mapped
			if value, hasProperty := constraint.GrammarInputProperty.Get(); hasProperty {
				property = new(value)
			}
		}
		result = append(
			result,
			extensionpb.CompactionToolDefinition_builder{Descriptor: extensionpb.ToolDescriptor_builder{
				Name: new(
					descriptor.Name,
				),
				Description:         new(descriptor.Description),
				InputSchemaJson:     bytes.Clone(descriptor.InputSchemaJSON),
				ConstrainedSampling: sampling,
			}.Build(), GrammarInputProperty: property}.Build(),
		)
	}
	return result, nil
}

// mapPolicySampling encodes the provider-neutral input constraint.
func mapPolicySampling(sampling tool.ConstrainedSampling) (*extensionpb.ConstrainedSampling, error) {
	mapped := new(extensionpb.ConstrainedSampling)
	switch sampling.Kind {
	case tool.ConstrainedSamplingJSONSchema:
		var strictness extensionpb.JsonSchemaStrictness
		switch sampling.JSONSchemaStrictness.OrEmpty() {
		case tool.JSONSchemaStrictPrefer:
			strictness = extensionpb.JsonSchemaStrictness_JSON_SCHEMA_STRICTNESS_PREFER
		case tool.JSONSchemaStrictRequire:
			strictness = extensionpb.JsonSchemaStrictness_JSON_SCHEMA_STRICTNESS_REQUIRE
		default:
			return nil, errors.New("captured JSON Schema strictness is unavailable")
		}
		mapped.SetJsonSchema(extensionpb.JsonSchemaConstrainedSampling_builder{Strictness: new(strictness)}.Build())
	case tool.ConstrainedSamplingGrammar:
		grammar := sampling.Grammar.OrEmpty()
		var lark *string
		var regex *string
		if value, present := grammar.Lark.Get(); present {
			lark = new(value)
		}
		if value, present := grammar.Regex.Get(); present {
			regex = new(value)
		}
		mapped.SetGrammar(extensionpb.GrammarConstrainedSampling_builder{Lark: lark, Regex: regex}.Build())
	default:
		return nil, errors.New("captured constrained sampling kind is unavailable")
	}
	return mapped, nil
}
