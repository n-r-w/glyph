package extensionruntime

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
)

// InvokeCompactionResult dispatches a result handler from the captured runtime generation.
func (s *Service) InvokeCompactionResult(
	ctx context.Context,
	participant extensioncontext.Participant,
	binding extension.Context,
	invocation extensioncontext.ResultInvocation,
) (extensioncontext.ResultAction, error) {
	owner, err := s.compactionOwner(participant)
	if err != nil {
		return extensioncontext.ResultAction{}, err
	}
	result, invokeErr := owner.state.runtime.HandleCompactionResult(ctx, participant.HandlerID, binding, invocation)
	s.finishAndReport(ctx, owner, invokeErr)
	return result, invokeErr
}
