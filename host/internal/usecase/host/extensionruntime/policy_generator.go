package extensionruntime

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
)

// InvokeCompactionGenerator dispatches the selected captured generator through its runtime owner.
func (s *Service) InvokeCompactionGenerator(
	ctx context.Context,
	participant extensioncontext.Participant,
	binding extension.Context,
	invocation extensioncontext.RequestInvocation,
) (extensioncontext.Result, error) {
	owner, err := s.compactionOwner(participant)
	if err != nil {
		return extensioncontext.Result{}, err
	}
	result, invokeErr := owner.state.runtime.GenerateCompaction(ctx, participant.HandlerID, binding, invocation)
	s.finishAndReport(ctx, owner, invokeErr)
	return result, invokeErr
}
