package modelexecution

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/n-r-w/glyph/internal/errtree/diagnostic"

	"github.com/cenkalti/backoff/v7"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/internal/errtree"
)

// overflowRecoveryHandler obtains history and durable-replacement status from the captured conversation owner.
type overflowRecoveryHandler func(context.Context, ProviderRequest, error) (ConversationPreparation, error)

// selectCapturedOverflowRecovery selects recovery from the admitted conversation owner.
func selectCapturedOverflowRecovery(captured mo.Option[CapturedConversation]) overflowRecoveryHandler {
	owner, present := captured.Get()
	if !present {
		return nil
	}
	return owner.RecoverOverflow
}

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
) (providerCompletion, error) {
	policy := s.retryPolicySnapshot()
	var handlers []RetryHandler
	if policy.Enabled && s.retryHandlers != nil {
		handlers = append([]RetryHandler(nil), s.retryHandlers.SnapshotRetryHandlers()...)
	}
	var causes []error
	var failedResponse model.Response
	completedAttempts := int64(0)
	response, retryErr := backoff.Retry(ctx, func() (model.Response, error) {
		completedAttempts++
		terminal := mo.None[model.Response]()
		attemptRequest, cloneErr := request.clone()
		if cloneErr != nil {
			return model.Response{}, backoff.Permanent(errors.Join(append(causes, cloneErr)...))
		}
		wireFailure := &wireFailure{}
		if s.wire != nil {
			attemptRequest.Wire = mo.Some(s.wire.NewWireCallbacks().captureFailures(wireFailure))
		}
		var callbackErr error
		attemptErr := provider.Stream(ctx, attemptRequest, func(event StreamEvent) error {
			if event.Kind == StreamEventDone || event.Kind == StreamEventError {
				response, present := event.Response.Get()
				if !present {
					return terminalMissingError
				}
				terminal = mo.Some(response.Clone())
				return nil
			}
			if callbackErr != nil {
				return callbackErr
			}
			if handle != nil {
				callbackErr = handle(event)
				return callbackErr
			}
			return nil
		})
		failedResponse = model.Response{}
		if terminalResponse, present := terminal.Get(); present {
			failedResponse = terminalResponse
		}
		if acquired := wireFailure.acquired(); acquired != nil {
			if !isPureOwningCancellation(ctx, acquired) {
				causes = append(causes, joinOwningContextCause(ctx, errors.Join(acquired, attemptErr, callbackErr)))
				return failedResponse, backoff.Permanent(logicalFailure(FailureExtensionFailed, causes))
			}
			attemptErr = errors.Join(attemptErr, acquired)
		}
		if callbackErr != nil {
			// The callback's caller has failed; provider descendants remain causes, not recovery permission.
			retained := errors.Join(callbackErr, attemptErr)
			if isPureOwningCancellation(ctx, retained) {
				return failedResponse, backoff.Permanent(diagnostic.NewCurrentCause(
					joinOwningContextCause(ctx, retained), errors.Join(causes...),
				))
			}
			causes = append(causes, joinOwningContextCause(ctx, retained))
			return failedResponse, backoff.Permanent(logicalFailure(FailureInternal, causes))
		}
		if attemptErr == nil {
			response, present := terminal.Get()
			if present {
				return response, nil
			}
			attemptErr = missingTerminalFailure(missingTerminalMessage)
		} else if errors.Is(attemptErr, terminalMissingError) {
			attemptErr = missingTerminalFailure(attemptErr.Error())
		}
		if ctx.Err() != nil {
			if acquiredFailure, ok := errors.AsType[*ProviderFailureError](attemptErr); ok {
				acquiredCauses := append(slices.Clone(causes), attemptErr, context.Cause(ctx))
				return failedResponse, backoff.Permanent(concurrentProviderFailure(
					policy, acquiredFailure, completedAttempts, acquiredCauses,
				))
			}
			if isPureOwningCancellation(ctx, attemptErr) {
				retained := diagnostic.NewCurrentCause(joinOwningContextCause(ctx, attemptErr), errors.Join(causes...))
				return model.Response{}, backoff.Permanent(retained)
			}
			acquiredCauses := append(slices.Clone(causes), attemptErr, context.Cause(ctx))
			return failedResponse, backoff.Permanent(logicalFailure(FailureInternal, acquiredCauses))
		}
		causes = append(causes, attemptErr)
		var providerFailure *ProviderFailureError
		if !errors.As(attemptErr, &providerFailure) {
			return failedResponse, backoff.Permanent(logicalFailure(FailureInternal, causes))
		}
		if providerFailure.Classification == ProviderFailureContextOverflow && !policy.Enabled {
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
					retained := diagnostic.NewCurrentCause(
						joinOwningContextCause(ctx, handlerErr),
						errors.Join(causes...),
					)
					return model.Response{}, backoff.Permanent(retained)
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
		if providerFailure.Classification == ProviderFailureContextOverflow && recoverOverflow == nil {
			return failedResponse, backoff.Permanent(logicalFailure(FailureContextLimit, causes))
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
			request, recoveryErr = prepareOverflowRecovery(
				ctx, request, attemptErr, recoverOverflow, causes,
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
					return model.Response{}, backoff.Permanent(diagnostic.NewCurrentCause(
						joinOwningContextCause(ctx, progressErr), errors.Join(causes...),
					))
				}
				causes = append(causes, fmt.Errorf("deliver retry progress: %w", progressErr))
				return model.Response{}, backoff.Permanent(logicalFailure(FailureInternal, causes))
			}
		}
		return model.Response{}, backoff.RetryAfter(decision.Delay, attemptErr)
	}, backoff.WithBackOff(backoff.NewConstantBackOff(0)), backoff.WithMaxTries(0), backoff.WithMaxElapsedTime(0))
	completion := providerCompletion{response: response, input: request, causes: errors.Join(causes...)}
	if retryErr == nil {
		return completion, nil
	}
	details := backoff.AsRetryError(retryErr)
	if details == nil {
		return completion, retryErr
	}
	if ctx.Err() != nil && errors.Is(details.Cause, context.Cause(ctx)) {
		return completion, diagnostic.NewCurrentCause(context.Cause(ctx), errors.Join(causes...))
	}
	return completion, details.LastErr
}

// prepareOverflowRecovery settles captured recovery within an admitted provider retry.
func prepareOverflowRecovery(
	ctx context.Context,
	request ProviderRequest,
	source error,
	recoverOverflow overflowRecoveryHandler,
	causes []error,
) (ProviderRequest, error) {
	recovered, err := recoverOverflow(ctx, request.cloneBinding(), source)
	if err != nil {
		if !recovered.Committed && isPureOwningCancellation(ctx, err) {
			return request, diagnostic.NewCurrentCause(joinOwningContextCause(ctx, err), errors.Join(causes...))
		}
		current, _ := errors.AsType[*LogicalFailureError](contextPreparationLogicalFailure(err))
		category := current.Category
		causes = append(causes, fmt.Errorf("recover context overflow: %w", joinOwningContextCause(ctx, err)))
		return request, logicalFailure(category, causes)
	}
	if !recovered.Committed {
		return request, logicalFailure(FailureContextLimit, causes)
	}
	request.History = recovered.History
	return request, nil
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

// isPureOwningCancellation reports whether the current causes indicate owning-context cancellation.
func isPureOwningCancellation(ctx context.Context, err error) bool {
	if ctx.Err() == nil {
		return false
	}
	owningCause := context.Cause(ctx)
	return errtree.IsCurrentCancellation(err, func(cause error) bool {
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
