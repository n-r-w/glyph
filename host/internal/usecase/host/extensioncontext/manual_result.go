package extensioncontext

import (
	"context"
	"fmt"

	"github.com/samber/mo"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// InvokePolicyResult routes one captured participant under its bounded lifetime.
func (s *Service) InvokePolicyResult(
	ctx context.Context,
	extensionID, runtimeID string,
	command extensioncontroller.CompactionResultCommand,
) (extensioncontroller.CompactionResultResponse, error) {
	parent, err := s.findCompactionOperation(extensionID, runtimeID, command.OperationID)
	if err != nil {
		return extensioncontroller.CompactionResultResponse{}, err
	}
	participant, err := parent.findParticipant(command.Participant, parent.results)
	if err != nil {
		return extensioncontroller.CompactionResultResponse{}, err
	}
	const resultParticipantKind = "result"
	childContext, child, bound, err := s.beginParticipant(ctx, parent, participant, resultParticipantKind)
	if err != nil {
		return extensioncontroller.CompactionResultResponse{}, err
	}
	invocation := ResultInvocation{
		ParentCompactionID: mo.Some(child.lifecycleID),
		OriginalRequest:    newRequest(command.OriginalRequest),
		CurrentRequest:     newRequest(command.CurrentRequest),
		OriginalResult:     newResult(command.OriginalResult),
		CurrentResult:      newResult(command.CurrentResult),
	}
	action, invokeErr := s.policyRuntime.InvokeCompactionResult(childContext, participant, bound, invocation)
	if err = s.finishParticipant(ctx, childContext, parent, child, invokeErr); err != nil {
		return extensioncontroller.CompactionResultResponse{}, fmt.Errorf(
			"result participant %q generation %q: %w",
			participant.HandlerID,
			participant.RuntimeID,
			err,
		)
	}
	response := extensioncontroller.CompactionResultResponse{
		Cancel:       action.Cancel,
		ResultAction: extensioncontroller.CompactionResultChange(action.ResultAction),
		Result:       mo.None[session.CompactionEntry](),
	}
	if result, present := action.Result.Get(); present {
		if action.ResultAction == ResultActionReplace {
			if producer, attributed := result.Source.ExtensionID.Get(); attributed &&
				producer != participant.ExtensionID {
				return extensioncontroller.CompactionResultResponse{}, fmt.Errorf(
					"result producer %q does not match the selected extension %q",
					producer,
					participant.ExtensionID,
				)
			}
		}
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
