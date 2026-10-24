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
	participant *extensionpb.CompactionParticipant,
	invocation *extensionpb.CompactionRequestInvocation,
) (*CompactionRequestOperation, error) {
	operation, err := p.startCompactionParticipant(ctx, participant, invocation, hostRequestCompactionRequest)
	if err != nil {
		return nil, err
	}
	return &CompactionRequestOperation{operation: operation}, nil
}

// StartGenerator invokes one generator captured by the policy operation.
func (p *CompactionPolicyContext) StartGenerator(
	ctx context.Context,
	participant *extensionpb.CompactionParticipant,
	invocation *extensionpb.CompactionRequestInvocation,
) (*CompactionGeneratorOperation, error) {
	operation, err := p.startCompactionParticipant(ctx, participant, invocation, hostRequestCompactionGenerator)
	if err != nil {
		return nil, err
	}
	return &CompactionGeneratorOperation{operation: operation}, nil
}

// startCompactionParticipant binds one typed invocation to the policy's authority and lifetime.
func (p *CompactionPolicyContext) startCompactionParticipant(
	ctx context.Context,
	participant *extensionpb.CompactionParticipant,
	invocation *extensionpb.CompactionRequestInvocation,
	kind hostRequestKind,
) (*contextOperation, error) {
	if participant == nil || invocation == nil {
		return nil, errors.New("compaction participant and invocation are required")
	}
	target := proto.CloneOf(participant)
	input := proto.CloneOf(invocation)
	request := new(extensionpb.ExtensionRequest)
	request.SetParentCompactionId(p.parentCompactionID)
	if kind == hostRequestCompactionRequest {
		request.SetInvokeCompactionRequest(extensionpb.InvokeCompactionRequestRequest_builder{
			Context: p.extension.reference(), OperationId: new(p.invocation.GetOperationId()),
			Participant: target, Invocation: input,
		}.Build())
	} else {
		request.SetInvokeCompactionGenerator(extensionpb.InvokeCompactionGeneratorRequest_builder{
			Context: p.extension.reference(), OperationId: new(p.invocation.GetOperationId()),
			Participant: target, Invocation: input,
		}.Build())
	}
	return p.extension.start(ctx, request)
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

// CompactionGeneratorOperation owns one asynchronous generator result.
type CompactionGeneratorOperation struct {
	// operation owns the nested Host operation lifecycle.
	operation *contextOperation
}

// Wait returns the generator's result or its terminal canceled or failed outcome.
func (o *CompactionGeneratorOperation) Wait(ctx context.Context) (*extensionpb.CompactionResult, error) {
	completed, err := o.operation.wait(ctx, nil)
	if err != nil {
		return nil, err
	}
	return proto.CloneOf(completed.GetInvokeCompactionGenerator()), nil
}
