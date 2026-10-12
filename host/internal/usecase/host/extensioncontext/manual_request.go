// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"context"
	"fmt"

	"github.com/samber/mo"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// InvokePolicyRequest invokes one captured participant under a separate non-authorizing lifetime.
func (s *Service) InvokePolicyRequest(
	ctx context.Context,
	extensionID, runtimeID string,
	command extensioncontroller.CompactionRequestCommand,
) (extensioncontroller.CompactionRequestResponse, error) {
	parent, err := s.findCompactionOperation(extensionID, runtimeID, command.OperationID)
	if err != nil {
		return extensioncontroller.CompactionRequestResponse{}, err
	}
	participant, err := parent.findParticipant(command.Participant, parent.requests)
	if err != nil {
		return extensioncontroller.CompactionRequestResponse{}, err
	}
	childContext, child, bound, err := s.beginParticipant(ctx, parent, participant, requestParticipantKind)
	if err != nil {
		return extensioncontroller.CompactionRequestResponse{}, err
	}
	invocation := RequestInvocation{
		ParentCompactionID: mo.Some(
			child.lifecycleID,
		),
		Original:      newRequest(command.Original),
		Current:       newRequest(command.Current),
		CurrentResult: mo.None[Result](),
	}
	if result, present := command.CurrentResult.Get(); present {
		invocation.CurrentResult = mo.Some(newResult(result))
	}
	action, invokeErr := s.policyRuntime.InvokeCompactionRequest(childContext, participant, bound, invocation)
	if err = s.finishParticipant(ctx, childContext, parent, child, invokeErr); err != nil {
		return extensioncontroller.CompactionRequestResponse{}, fmt.Errorf(
			"request participant %q generation %q: %w", participant.HandlerID, participant.RuntimeID, err,
		)
	}
	response := extensioncontroller.CompactionRequestResponse{
		Cancel:        action.Cancel,
		RequestAction: extensioncontroller.CompactionRequestChange(action.RequestAction),
		Request:       mo.None[extensioncontroller.CompactionRequestValue](),
		ResultAction:  extensioncontroller.CompactionResultChange(action.ResultAction),
		Result:        mo.None[session.CompactionEntry](),
	}
	if request, present := action.Request.Get(); present {
		response.Request = mo.Some(request.mapToControllerValue())
	}
	if result, present := action.Result.Get(); present {
		response.Result = mo.Some(
			session.CompactionEntry{
				Summary:          result.Summary,
				FirstKeptEntryID: result.FirstKeptEntryID,
				Source:           result.Source,
				Details:          result.Details,
				EstimatedCost:    mo.None[session.EstimatedCost](),
			},
		)
	}
	return response, nil
}
