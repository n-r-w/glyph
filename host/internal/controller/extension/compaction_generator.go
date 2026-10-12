package extension

import (
	"context"
	"errors"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/errtree"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// newPolicyGeneratorContextRequest decodes one selected generator's public Host operation.
func newPolicyGeneratorContextRequest(
	mapped contextRequest,
	wire *extensionpb.InvokeCompactionGeneratorRequest,
) (contextRequest, error) {
	if wire == nil || wire.GetOperationId() == "" || wire.GetParticipant() == nil || wire.GetInvocation() == nil {
		return contextRequest{}, errors.New("complete captured generator invocation is required")
	}
	if wire.GetInvocation().GetCurrentResult() != nil {
		return contextRequest{}, errors.New("generator invocation cannot supply a current result")
	}
	original, err := decodePolicyRequest(wire.GetInvocation().GetOriginal())
	if err != nil {
		return contextRequest{}, fmt.Errorf("decode original generator request: %w", err)
	}
	current, err := decodePolicyRequest(wire.GetInvocation().GetCurrent())
	if err != nil {
		return contextRequest{}, fmt.Errorf("decode current generator request: %w", err)
	}
	target := wire.GetParticipant()
	command := CompactionGeneratorCommand{
		OperationID: wire.GetOperationId(),
		Participant: CompactionParticipant{
			ExtensionID: target.GetExtensionId(), RuntimeID: target.GetRuntimeId(),
			HandlerID: target.GetHandlerId(), RegistrationOrder: target.GetRegistrationOrder(),
		},
		Original: original, Current: current,
	}
	mapped.reference = wire.GetContext()
	mapped.admission = contextAdmissionCandidate
	mapped.prepare = func(o *contextOperation) (contextAction, error) {
		return contextReadAction(func(ctx context.Context) (*extensionpb.HostCompleted, error) {
			result, invokeErr := o.service.compactionPolicy.InvokePolicyGenerator(
				ctx, o.service.extensionID, o.service.runtimeID, command,
			)
			if invokeErr != nil {
				if isGeneratorCancellation(invokeErr) {
					return nil, invokeErr
				}
				if failure, classified := errors.AsType[interface {
					error
					Code() string
				}](invokeErr); classified {
					invokeErr = fmt.Errorf("generator category %s: %w", failure.Code(), invokeErr)
				}
				return nil, extensionsdk.Fail(compactionFailureCode(invokeErr), invokeErr)
			}
			response := extensionpb.CompactionResult_builder{
				Summary: new(result.Summary), FirstKeptEntryId: new(result.FirstKeptEntryID),
				Source: mapCompactionSource(result.Source), Details: nil,
			}.Build()
			if details, present := result.Details.Get(); present {
				response.SetDetails(append([]byte{}, details...))
			}
			completed := new(extensionpb.HostCompleted)
			completed.SetInvokeCompactionGenerator(response)
			return completed, nil
		}), nil
	}
	return mapped, nil
}

// isGeneratorCancellation distinguishes explicit cancellation from classified or mixed failure.
func isGeneratorCancellation(err error) bool {
	if _, classified := errors.AsType[interface {
		error
		Code() string
	}](err); classified {
		return false
	}
	if _, classified := errors.AsType[interface {
		error
		CompactionFailureCode() string
	}](err); classified {
		return false
	}
	return errtree.AllLeavesMatch(err, func(leaf error) bool { return errors.Is(leaf, context.Canceled) })
}

// newPolicyParticipantContextRequest selects the decoder for a captured participant's operation.
func newPolicyParticipantContextRequest(
	mapped contextRequest,
	request *extensionpb.ExtensionRequest,
) (contextRequest, error) {
	if wire := request.GetInvokeCompactionRequest(); wire != nil {
		return newPolicyRequestContextRequest(mapped, wire)
	}
	return newPolicyGeneratorContextRequest(mapped, request.GetInvokeCompactionGenerator())
}
