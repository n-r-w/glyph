package runtime

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/usecase/host/middleware"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// HandleSystemInstructions maps one instruction transformer over the existing accepted operation stream.
func (r *Runtime) HandleSystemInstructions(
	ctx context.Context,
	handlerID string,
	input middleware.Transformation[string],
) (middleware.Transformed[string], error) {
	var action middleware.Transformed[string]
	err := r.handleMiddleware(
		ctx,
		mapSystemInstructionsRequest(handlerID, input),
		func(response *extensionpb.HandleResponse) error {
			action = mapSystemInstructionsAction(response)
			return nil
		},
	)
	return action, err
}

// HandleModelContext maps one history transformer and restores private replay retained by its replacement.
func (r *Runtime) HandleModelContext(
	ctx context.Context,
	handlerID string,
	input middleware.Transformation[[]agent.HistoryEntry],
) (middleware.Transformed[[]agent.HistoryEntry], error) {
	projection := newReplayProjection()
	request, err := mapModelContextRequest(handlerID, input, projection)
	if err != nil {
		return middleware.Transformed[[]agent.HistoryEntry]{}, err
	}
	var action middleware.Transformed[[]agent.HistoryEntry]
	err = r.handleMiddleware(ctx, request, func(response *extensionpb.HandleResponse) error {
		var decodeErr error
		action, decodeErr = mapModelContextAction(response, projection)
		return decodeErr
	})
	return action, err
}

// mapSystemInstructionsRequest converts detached owner instructions into their public payload.
func mapSystemInstructionsRequest(
	handlerID string,
	input middleware.Transformation[string],
) *extensionpb.HandleRequest {
	request := new(extensionpb.HandleRequest)
	request.SetHandlerId(handlerID)
	request.SetContext(mapContext(input.Context))
	request.SetSystemInstructions(extensionpb.SystemInstructionsInvocation_builder{
		Original: new(input.Original), Current: new(input.Current),
	}.Build())
	return request
}

// mapSystemInstructionsAction returns a zero-kind result for a missing or false action;
// the owner rejects it as an invalid action.
func mapSystemInstructionsAction(response *extensionpb.HandleResponse) middleware.Transformed[string] {
	value := response.GetSystemInstructions()
	if value.GetPreserve() {
		return middleware.Transformed[string]{Kind: middleware.Preserve, Replacement: ""}
	}
	if value.HasReplace() {
		return middleware.Transformed[string]{Kind: middleware.Replace, Replacement: value.GetReplace()}
	}
	return middleware.Transformed[string]{}
}

// mapModelContextRequest projects detached owner history into its public payload with replay references.
func mapModelContextRequest(
	handlerID string,
	input middleware.Transformation[[]agent.HistoryEntry],
	projection replayProjection,
) (*extensionpb.HandleRequest, error) {
	original, err := projection.project(input.Original)
	if err != nil {
		return nil, err
	}
	current, err := projection.project(input.Current)
	if err != nil {
		return nil, err
	}
	request := new(extensionpb.HandleRequest)
	request.SetHandlerId(handlerID)
	request.SetContext(mapContext(input.Context))
	request.SetModelContext(extensionpb.ModelContextInvocation_builder{Original: original, Current: current}.Build())
	return request, nil
}

// mapModelContextAction restores a replacement and leaves a missing or false action invalid for the owner.
func mapModelContextAction(
	response *extensionpb.HandleResponse,
	projection replayProjection,
) (middleware.Transformed[[]agent.HistoryEntry], error) {
	value := response.GetModelContext()
	if value.GetPreserve() {
		return middleware.Transformed[[]agent.HistoryEntry]{Kind: middleware.Preserve, Replacement: nil}, nil
	}
	if !value.HasReplace() {
		return middleware.Transformed[[]agent.HistoryEntry]{}, nil
	}
	history, err := projection.restore(value.GetReplace())
	if err != nil {
		return middleware.Transformed[[]agent.HistoryEntry]{}, err
	}
	return middleware.Transformed[[]agent.HistoryEntry]{Kind: middleware.Replace, Replacement: history}, nil
}
