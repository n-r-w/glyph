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
	// input and err contain the captured public policy input and its encoding failure.
	input, err := mapPolicyInvocation(invocation, binding.RuntimeInstanceID)
	if err != nil {
		return false, fmt.Errorf("map compaction policy input: %w", err)
	}
	// request contains the policy handler invocation and parent correlation.
	request := new(extensionpb.HandleRequest)
	request.SetHandlerId(handlerID)
	request.SetContext(mapContext(binding))
	request.SetParentCompactionId(invocation.LifecycleID)
	request.SetCompactionPolicy(input)
	// response and err contain the handler completion and transport failure.
	response, err := r.runCompactionHandler(ctx, handlerID, request)
	// result contains policy diagnostics and cancellation state.
	result := response.GetCompactionPolicy()
	if result.GetError() != "" {
		err = errors.Join(err, newOrdinaryHandlerError(result.GetError()))
	}
	// rawState contains the acquired policy diagnostic payload.
	if rawState := result.GetState(); rawState != nil {
		// acquired and stateErr contain policy diagnostic state and its decoding failure.
		acquired, stateErr := mapPolicyStateFromProto(rawState)
		if stateErr != nil {
			return result.GetCanceled(), errors.Join(err, stateErr)
		}
		invocation.RecordState(acquired)
	}
	if err != nil {
		return result.GetCanceled(), err
	}
	if result == nil {
		return false, r.protocolViolation(errors.New("compaction policy response is missing"))
	}
	return result.GetCanceled(), nil
}

// mapPolicyInvocation encodes immutable input without private replay payloads.
func mapPolicyInvocation(
	invocation extensioncontext.PolicyInvocation,
	runtimeID string,
) (*extensionpb.CompactionPolicyInvocation, error) {
	// snapshot contains the captured session input.
	snapshot := invocation.Snapshot
	if snapshot.Identity.Incarnation > math.MaxInt64 {
		return nil, errors.New("session incarnation exceeds the policy input range")
	}
	// entries contains public original-branch records and private payload sizes.
	entries := make([]*extensionpb.CompactionSnapshotEntry, 0, len(snapshot.Entries))
	for index := range snapshot.Entries {
		// entry identifies the captured original-branch record.
		entry := &snapshot.Entries[index]
		// projected and err contain the public session entry and its encoding failure.
		projected, err := mapSessionEntry(extensionmanager.ProjectTreeEntry(*entry))
		if err != nil {
			return nil, err
		}
		// privateBytes contains the hidden provider-payload size in bytes.
		var privateBytes int64
		// response contains an optional model response; present indicates its availability.
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
	// history and err contain public prepared model history and its encoding failure.
	history, err := controllerextension.MapCompactionHistory(snapshot.History)
	if err != nil {
		return nil, err
	}
	// previous contains the optional persisted replacement marker.
	var previous *extensionpb.SessionTreeCompaction
	// marker contains the previous replacement; present indicates its availability.
	if marker, present := snapshot.Previous.Get(); present {
		previous = mapCompactionMarker(marker)
	}
	// tools and err contain captured tool definitions and their encoding failure.
	tools, err := mapPolicyTools(invocation.Input.Tools)
	if err != nil {
		return nil, err
	}
	// leaf contains the optional active branch leaf identifier.
	var leaf *string
	// value contains the captured leaf identifier; present indicates its availability.
	if value, present := snapshot.ActiveLeafID.Get(); present {
		leaf = new(value)
	}
	// instructions contains optional manual user guidance.
	var instructions *string
	// value contains manual user guidance; present indicates its availability.
	if value, present := invocation.Instructions.Get(); present {
		instructions = new(value)
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
		Tools:            tools,
		UserInstructions: instructions,
		PolicyRuntimeId:  new(runtimeID),
	}.Build(), nil
}

// mapPolicyTools encodes the captured outbound tool definitions.
func mapPolicyTools(descriptors []tool.Descriptor) ([]*extensionpb.CompactionToolDefinition, error) {
	// result contains the captured public tool definitions.
	result := make([]*extensionpb.CompactionToolDefinition, 0, len(descriptors))
	for index := range descriptors {
		// descriptor identifies the captured outbound tool definition.
		descriptor := &descriptors[index]
		// sampling contains the optional public input constraint.
		sampling := (*extensionpb.ConstrainedSampling)(nil)
		// property contains the optional grammar input property name.
		var property *string
		// constraint contains the tool input constraint; present indicates its availability.
		if constraint, present := descriptor.ConstrainedSampling.Get(); present {
			// mapped and err contain the public constraint and its encoding failure.
			mapped, err := mapPolicySampling(constraint)
			if err != nil {
				return nil, err
			}
			sampling = mapped
			// value contains the grammar input property name; hasProperty indicates its availability.
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
	// mapped contains the public provider-neutral input constraint.
	mapped := new(extensionpb.ConstrainedSampling)
	switch sampling.Kind {
	case tool.ConstrainedSamplingJSONSchema:
		// strictness contains the public JSON Schema enforcement choice.
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
		// grammar contains the captured grammar alternatives.
		grammar := sampling.Grammar.OrEmpty()
		// lark contains the optional Lark grammar.
		var lark *string
		// regex contains the optional regular-expression grammar.
		var regex *string
		// value contains the Lark grammar; present indicates its availability.
		if value, present := grammar.Lark.Get(); present {
			lark = new(value)
		}
		// value contains the regular-expression grammar; present indicates its availability.
		if value, present := grammar.Regex.Get(); present {
			regex = new(value)
		}
		mapped.SetGrammar(extensionpb.GrammarConstrainedSampling_builder{Lark: lark, Regex: regex}.Build())
	default:
		return nil, errors.New("captured constrained sampling kind is unavailable")
	}
	return mapped, nil
}
