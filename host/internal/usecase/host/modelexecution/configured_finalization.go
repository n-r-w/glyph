package modelexecution

import (
	"context"
	"errors"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

//go:generate go tool mockgen -source=configured_finalization.go -destination=configured_finalization_mock.go -package=modelexecution

// ResponseFinalizer returns the final value of a configured terminal model response before it reaches its caller.
type ResponseFinalizer interface {
	// FinalizeConfiguredResponse returns the final response with the outcome, provider identity, and tool calls of
	// the given response.
	FinalizeConfiguredResponse(context.Context, model.Response) (model.Response, error)
}

// BindResponseFinalizer connects the Host finalized-message owner before configured requests start.
func (s *Service) BindResponseFinalizer(owner ResponseFinalizer) error {
	if owner == nil {
		return errors.New("response finalizer is required")
	}
	if s.responseFinalizer != nil {
		return errors.New("response finalizer is already bound")
	}
	s.responseFinalizer = owner
	return nil
}

// finalizeConfiguredResponse returns the final configured response; a finalization failure is an extension failure
// unless its current cause is only the cancellation of the owning context.
func (s *Service) finalizeConfiguredResponse(ctx context.Context, response model.Response) (model.Response, error) {
	final, err := s.responseFinalizer.FinalizeConfiguredResponse(ctx, response)
	if err == nil {
		return final, nil
	}
	if isPureOwningCancellation(ctx, err) {
		return model.Response{}, joinOwningContextCause(ctx, err)
	}
	return model.Response{}, logicalFailure(
		FailureExtensionFailed, []error{fmt.Errorf("finalize configured model response: %w", err)},
	)
}
