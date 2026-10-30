package middleware

import (
	"context"
	"fmt"
	"slices"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// composeHeaders applies each validated replacement to detached public headers.
func (s *Service) composeHeaders(
	ctx context.Context,
	handlers []Handler,
	original modelexecution.WireHeaders,
	validate func(modelexecution.WireHeaders) error,
) (modelexecution.WireHeaders, error) {
	original = original.Clone()
	current := original.Clone()
	for _, handler := range handlers {
		if handler.Kind != Headers {
			continue
		}
		action, err := s.invoke(ctx, handler, Invocation{
			Context:         extension.Context{},
			Kind:            Headers,
			OriginalHeaders: original.Clone(),
			CurrentHeaders:  current.Clone(),
			OriginalBody:    nil,
			CurrentBody:     nil,
			Response:        modelexecution.WireResponse{},
		})
		if err != nil {
			return nil, err
		}
		if action.Kind == Preserve {
			continue
		}
		if action.Kind != Replace {
			return nil, fmt.Errorf(
				"middleware handler %q returned invalid header action %d",
				handler.HandlerID,
				action.Kind,
			)
		}
		if validationErr := validate(action.Headers.Clone()); validationErr != nil {
			return nil, fmt.Errorf("middleware handler %q header replacement: %w", handler.HandlerID, validationErr)
		}
		current = action.Headers.Clone()
	}
	return current, nil
}

// composeBody retains preservation separately from arbitrary safe-JSON replacement.
func (s *Service) composeBody(
	ctx context.Context,
	handlers []Handler,
	original []byte,
	validate func([]byte) error,
) (modelexecution.WireBodyResult, error) {
	original = slices.Clone(original)
	current := slices.Clone(original)
	replaced := false
	for _, handler := range handlers {
		if handler.Kind != Body {
			continue
		}
		action, err := s.invoke(ctx, handler, Invocation{
			Context:         extension.Context{},
			Kind:            Body,
			OriginalHeaders: nil,
			CurrentHeaders:  nil,
			OriginalBody: slices.Clone(
				original,
			),
			CurrentBody: slices.Clone(current),
			Response:    modelexecution.WireResponse{},
		})
		if err != nil {
			return modelexecution.WireBodyResult{}, err
		}
		if action.Kind == Preserve {
			continue
		}
		if action.Kind != Replace {
			return modelexecution.WireBodyResult{}, fmt.Errorf(
				"middleware handler %q returned invalid body action %d",
				handler.HandlerID,
				action.Kind,
			)
		}
		if validationErr := validate(slices.Clone(action.Body)); validationErr != nil {
			return modelexecution.WireBodyResult{}, fmt.Errorf(
				"middleware handler %q body replacement: %w",
				handler.HandlerID,
				validationErr,
			)
		}
		current = slices.Clone(action.Body)
		replaced = true
	}
	return modelexecution.WireBodyResult{Body: current, Replaced: replaced}, nil
}

// observeResponse invokes ordered immutable metadata observers before SDK decoding.
func (s *Service) observeResponse(ctx context.Context, handlers []Handler, response modelexecution.WireResponse) error {
	response.Headers = response.Headers.Clone()
	for _, handler := range handlers {
		if handler.Kind != Response {
			continue
		}
		action, err := s.invoke(ctx, handler, Invocation{
			Context:         extension.Context{},
			Kind:            Response,
			OriginalHeaders: nil,
			CurrentHeaders:  nil,
			OriginalBody:    nil,
			CurrentBody:     nil,
			Response:        modelexecution.WireResponse{Status: response.Status, Headers: response.Headers.Clone()},
		})
		if err != nil {
			return err
		}
		if action.Kind != Acknowledge {
			return fmt.Errorf(
				"middleware handler %q returned invalid response action %d",
				handler.HandlerID,
				action.Kind,
			)
		}
	}
	return nil
}
