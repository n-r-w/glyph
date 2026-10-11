// Package extensionv1 provides asynchronous Extension SDK operations.
package extensionv1

import (
	"context"

	"google.golang.org/protobuf/proto"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// compactionPolicyContextKey identifies invocation-scoped policy authority.
type compactionPolicyContextKey struct{}

// parentCompactionContextKey identifies non-authorizing nested-work correlation.
type parentCompactionContextKey struct{}

// CompactionPolicyContext supplies captured input and operation-scoped policy services.
type CompactionPolicyContext struct {
	// invocation retains the immutable policy input.
	invocation *extensionpb.CompactionPolicyInvocation
	// extension supplies session-bound asynchronous Host operations.
	extension *ExtensionContext
	// parentCompactionID carries non-authorizing nested-work correlation.
	parentCompactionID string
}

// StartProjectReplacement requests structurally valid model history for a replacement candidate.
func (p *CompactionPolicyContext) StartProjectReplacement(
	ctx context.Context,
	candidate *extensionpb.CompactionResult,
) (*CompactionProjectionOperation, error) {
	request := new(extensionpb.ExtensionRequest)
	request.SetParentCompactionId(p.parentCompactionID)
	request.SetProjectCompaction(p.newCandidateRequest(candidate))
	started, err := p.extension.initiator.start(ctx, request)
	if err != nil {
		return nil, err
	}
	return &CompactionProjectionOperation{operation: started}, nil
}

// StartReplaceHistory requests one durable replacement for the captured operation.
func (p *CompactionPolicyContext) StartReplaceHistory(
	ctx context.Context,
	candidate *extensionpb.CompactionResult,
) (*CompactionReplacementOperation, error) {
	request := new(extensionpb.ExtensionRequest)
	request.SetParentCompactionId(p.parentCompactionID)
	request.SetReplaceCompaction(p.newCandidateRequest(candidate))
	started, err := p.extension.initiator.start(ctx, request)
	if err != nil {
		return nil, err
	}
	return &CompactionReplacementOperation{operation: started}, nil
}

// newCandidateRequest binds a detached candidate to the issued policy authority.
func (p *CompactionPolicyContext) newCandidateRequest(
	candidate *extensionpb.CompactionResult,
) *extensionpb.CompactionCandidateRequest {
	return extensionpb.CompactionCandidateRequest_builder{
		Context:     p.extension.reference(),
		OperationId: new(p.invocation.GetOperationId()), Result: proto.CloneOf(candidate),
	}.Build()
}

// Input returns a detached copy of the captured policy input.
func (p *CompactionPolicyContext) Input() *extensionpb.CompactionPolicyInvocation {
	return proto.CloneOf(p.invocation)
}

// CompactionProjectionOperation tracks one candidate projection request.
type CompactionProjectionOperation struct {
	// operation owns the asynchronous Host request.
	operation *contextOperation
}

// Wait awaits the candidate's structurally valid model history.
func (o *CompactionProjectionOperation) Wait(ctx context.Context) (*extensionpb.CompactionProjection, error) {
	completed, err := o.operation.wait(ctx, nil)
	if err != nil {
		return nil, err
	}
	return completed.GetProjectCompaction(), nil
}

// CompactionReplacementOperation tracks one durable replacement request.
type CompactionReplacementOperation struct {
	// operation owns the asynchronous Host request.
	operation *contextOperation
}

// Wait awaits the durable replacement receipt and diagnostics.
func (o *CompactionReplacementOperation) Wait(ctx context.Context) (*extensionpb.CompactResult, error) {
	completed, err := o.operation.wait(ctx, nil)
	if err != nil {
		return nil, err
	}
	return completed.GetReplaceCompaction(), nil
}

// FindCompactionPolicy retrieves authority issued only to the owning policy invocation.
func FindCompactionPolicy(ctx context.Context) (*CompactionPolicyContext, bool) {
	policy, found := ctx.Value(compactionPolicyContextKey{}).(*CompactionPolicyContext)
	return policy, found
}
