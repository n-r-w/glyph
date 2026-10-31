package runtime

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/usecase/host/middleware"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// HandleFinalizedMessage maps one finalized-message transformer and restores private replay retained by its
// replacement.
func (r *Runtime) HandleFinalizedMessage(
	ctx context.Context,
	handlerID string,
	input middleware.Transformation[agent.HistoryEntry],
) (middleware.Transformed[agent.HistoryEntry], error) {
	projection := newReplayProjection()
	request, err := mapFinalizedMessageRequest(handlerID, input, projection)
	if err != nil {
		return middleware.Transformed[agent.HistoryEntry]{}, err
	}
	var action middleware.Transformed[agent.HistoryEntry]
	err = r.handleMiddleware(ctx, request, func(response *extensionpb.HandleResponse) error {
		var decodeErr error
		action, decodeErr = mapFinalizedMessageAction(response, projection)
		return decodeErr
	})
	return action, err
}

// mapFinalizedMessageRequest projects detached owner messages into their public payload with replay references.
func mapFinalizedMessageRequest(
	handlerID string,
	input middleware.Transformation[agent.HistoryEntry],
	projection replayProjection,
) (*extensionpb.HandleRequest, error) {
	original, err := projection.projectEntry(&input.Original)
	if err != nil {
		return nil, err
	}
	current, err := projection.projectEntry(&input.Current)
	if err != nil {
		return nil, err
	}
	request := new(extensionpb.HandleRequest)
	request.SetHandlerId(handlerID)
	request.SetContext(mapContext(input.Context))
	request.SetFinalizedMessage(
		extensionpb.FinalizedMessageInvocation_builder{Original: original, Current: current}.Build(),
	)
	return request, nil
}

// mapFinalizedMessageAction restores a replacement and leaves a missing or false action invalid for the owner.
func mapFinalizedMessageAction(
	response *extensionpb.HandleResponse,
	projection replayProjection,
) (middleware.Transformed[agent.HistoryEntry], error) {
	value := response.GetFinalizedMessage()
	if value.GetPreserve() {
		return middleware.Transformed[agent.HistoryEntry]{
			Kind:        middleware.Preserve,
			Replacement: agent.HistoryEntry{},
		}, nil
	}
	if !value.HasReplace() {
		return middleware.Transformed[agent.HistoryEntry]{}, nil
	}
	entry, err := projection.restoreEntry(value.GetReplace(), make(map[string]bool))
	if err != nil {
		return middleware.Transformed[agent.HistoryEntry]{}, err
	}
	return middleware.Transformed[agent.HistoryEntry]{Kind: middleware.Replace, Replacement: entry}, nil
}
