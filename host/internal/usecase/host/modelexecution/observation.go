package modelexecution

import (
	"context"
	"errors"

	"github.com/samber/mo"

	controllerextension "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// captureConversation captures the admitted policy owner.
func (s *Service) captureConversation(ctx context.Context) (mo.Option[ConversationObservation], error) {
	if s.observations == nil {
		return mo.None[ConversationObservation](), errors.New("conversation observation dependency is unavailable")
	}
	observation, err := s.observations.CaptureConversationObservation()
	if err != nil {
		return mo.None[ConversationObservation](), observationFailure(ctx, err, nil)
	}
	return observation, nil
}

// providerCompletion contains the final attempt input, response, and earlier attempt errors.
type providerCompletion struct {
	// response contains the terminal provider result.
	response model.Response
	// input contains the final attempt input.
	input ProviderRequest
	// causes contains errors from earlier failed attempts.
	causes error
}

// observe delivers the successful attempt to its admitted policy owner.
func (completion providerCompletion) observe(
	ctx context.Context,
	observation mo.Option[ConversationObservation],
) error {
	outcome, present := completion.response.Outcome.Get()
	if !present || (outcome != model.OutcomeStop && outcome != model.OutcomeToolUse && outcome != model.OutcomeLength) {
		return nil
	}
	observe, captured := observation.Get()
	if !captured {
		return nil
	}
	if err := observe(ctx, completion.input, completion.response); err != nil {
		return observationFailure(ctx, err, completion.causes)
	}
	return nil
}

// IsPureObservationCancellation identifies cancellation owned by the observation lifetime.
func IsPureObservationCancellation(ctx context.Context, cause error) bool {
	_, modelFailure := errors.AsType[*LogicalFailureError](cause)
	_, contextFailure := errors.AsType[controllerextension.ContextFailure](cause)
	return !modelFailure && !contextFailure && isPureOwningCancellation(ctx, cause)
}

// observationFailure classifies errors from conversation observation.
func observationFailure(ctx context.Context, cause, attempts error) error {
	current := joinOwningContextCause(ctx, cause)
	if attempts != nil {
		current = &currentCauseError{current: current, recovered: attempts}
	}
	if IsPureObservationCancellation(ctx, cause) {
		if attempts == nil {
			return context.Cause(ctx)
		}
		return current
	}
	return &LogicalFailureError{Category: FailureExtensionFailed, Cause: current}
}
