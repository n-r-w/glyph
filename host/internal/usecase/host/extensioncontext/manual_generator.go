package extensioncontext

import (
	"context"
	"fmt"

	"github.com/samber/mo"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// InvokePolicyGenerator invokes the policy-selected captured generator under a non-authorizing lifetime.
func (s *Service) InvokePolicyGenerator(
	ctx context.Context,
	extensionID, runtimeID string,
	command extensioncontroller.CompactionGeneratorCommand,
) (session.CompactionEntry, error) {
	parent, err := s.findCompactionOperation(extensionID, runtimeID, command.OperationID)
	if err != nil {
		return session.CompactionEntry{}, err
	}
	participant, err := parent.findParticipant(command.Participant, parent.generators)
	if err != nil {
		return session.CompactionEntry{}, err
	}
	childContext, child, bound, err := s.beginParticipant(ctx, parent, participant, generatorParticipantKind)
	if err != nil {
		return session.CompactionEntry{}, err
	}
	invocation := RequestInvocation{
		ParentCompactionID: mo.Some(child.lifecycleID),
		Original:           newRequest(command.Original), Current: newRequest(command.Current),
		CurrentResult: mo.None[Result](),
	}
	result, invokeErr := s.policyRuntime.InvokeCompactionGenerator(childContext, participant, bound, invocation)
	if err = s.finishParticipant(ctx, childContext, parent, child, invokeErr); err != nil {
		return session.CompactionEntry{}, fmt.Errorf(
			"generator participant %q generation %q: %w", participant.HandlerID, participant.RuntimeID, err,
		)
	}
	if producer, present := result.Source.ExtensionID.Get(); present && producer != participant.ExtensionID {
		return session.CompactionEntry{}, fmt.Errorf(
			"generator producer %q does not match the selected extension %q", producer, participant.ExtensionID,
		)
	}
	return session.CompactionEntry{
		Summary: result.Summary, FirstKeptEntryID: result.FirstKeptEntryID,
		Source: result.Source, Details: result.Details,
	}, nil
}
