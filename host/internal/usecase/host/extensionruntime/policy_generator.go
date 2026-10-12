package extensionruntime

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
)

// InvokeCompactionGenerator dispatches the selected captured generator through its runtime owner.
func (s *Service) InvokeCompactionGenerator(
	ctx context.Context,
	participant extensioncontext.Participant,
	binding extension.Context,
	invocation extensioncontext.RequestInvocation,
) (extensioncontext.Result, error) {
	handler := contextcompaction.Handler{
		ExtensionID: participant.ExtensionID, RuntimeID: participant.RuntimeID, HandlerID: participant.HandlerID,
	}
	owner, err := s.compactionOwner(handler)
	if err != nil {
		return extensioncontext.Result{}, err
	}
	result, invokeErr := owner.state.runtime.GenerateCompaction(
		ctx, participant.HandlerID, binding, mapPolicyRequestInvocation(invocation),
	)
	s.finishAndReport(ctx, owner, invokeErr)
	return mapPolicyResponseResult(result), invokeErr
}
