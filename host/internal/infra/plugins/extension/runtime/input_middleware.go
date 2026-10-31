package runtime

import (
	"context"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/middleware"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// HandleUserInput maps one user-input handler over the existing accepted operation stream.
func (r *Runtime) HandleUserInput(
	ctx context.Context,
	handlerID string,
	input middleware.Transformation[model.Message],
) (middleware.Transformed[model.Message], error) {
	var action middleware.Transformed[model.Message]
	err := r.handleMiddleware(
		ctx,
		mapUserInputRequest(handlerID, input),
		func(response *extensionpb.HandleResponse) error {
			var decodeErr error
			action, decodeErr = mapUserInputAction(response)
			return decodeErr
		},
	)
	return action, err
}

// mapUserInputRequest converts detached owner input into its public payload.
func mapUserInputRequest(handlerID string, input middleware.Transformation[model.Message]) *extensionpb.HandleRequest {
	request := new(extensionpb.HandleRequest)
	request.SetHandlerId(handlerID)
	request.SetContext(mapContext(input.Context))
	request.SetUserInput(extensionpb.UserInputInvocation_builder{
		Original: mapUserMessage(input.Original), Current: mapUserMessage(input.Current),
	}.Build())
	return request
}

// mapUserInputAction decodes a replacement and leaves a missing or false action invalid for the owner.
func mapUserInputAction(response *extensionpb.HandleResponse) (middleware.Transformed[model.Message], error) {
	value := response.GetUserInput()
	switch {
	case value.GetPreserve():
		return middleware.Transformed[model.Message]{Kind: middleware.Preserve, Replacement: model.Message{}}, nil
	case value.GetHandle():
		return middleware.Transformed[model.Message]{Kind: middleware.Handle, Replacement: model.Message{}}, nil
	case value.HasReplace():
		message, err := extensioncontroller.DecodeSessionTreeUser(value.GetReplace())
		if err != nil {
			return middleware.Transformed[model.Message]{}, err
		}
		return middleware.Transformed[model.Message]{Kind: middleware.Replace, Replacement: message}, nil
	default:
		return middleware.Transformed[model.Message]{}, nil
	}
}
