package extensioncontext

import (
	"context"
	"errors"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/internal/errtree"
)

// PolicyOperationError records the source of a compaction integration failure.
type PolicyOperationError struct {
	// Category identifies the existing Glyph category for the source.
	Category modelexecution.FailureCategory
	// Cause retains the original source error.
	Cause error
}

// Error exposes the complete source text.
func (e *PolicyOperationError) Error() string { return e.Cause.Error() }

// Unwrap exposes the source identity.
func (e *PolicyOperationError) Unwrap() error { return e.Cause }

// NewPolicyOperationError annotates an independent source at its integration boundary.
func NewPolicyOperationError(ctx context.Context, category modelexecution.FailureCategory, cause error) error {
	if cause == nil || isPolicyOwningCancellation(ctx, cause) {
		return cause
	}
	if existing, present := selectPolicyOperationCategory(cause).Get(); present {
		category = existing
	}
	return &PolicyOperationError{Category: category, Cause: cause}
}

// selectPolicyOperationCategory selects a failure category from current causes, giving persistence failures precedence.
func selectPolicyOperationCategory(cause error) mo.Option[modelexecution.FailureCategory] {
	category := mo.None[modelexecution.FailureCategory]()
	for node := range errtree.IterateCurrentErrors(cause) {
		//nolint:errorlint // Direct node inspection excludes diagnostic history from source selection.
		if node == session.ErrPersistenceUnavailable || node == agent.ErrPersistenceUnavailable {
			return mo.Some(modelexecution.FailurePersistenceUnavailable)
		}
		//nolint:errorlint // The iterator supplies current nodes without recursive historical searches.
		if source, present := node.(*PolicyOperationError); present {
			if source.Category == modelexecution.FailurePersistenceUnavailable {
				return mo.Some(source.Category)
			}
			if category.IsNone() {
				category = mo.Some(source.Category)
			}
		}
		if source, present := node.(interface{ ContextCode() string }); present &&
			source.ContextCode() == string(modelexecution.FailurePersistenceUnavailable) {
			return mo.Some(modelexecution.FailurePersistenceUnavailable)
		}
		if source, present := node.(interface{ CompactionFailureCode() string }); present &&
			source.CompactionFailureCode() == string(modelexecution.FailurePersistenceUnavailable) {
			return mo.Some(modelexecution.FailurePersistenceUnavailable)
		}
		if source, present := node.(interface{ FailureCode() string }); present &&
			source.FailureCode() == string(modelexecution.FailurePersistenceUnavailable) {
			return mo.Some(modelexecution.FailurePersistenceUnavailable)
		}
		if source, present := node.(interface{ Code() string }); present &&
			source.Code() == string(modelexecution.FailurePersistenceUnavailable) {
			return mo.Some(modelexecution.FailurePersistenceUnavailable)
		}
	}
	return category
}

// selectPolicyTerminalSource retains authoritative operation sources before policy and completion reports.
func selectPolicyTerminalSource(
	ctx context.Context,
	operation, policy, completion error,
) mo.Option[modelexecution.FailureCategory] {
	if source := selectPolicyOperationCategory(operation); source.IsSome() {
		return source
	}
	if operation != nil && !isPolicyOwningCancellation(ctx, operation) {
		return mo.Some(modelexecution.FailureCompactionFailed)
	}
	if source := selectPolicyOperationCategory(policy); source.IsSome() {
		return source
	}
	return selectPolicyOperationCategory(completion)
}

// isPolicyOwningCancellation checks only the active failure branch for caller cancellation.
func isPolicyOwningCancellation(ctx context.Context, cause error) bool {
	return ctx.Err() != nil && errtree.IsCurrentCancellation(cause, func(node error) bool {
		return errors.Is(node, ctx.Err()) || errors.Is(node, context.Cause(ctx))
	})
}

// overflowPreparationError exposes the recovery consumer's existing failure contract.
type overflowPreparationError struct {
	// category identifies the terminal recovery source.
	category modelexecution.FailureCategory
	// cause contains every retained recovery cause.
	cause error
}

var _ modelexecution.ContextPreparationFailure = (*overflowPreparationError)(nil)

// Error exposes every recovery diagnostic.
func (e *overflowPreparationError) Error() string { return e.cause.Error() }

// Unwrap retains original local recovery identities.
func (e *overflowPreparationError) Unwrap() error { return e.cause }

// CompactionFailureCode supplies the captured recovery category.
func (e *overflowPreparationError) CompactionFailureCode() string { return string(e.category) }
