package runtime

import (
	"context"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// runCompactionParticipant acquires accepted handler data for participant decoding.
func (r *Runtime) runCompactionParticipant(
	ctx context.Context,
	handlerID string,
	request *extensionpb.HandleRequest,
) (*extensionpb.HandleResponse, error) {
	completion, err := r.runCompactionHandler(ctx, handlerID, request)
	if completion.Response == nil || completion.Response.GetError() != nil {
		return nil, err
	}
	return completion.Response, err
}
