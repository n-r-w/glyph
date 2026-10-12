package extension

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// newPolicyResultContextRequest binds one selected result call to policy authority.
func newPolicyResultContextRequest(
	mapped contextRequest,
	wire *extensionpb.InvokeCompactionResultRequest,
) (contextRequest, error) {
	if wire == nil || wire.GetOperationId() == "" || wire.GetParticipant() == nil || wire.GetInvocation() == nil {
		return contextRequest{}, errors.New("complete captured result invocation is required")
	}
	input := wire.GetInvocation()
	original, err := decodePolicyRequest(input.GetOriginalRequest())
	if err != nil {
		return contextRequest{}, fmt.Errorf("decode original result request: %w", err)
	}
	current, err := decodePolicyRequest(input.GetCurrentRequest())
	if err != nil {
		return contextRequest{}, fmt.Errorf("decode current result request: %w", err)
	}
	originalResult, err := decodePolicyResult(input.GetOriginalResult())
	if err != nil {
		return contextRequest{}, fmt.Errorf("decode original result: %w", err)
	}
	currentResult, err := decodePolicyResult(input.GetCurrentResult())
	if err != nil {
		return contextRequest{}, fmt.Errorf("decode current result: %w", err)
	}
	target := wire.GetParticipant()
	command := CompactionResultCommand{
		OperationID: wire.GetOperationId(),
		Participant: CompactionParticipant{
			ExtensionID:       target.GetExtensionId(),
			RuntimeID:         target.GetRuntimeId(),
			HandlerID:         target.GetHandlerId(),
			RegistrationOrder: target.GetRegistrationOrder(),
		},
		OriginalRequest: original,
		CurrentRequest:  current,
		OriginalResult:  originalResult,
		CurrentResult:   currentResult,
	}
	mapped.reference = wire.GetContext()
	mapped.admission = contextAdmissionCandidate
	mapped.prepare = func(o *contextOperation) (contextAction, error) {
		return contextReadAction(func(ctx context.Context) (*extensionpb.HostCompleted, error) {
			action, invokeErr := o.service.compactionPolicy.InvokePolicyResult(
				ctx,
				o.service.extensionID,
				o.service.runtimeID,
				command,
			)
			if invokeErr != nil {
				if isParticipantCancellation(invokeErr) {
					return nil, invokeErr
				}
				if failure, classified := errors.AsType[interface {
					error
					Code() string
				}](invokeErr); classified {
					invokeErr = fmt.Errorf("result category %s: %w", failure.Code(), invokeErr)
				}
				return nil, extensionsdk.Fail(compactionFailureCode(invokeErr), invokeErr)
			}
			completed := new(extensionpb.HostCompleted)
			completed.SetInvokeCompactionResult(action.encodePolicyResultAction())
			return completed, nil
		}), nil
	}
	return mapped, nil
}

// decodePolicyResult decodes a required compaction result for session use.
func decodePolicyResult(input *extensionpb.CompactionResult) (session.CompactionEntry, error) {
	if input == nil {
		return session.CompactionEntry{}, errors.New("complete compaction result is required")
	}
	source, err := mapPolicyCandidateSource(input.GetSource())
	if err != nil {
		return session.CompactionEntry{}, err
	}
	return session.CompactionEntry{
		Summary:          input.GetSummary(),
		FirstKeptEntryID: input.GetFirstKeptEntryId(),
		Source:           source,
		Details:          mo.TupleToOption(append([]byte{}, input.GetDetails()...), input.HasDetails()),
		EstimatedCost:    mo.None[session.EstimatedCost](),
	}, nil
}
