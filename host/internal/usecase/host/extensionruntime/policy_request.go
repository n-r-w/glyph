// Package extensionruntime owns extension process lifetimes and routes scoped calls.
package extensionruntime

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
)

// InvokeCompactionRequest invokes the participant's compaction request handler in the captured runtime.
func (s *Service) InvokeCompactionRequest(
	ctx context.Context,
	participant extensioncontext.Participant,
	binding extension.Context,
	invocation extensioncontext.RequestInvocation,
) (extensioncontext.RequestAction, error) {
	owner, err := s.compactionOwner(participant)
	if err != nil {
		return extensioncontext.RequestAction{}, err
	}
	result, invokeErr := owner.state.runtime.HandleCompactionRequest(ctx, participant.HandlerID, binding, invocation)
	s.finishAndReport(ctx, owner, invokeErr)
	return result, invokeErr
}
