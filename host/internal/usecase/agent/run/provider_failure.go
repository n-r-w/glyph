package run

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/internal/errtree"
	"github.com/n-r-w/glyph/internal/errtree/diagnostic"
)

// finalizeProviderError stores retained partial content and excludes it from later projections.
func (s *Service) finalizeProviderError(
	ctx context.Context,
	runID string,
	response model.Response,
	providerErr error,
) (turnResult, bool, error) {
	response = response.Clone()
	if partial, present := s.State().PartialResponse.Get(); len(response.Content) == 0 && present {
		response.Content = partial.Content
		finalizeRetainedStreamedContent(response.Content)
	}
	outcome := model.OutcomeFailed
	if isPureCancellation(providerErr) {
		outcome = model.OutcomeAborted
	}
	errorMessage := providerFailureMessage(response, outcome, providerErr)
	response.Outcome = mo.Some(outcome)
	response.ErrorMessage = mo.Some(errorMessage)
	validationErr := response.ValidateTerminalContent()
	s.clearPartial()
	if validationErr != nil {
		combinedErr := errors.Join(providerErr, fmt.Errorf("validate provider failure response: %w", validationErr))
		resultMessage := visibleErrorMessage(combinedErr)
		return turnResult{
			Outcome:      outcomeToRunOutcome(outcome),
			ErrorMessage: mo.EmptyableToOption(resultMessage),
		}, false, combinedErr
	}
	// An aborted response is recorded without finalization, because cancellation starts no new handler work.
	if outcome == model.OutcomeFailed {
		final, finalizeErr := s.finalizer.FinalizeModelResponse(ctx, response)
		if finalizeErr != nil {
			// The failed finalization determines the outcome; the provider failure stays as diagnostic text.
			combinedErr := diagnostic.NewCurrentCause(finalizeErr, providerErr)
			return turnResult{
				Outcome:      agent.RunOutcomeFailed,
				ErrorMessage: mo.Some(visibleErrorMessage(combinedErr)),
			}, false, combinedErr
		}
		response = final
	}
	terminalContext := context.WithoutCancel(ctx)
	if err := s.appendModel(terminalContext, response); err != nil {
		combinedErr := errors.Join(err, providerErr)
		resultMessage := visibleErrorMessage(combinedErr)
		return turnResult{
			Outcome:      outcomeToRunOutcome(outcome),
			ErrorMessage: mo.Some(resultMessage),
		}, false, combinedErr
	}
	messageEnd := newEvent(agent.EventMessageEnd, runID)
	messageEnd.Message = mo.Some(response)
	deliveryErr := s.deliver(terminalContext, messageEnd)
	turn := agent.TurnSummary{Response: response, ToolResults: nil}
	turnEnd := newEvent(agent.EventTurnEnd, runID)
	turnEnd.Turn = mo.Some(turn)
	deliveryErr = errors.Join(deliveryErr, s.deliver(terminalContext, turnEnd))
	combinedErr := errors.Join(providerErr, deliveryErr)
	resultMessage := errorMessage
	if deliveryErr != nil {
		resultMessage = visibleErrorMessage(combinedErr)
	}
	runOutcome := outcomeToRunOutcome(outcome)
	for cause := range errtree.IterateCurrentErrors(deliveryErr) {
		// A classified delivery failure changes the run, while the persisted response keeps its provider outcome.
		//nolint:errorlint // Inspect only current nodes, excluding historical diagnostic descendants.
		if _, classified := cause.(errtree.CodedError); classified {
			runOutcome = agent.RunOutcomeFailed
			break
		}
	}
	return turnResult{
		Outcome:      runOutcome,
		ErrorMessage: mo.EmptyableToOption(resultMessage),
	}, false, combinedErr
}

// providerFailureMessage returns the error text recorded with a response retained after a provider failure.
func providerFailureMessage(response model.Response, outcome model.Outcome, providerErr error) string {
	if outcome == model.OutcomeAborted || errors.Is(providerErr, context.Canceled) ||
		errors.Is(providerErr, context.DeadlineExceeded) {
		return visibleErrorMessage(providerErr)
	}
	// causeMessage keeps the complete logical failure when retained provider text is narrower.
	causeMessage := providerErr.Error()
	if errorMessage := response.ErrorMessage.OrEmpty(); errorMessage != "" &&
		!strings.Contains(causeMessage, errorMessage) {
		return errorMessage + "\n" + causeMessage
	}
	return causeMessage
}

// finalizeRetainedStreamedContent closes only well-formed streamed content kept after provider failure.
func finalizeRetainedStreamedContent(content []model.Content) {
	for position := range content {
		item := &content[position]
		if item.Final || !item.Kind.Streamed() {
			continue
		}
		if item.ValidateShape() == nil {
			item.Final = true
		}
	}
}

// outcomeToRunOutcome maps a model terminal outcome to the run result used for provider failures.
func outcomeToRunOutcome(outcome model.Outcome) agent.RunOutcome {
	if outcome == model.OutcomeAborted {
		return agent.RunOutcomeAborted
	}
	return agent.RunOutcomeFailed
}
