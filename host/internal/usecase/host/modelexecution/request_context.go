package modelexecution

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/internal/errtree/diagnostic"
)

// RequestContext contains the request-local instructions and history of one prepared model request.
type RequestContext struct {
	// Instructions contains the system instructions.
	Instructions string
	// History contains provider-neutral history in request order.
	History []agent.HistoryEntry
}

// ComposedContext contains the final request context of one prepared-context revision.
type ComposedContext struct {
	// Context contains the final instructions and history.
	Context RequestContext
	// HistoryReplaced reports whether a history handler replaced the history in this composition.
	HistoryReplaced bool
}

// ContextComposer applies captured instruction and history handlers to one prepared-context revision.
type ContextComposer func(context.Context, RequestContext) (ComposedContext, error)

//go:generate go tool mockgen -source=request_context.go -destination=request_context_mock.go -package=modelexecution

// ContextMiddleware captures ordered instruction and history handlers for one logical model execution.
type ContextMiddleware interface {
	// NewContextComposer captures the applicable handlers once for every prepared-context revision.
	NewContextComposer() ContextComposer
}

// BindContextMiddleware connects the Host instruction and history middleware owner before model execution starts.
func (s *Service) BindContextMiddleware(owner ContextMiddleware) error {
	if owner == nil {
		return errors.New("context middleware owner is required")
	}
	if s.contextMiddleware != nil {
		return errors.New("context middleware owner is already bound")
	}
	s.contextMiddleware = owner
	return nil
}

// newContextComposer captures the bound handlers for one logical execution.
func (s *Service) newContextComposer() mo.Option[ContextComposer] {
	if s.contextMiddleware == nil {
		return mo.None[ContextComposer]()
	}
	return mo.Some(s.contextMiddleware.NewContextComposer())
}

// preparedRequest pairs a prepared request with the revision that raw provider attempts receive.
type preparedRequest struct {
	// base contains the prepared instructions and history before middleware transformation.
	base ProviderRequest
	// effective contains the transformed instructions and history sent to raw provider attempts.
	effective ProviderRequest
}

// composeRequest applies captured handlers to one prepared request and validates the final history.
func composeRequest(
	ctx context.Context,
	composer mo.Option[ContextComposer],
	base ProviderRequest,
	causes []error,
) (preparedRequest, error) {
	original := RequestContext{Instructions: base.Instructions, History: base.History}
	composed := ComposedContext{Context: original, HistoryReplaced: false}
	if compose, present := composer.Get(); present {
		var err error
		composed, err = compose(ctx, original)
		if err != nil {
			if isPureOwningCancellation(ctx, err) {
				return preparedRequest{}, diagnostic.NewCurrentCause(
					joinOwningContextCause(ctx, err), errors.Join(causes...),
				)
			}
			causes = append(
				slices.Clone(causes),
				fmt.Errorf("compose model request context: %w", joinOwningContextCause(ctx, err)),
			)
			return preparedRequest{}, logicalFailure(FailureExtensionFailed, causes)
		}
	}
	if err := validateInputModalities(base.Model, composed.Context.History); err != nil {
		if !composed.HistoryReplaced {
			// Unreplaced history keeps the uncategorized input-validation meaning of its caller.
			return preparedRequest{}, errors.Join(append(slices.Clone(causes), err)...)
		}
		causes = append(slices.Clone(causes), fmt.Errorf("validate composed model request context: %w", err))
		return preparedRequest{}, logicalFailure(FailureExtensionFailed, causes)
	}
	effective := base.cloneBinding()
	effective.Instructions = composed.Context.Instructions
	effective.History = composed.Context.History
	return preparedRequest{base: base, effective: effective}, nil
}
