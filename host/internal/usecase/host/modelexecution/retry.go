package modelexecution

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/cenkalti/backoff/v7"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/errtree"
)

// overflowRecoveryHandler rebuilds one rejected active-conversation request after compaction.
type overflowRecoveryHandler func(context.Context, ProviderRequest) (ProviderRequest, error)

// execute runs one provider request through the captured retry policy and handler snapshot.
//
//nolint:gocognit,gocyclo // The retry operation keeps terminal classification and handler composition together.
func (s *Service) execute(
	ctx context.Context,
	provider ProviderAttempt,
	request ProviderRequest,
	handle StreamHandler,
	progress retryProgressHandler,
	terminalMissingError error,
	missingTerminalMessage string,
	recoverOverflow overflowRecoveryHandler,
) (model.Response, error) {
	policy := s.retryPolicySnapshot()
	var handlers []RetryHandler
	if policy.Enabled && s.retryHandlers != nil {
		handlers = append([]RetryHandler(nil), s.retryHandlers.SnapshotRetryHandlers()...)
	}
	var causes []error
	var failedResponse model.Response
	completedAttempts := int64(0)
	overflowRecovered := false
	response, retryErr := backoff.Retry(ctx, func() (model.Response, error) {
		completedAttempts++
		terminal := mo.None[model.Response]()
		attemptRequest, cloneErr := request.clone()
		if cloneErr != nil {
			return model.Response{}, backoff.Permanent(errors.Join(append(causes, cloneErr)...))
		}
		attemptErr := provider.Stream(ctx, attemptRequest, func(event StreamEvent) error {
			if event.Kind == StreamEventDone || event.Kind == StreamEventError {
				response, present := event.Response.Get()
				if !present {
					return terminalMissingError
				}
				terminal = mo.Some(response.Clone())
				return nil
			}
			if handle != nil {
				return handle(event)
			}
			return nil
		})
		if attemptErr == nil {
			response, present := terminal.Get()
			if present {
				return response, nil
			}
			attemptErr = missingTerminalFailure(missingTerminalMessage)
		} else if errors.Is(attemptErr, terminalMissingError) {
			attemptErr = missingTerminalFailure(attemptErr.Error())
		}
		failedResponse = model.Response{}
		if terminalResponse, present := terminal.Get(); present {
			failedResponse = terminalResponse
		}
		if ctx.Err() != nil {
			if acquiredFailure, ok := errors.AsType[*ProviderFailureError](attemptErr); ok {
				acquiredCauses := append(slices.Clone(causes), attemptErr, context.Cause(ctx))
				return failedResponse, backoff.Permanent(concurrentProviderFailure(
					policy, acquiredFailure, completedAttempts, acquiredCauses,
				))
			}
			if isPureOwningCancellation(ctx, attemptErr) {
				return model.Response{}, backoff.Permanent(context.Cause(ctx))
			}
			acquiredCauses := append(slices.Clone(causes), attemptErr, context.Cause(ctx))
			return failedResponse, backoff.Permanent(logicalFailure(FailureInternal, acquiredCauses))
		}
		causes = append(causes, attemptErr)
		var providerFailure *ProviderFailureError
		if !errors.As(attemptErr, &providerFailure) {
			return failedResponse, backoff.Permanent(logicalFailure(FailureInternal, causes))
		}
		if providerFailure.Classification == ProviderFailureContextOverflow &&
			(!policy.Enabled || recoverOverflow == nil) {
			return failedResponse, backoff.Permanent(logicalFailure(FailureContextLimit, causes))
		}
		if !policy.Enabled {
			return failedResponse, backoff.Permanent(logicalFailure(FailureModelFailed, causes))
		}
		providerDelay, delayPresent := providerFailure.RetryDelay.Get()
		decision := retryDecision(
			policy,
			providerFailure.Classification,
			completedAttempts,
			providerFailure.RetryDelay,
			recoverOverflow != nil,
		)
		original := decision
		for _, handler := range handlers {
			action, handlerErr := s.retryHandlers.HandleRetry(ctx, handler, RetryInvocation{
				SourceError: attemptErr.Error(), Classification: providerFailure.Classification,
				Original: original, Current: decision, CompletedAttempts: completedAttempts,
				ProviderDelay: providerFailure.RetryDelay,
			})
			if handlerErr != nil {
				if isPureOwningCancellation(ctx, handlerErr) {
					return model.Response{}, backoff.Permanent(context.Cause(ctx))
				}
				causes = append(causes, fmt.Errorf("retry handler %q: %w", handler.HandlerID, handlerErr))
				return failedResponse, backoff.Permanent(logicalFailure(FailureExtensionFailed, causes))
			}
			var actionErr error
			decision, actionErr = applyRetryAction(action, decision)
			if actionErr != nil {
				causes = append(causes, fmt.Errorf("retry handler %q: %w", handler.HandlerID, actionErr))
				return failedResponse, backoff.Permanent(logicalFailure(FailureExtensionFailed, causes))
			}
			if action.Kind == RetryActionCancel {
				return failedResponse, backoff.Permanent(logicalFailure(FailureRetryCanceled, causes))
			}
			if validationErr := decision.validate(
				completedAttempts, providerFailure.RetryDelay,
			); validationErr != nil {
				causes = append(causes, fmt.Errorf("retry handler %q: %w", handler.HandlerID, validationErr))
				return failedResponse, backoff.Permanent(logicalFailure(FailureExtensionFailed, causes))
			}
		}
		if validationErr := decision.validate(
			completedAttempts, providerFailure.RetryDelay,
		); validationErr != nil {
			causes = append(causes, validationErr)
			return failedResponse, backoff.Permanent(logicalFailure(FailureExtensionFailed, causes))
		}
		if delayPresent && providerDelay > policy.MaxProviderDelay {
			causes = append(causes, fmt.Errorf(
				"provider requested retry delay %s exceeds configured maximum %s",
				providerDelay, policy.MaxProviderDelay,
			))
			return failedResponse, backoff.Permanent(logicalFailure(FailureRetryDelayExceeded, causes))
		}
		if !decision.Retry {
			return failedResponse, backoff.Permanent(
				terminalProviderFailure(decision, providerFailure.Classification, causes),
			)
		}
		if providerFailure.Classification == ProviderFailureContextOverflow {
			var recoveryErr error
			request, overflowRecovered, recoveryErr = prepareOverflowRecovery(
				ctx, request, overflowRecovered, recoverOverflow, causes,
			)
			if recoveryErr != nil {
				return failedResponse, backoff.Permanent(recoveryErr)
			}
		}
		if progress != nil {
			if progressErr := progress(RetryProgress{
				CompletedAttempts: completedAttempts, AttemptLimit: decision.AttemptLimit,
				Delay: decision.Delay, Error: attemptErr.Error(),
			}); progressErr != nil {
				if isPureOwningCancellation(ctx, progressErr) {
					return model.Response{}, backoff.Permanent(context.Cause(ctx))
				}
				causes = append(causes, fmt.Errorf("deliver retry progress: %w", progressErr))
				return model.Response{}, backoff.Permanent(logicalFailure(FailureInternal, causes))
			}
		}
		return model.Response{}, backoff.RetryAfter(decision.Delay, attemptErr)
	}, backoff.WithBackOff(backoff.NewConstantBackOff(0)), backoff.WithMaxTries(0), backoff.WithMaxElapsedTime(0))
	if retryErr == nil {
		return response, nil
	}
	details := backoff.AsRetryError(retryErr)
	if details == nil {
		return response, retryErr
	}
	if ctx.Err() != nil && errors.Is(details.Cause, context.Cause(ctx)) {
		return response, context.Cause(ctx)
	}
	return response, details.LastErr
}

// prepareOverflowRecovery applies the single allowed overflow recovery without masking typed failures.
func prepareOverflowRecovery(
	ctx context.Context,
	request ProviderRequest,
	alreadyRecovered bool,
	recoverOverflow overflowRecoveryHandler,
	causes []error,
) (ProviderRequest, bool, error) {
	if alreadyRecovered {
		return ProviderRequest{}, true, logicalFailure(FailureContextLimit, causes)
	}
	recovered, err := recoverOverflow(ctx, request)
	if err != nil {
		if isPureOwningCancellation(ctx, err) {
			return ProviderRequest{}, false, context.Cause(ctx)
		}
		causes = append(causes, fmt.Errorf("recover context overflow: %w", joinOwningContextCause(ctx, err)))
		return ProviderRequest{}, false, contextPreparationLogicalFailure(errors.Join(causes...))
	}
	if reflect.DeepEqual(recovered, request) {
		causes = append(causes, errors.New("overflow compaction returned an unchanged request"))
		return ProviderRequest{}, false, logicalFailure(FailureContextLimit, causes)
	}
	return recovered, true, nil
}

// concurrentProviderFailure classifies a provider failure acquired with caller cancellation.
func concurrentProviderFailure(
	policy RetryPolicy,
	failure *ProviderFailureError,
	completedAttempts int64,
	causes []error,
) error {
	switch failure.Classification {
	case ProviderFailureNonRetryable:
		return logicalFailure(FailureModelFailed, causes)
	case ProviderFailureContextOverflow:
		return logicalFailure(FailureContextLimit, causes)
	case ProviderFailureTransient:
		if !policy.Enabled {
			return logicalFailure(FailureModelFailed, causes)
		}
		if delay, present := failure.RetryDelay.Get(); present && delay > policy.MaxProviderDelay {
			causes = append(causes, fmt.Errorf(
				"provider requested retry delay %s exceeds configured maximum %s", delay, policy.MaxProviderDelay,
			))
			return logicalFailure(FailureRetryDelayExceeded, causes)
		}
		decision := retryDecision(policy, failure.Classification, completedAttempts, failure.RetryDelay, false)
		if !decision.Retry {
			return logicalFailure(FailureRetryExhausted, causes)
		}
	}
	return logicalFailure(FailureInternal, causes)
}

// retryDecision creates the built-in decision for one completed attempt.
func retryDecision(
	policy RetryPolicy,
	classification ProviderFailureClassification,
	completedAttempts int64,
	providerDelay mo.Option[time.Duration],
	allowOverflow bool,
) RetryDecision {
	limit := policy.MaxRetries + 1
	retryable := classification == ProviderFailureTransient ||
		(classification == ProviderFailureContextOverflow && allowOverflow)
	delay := time.Duration(0)
	if retryable && completedAttempts < limit {
		index := min(int(completedAttempts-1), len(policy.Delays)-1)
		delay = policy.Delays[index]
	}
	if requested, present := providerDelay.Get(); present {
		delay = max(delay, requested)
	}
	return RetryDecision{
		Retryable: retryable, Retry: retryable && completedAttempts < limit,
		Delay: delay, AttemptLimit: limit,
	}
}

// applyRetryAction validates action shape and composes its decision.
func applyRetryAction(action RetryAction, current RetryDecision) (RetryDecision, error) {
	switch action.Kind {
	case RetryActionPreserve:
		if action.Decision.IsSome() {
			return RetryDecision{}, errors.New("preserve retry action must not contain a decision")
		}
		return current, nil
	case RetryActionReplace:
		decision, present := action.Decision.Get()
		if !present {
			return RetryDecision{}, errors.New("replace retry action requires a decision")
		}
		return decision, nil
	case RetryActionCancel:
		if action.Decision.IsSome() {
			return RetryDecision{}, errors.New("cancel retry action must not contain a decision")
		}
		return current, nil
	default:
		return RetryDecision{}, errors.New("retry handler returned an unknown action")
	}
}

// validate rejects impossible replacement attempts.
func (decision RetryDecision) validate(
	completedAttempts int64,
	providerDelay mo.Option[time.Duration],
) error {
	if decision.Delay < 0 || decision.AttemptLimit < 1 {
		return errors.New("retry decision contains a negative delay or invalid attempt limit")
	}
	if decision.Retry && (!decision.Retryable || decision.AttemptLimit <= completedAttempts) {
		return errors.New("retry decision requests an attempt that is not permitted")
	}
	if requested, present := providerDelay.Get(); present && decision.Delay < requested {
		return fmt.Errorf("retry decision delay %s is below provider retry delay %s", decision.Delay, requested)
	}
	return nil
}

// terminalProviderFailure maps one source classification after retry coordination ends.
func terminalProviderFailure(
	decision RetryDecision,
	classification ProviderFailureClassification,
	causes []error,
) error {
	if classification == ProviderFailureContextOverflow {
		return logicalFailure(FailureContextLimit, causes)
	}
	if classification == ProviderFailureTransient && decision.Retryable {
		return logicalFailure(FailureRetryExhausted, causes)
	}
	return logicalFailure(FailureModelFailed, causes)
}

// isPureOwningCancellation reports whether every error leaf belongs to the canceled owning context.
func isPureOwningCancellation(ctx context.Context, err error) bool {
	if ctx.Err() == nil {
		return false
	}
	if _, typed := errors.AsType[ContextPreparationFailure](err); typed {
		return false
	}
	owningCause := context.Cause(ctx)
	return errtree.AllLeavesMatch(err, func(cause error) bool {
		return errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) ||
			errors.Is(cause, owningCause)
	})
}

// joinOwningContextCause retains concurrent owner cancellation beside an independent failure.
func joinOwningContextCause(ctx context.Context, err error) error {
	owningCause := context.Cause(ctx)
	if owningCause == nil || errors.Is(err, owningCause) {
		return err
	}
	return errors.Join(err, owningCause)
}

// contextPreparationLogicalFailure maps the compaction consumer contract into logical model execution.
func contextPreparationLogicalFailure(cause error) error {
	category := FailureCompactionFailed
	if failure, present := errors.AsType[ContextPreparationFailure](cause); present {
		switch failure.CompactionFailureCode() {
		case string(FailureExtensionFailed):
			category = FailureExtensionFailed
		case string(FailurePersistenceUnavailable):
			category = FailurePersistenceUnavailable
		case string(FailureInternal):
			category = FailureInternal
		case string(FailureCompactionFailed):
			category = FailureCompactionFailed
		default:
			category = FailureCompactionFailed
		}
	}
	return logicalFailure(category, []error{cause})
}

// logicalFailure creates one categorized failure from detached contributing causes.
func logicalFailure(category FailureCategory, causes []error) error {
	return &LogicalFailureError{Category: category, Cause: errors.Join(causes...)}
}
