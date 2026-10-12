package extensionruntime

import (
	"context"

	"github.com/samber/mo/option"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
)

// InvokeCompactionResult dispatches a result handler from the captured runtime generation.
func (s *Service) InvokeCompactionResult(
	ctx context.Context,
	participant extensioncontext.Participant,
	binding extension.Context,
	invocation extensioncontext.ResultInvocation,
) (extensioncontext.ResultAction, error) {
	handler := contextcompaction.Handler{
		ExtensionID: participant.ExtensionID,
		RuntimeID:   participant.RuntimeID,
		HandlerID:   participant.HandlerID,
	}
	owner, err := s.compactionOwner(handler)
	if err != nil {
		return extensioncontext.ResultAction{}, err
	}
	input := contextcompaction.ResultInvocation{
		ParentCompactionID: invocation.ParentCompactionID,
		OriginalRequest:    mapPolicyRequestValue(invocation.OriginalRequest),
		CurrentRequest:     mapPolicyRequestValue(invocation.CurrentRequest),
		OriginalResult:     mapPolicyRequestResult(invocation.OriginalResult),
		CurrentResult:      mapPolicyRequestResult(invocation.CurrentResult),
	}
	action, invokeErr := owner.state.runtime.HandleCompactionResult(ctx, participant.HandlerID, binding, input)
	s.finishAndReport(ctx, owner, invokeErr)
	kind := extensioncontext.ResultActionReplace
	if action.Cancel {
		kind = 0
	} else if action.Preserve {
		kind = extensioncontext.ResultActionPreserve
	}
	return extensioncontext.ResultAction{
		Cancel:       action.Cancel,
		ResultAction: kind,
		Result:       option.Map(mapPolicyResponseResult)(action.Result),
	}, invokeErr
}
