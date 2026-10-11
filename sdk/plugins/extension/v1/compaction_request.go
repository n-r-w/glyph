// Package extensionv1 provides asynchronous Extension SDK operations.
package extensionv1

import (
	"context"
	"errors"

	"google.golang.org/protobuf/proto"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// StartRequestHandler starts one captured request participant under policy authority.
func (p *CompactionPolicyContext) StartRequestHandler(
	ctx context.Context,
	participant *extensionpb.CompactionRequestParticipant,
	invocation *extensionpb.CompactionRequestInvocation,
) (*CompactionRequestOperation, error) {
	if participant == nil || invocation == nil {
		return nil, errors.New("request participant and invocation are required")
	}
	request := new(extensionpb.ExtensionRequest)
	request.SetParentCompactionId(p.parentCompactionID)
	request.SetInvokeCompactionRequest(extensionpb.InvokeCompactionRequestRequest_builder{
		Context: p.extension.reference(), OperationId: new(p.invocation.GetOperationId()),
		Participant: proto.CloneOf(participant), Invocation: proto.CloneOf(invocation),
	}.Build())
	operation, err := p.extension.initiator.start(ctx, request)
	if err != nil {
		return nil, err
	}
	return &CompactionRequestOperation{operation: operation}, nil
}

// CompactionRequestOperation owns the asynchronous result of one participant call.
type CompactionRequestOperation struct {
	// operation owns Host lifecycle delivery.
	operation *contextOperation
}

// Wait supplies the typed action selected by the request participant.
func (o *CompactionRequestOperation) Wait(ctx context.Context) (*extensionpb.CompactionRequestAction, error) {
	result, err := o.operation.wait(ctx, nil)
	if err != nil {
		return nil, err
	}
	return proto.CloneOf(result.GetInvokeCompactionRequest()), nil
}
