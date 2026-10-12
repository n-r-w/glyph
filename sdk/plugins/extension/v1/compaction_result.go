package extensionv1

import (
	"context"
	"errors"

	"google.golang.org/protobuf/proto"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// StartResultHandler invokes the selected captured result participant.
func (p *CompactionPolicyContext) StartResultHandler(
	ctx context.Context,
	participant *extensionpb.CompactionParticipant,
	invocation *extensionpb.CompactionResultInvocation,
) (*CompactionResultOperation, error) {
	if participant == nil || invocation == nil {
		return nil, errors.New("compaction participant and result invocation are required")
	}
	request := new(extensionpb.ExtensionRequest)
	request.SetParentCompactionId(p.parentCompactionID)
	request.SetInvokeCompactionResult(
		extensionpb.InvokeCompactionResultRequest_builder{
			Context:     p.extension.reference(),
			OperationId: new(p.invocation.GetOperationId()),
			Participant: proto.CloneOf(participant),
			Invocation:  proto.CloneOf(invocation),
		}.Build(),
	)
	operation, err := p.extension.initiator.start(ctx, request)
	if err != nil {
		return nil, err
	}
	return &CompactionResultOperation{operation: operation}, nil
}

// CompactionResultOperation owns a selected participant's Host call.
type CompactionResultOperation struct {
	// operation supplies asynchronous completion and cancellation.
	operation *contextOperation
}

// Wait supplies the selected participant's typed action.
func (o *CompactionResultOperation) Wait(ctx context.Context) (*extensionpb.CompactionResultAction, error) {
	result, err := o.operation.wait(ctx, nil)
	if err != nil {
		return nil, err
	}
	return proto.CloneOf(result.GetInvokeCompactionResult()), nil
}
