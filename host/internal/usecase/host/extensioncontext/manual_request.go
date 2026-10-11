// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"

	"github.com/samber/mo"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
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
	participant, err := parent.findRequestParticipant(command.Participant)
	if err != nil {
		return extensioncontroller.CompactionRequestResponse{}, err
	}
	bound, err := s.IssueContext(participant.ExtensionID)
	if err != nil {
		return extensioncontroller.CompactionRequestResponse{}, fmt.Errorf(
			"issue captured request participant context: %w",
			err,
		)
	}
	s.mutex.Lock()
	incarnation := s.bindings[participant.ExtensionID].incarnation
	s.mutex.Unlock()
	if bound.RuntimeInstanceID != participant.RuntimeID || bound.SessionID != parent.snapshot.Identity.ID ||
		incarnation != parent.snapshot.Identity.Incarnation {
		return extensioncontroller.CompactionRequestResponse{}, errors.New(
			"captured request participant runtime or session changed before invocation",
		)
	}
	// lifetimeID identifies nested work without granting policy authority.
	lifetimeID := rand.Text()
	childContext, cancel := context.WithTimeout(ctx, s.participantTimeout)
	child := &compactionOperation{
		authorityID:          "",
		lifecycleID:          lifetimeID,
		owner:                PolicyOwner(participant),
		snapshot:             CompactionSnapshot{},
		requests:             nil,
		context:              childContext,
		cancel:               cancel,
		mutex:                sync.Mutex{},
		children:             sync.WaitGroup{},
		closed:               false,
		replacementRequested: false,
		receipt:              mo.None[session.Entry](),
		causes:               nil,
		terminalError:        nil,
		state:                mo.None[contextcompaction.PolicyState](),
	}
	s.mutex.Lock()
	s.compactionOperations[lifetimeID] = child
	s.mutex.Unlock()
	invocation := RequestInvocation{
		ParentCompactionID: mo.Some(
			lifetimeID,
		),
		Original:      newRequest(command.Original),
		Current:       newRequest(command.Current),
		CurrentResult: mo.None[Result](),
	}
	if result, present := command.CurrentResult.Get(); present {
		invocation.CurrentResult = mo.Some(newResult(result))
	}
	action, invokeErr := s.policyRuntime.InvokeCompactionRequest(childContext, participant, bound, invocation)
	// Capture execution failure before child.finish cancels the lifetime during normal teardown.
	executionErr := errors.Join(childContext.Err(), context.Cause(childContext))
	_, acquired, terminal := child.finish()
	s.mutex.Lock()
	delete(s.compactionOperations, lifetimeID)
	s.mutex.Unlock()
	parent.mutex.Lock()
	parent.causes = errors.Join(parent.causes, acquired)
	parent.mutex.Unlock()
	if err = errors.Join(invokeErr, executionErr, terminal, ctx.Err(), context.Cause(ctx)); err != nil {
		return extensioncontroller.CompactionRequestResponse{}, fmt.Errorf(
			"request participant %q generation %q: %w",
			participant.HandlerID,
			participant.RuntimeID,
			errors.Join(err, acquired),
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
