package run

import (
	"context"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
)

// runEnd describes how a run finishes.
type runEnd struct {
	// outcome is the terminal outcome of the run.
	outcome agent.RunOutcome
	// errorMessage is the failure text that the run reports.
	errorMessage mo.Option[string]
	// err is the error that the run returns.
	err error
}

// nextStep is the decision after one turn: the run either finishes or continues after it gives the model the queued
// messages.
type nextStep struct {
	// end is present when the run finishes.
	end mo.Option[runEnd]
	// messages are the queued messages that the run appends to history before the next turn.
	messages []queuedMessage
}

// decideNextStep decides after a turn whether the run finishes or continues, and removes the queued messages that
// the run delivers. A completed turn takes the queued messages and moves the run to completion under one lock, so a
// message that arrives later fails to queue.
func (s *Service) decideNextStep(ctx context.Context, result turnResult, next bool, runErr error) nextStep {
	finishWithTurn := nextStep{
		end:      mo.Some(runEnd{outcome: result.Outcome, errorMessage: result.ErrorMessage, err: runErr}),
		messages: nil,
	}
	var queued []queuedMessage
	var cancelErr error
	switch {
	case next:
		queued, cancelErr = s.takeSteer(ctx)
	case result.Outcome == agent.RunOutcomeCompleted && runErr == nil:
		// A final response delivers a pending steer or followUp message instead of completing the run.
		queued, cancelErr = s.takeQueuedOrComplete(ctx)
		if cancelErr == nil && len(queued) == 0 {
			return finishWithTurn
		}
	default:
		return finishWithTurn
	}
	if cancelErr != nil {
		// A canceled run delivers no instruction to a model that a later run could see.
		return nextStep{
			end: mo.Some(runEnd{
				outcome: agent.RunOutcomeAborted, errorMessage: mo.Some(abortedModelMessage), err: cancelErr,
			}),
			messages: nil,
		}
	}
	return nextStep{end: mo.None[runEnd](), messages: queued}
}
