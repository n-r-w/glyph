package middleware

import (
	"context"
	"fmt"
	"strings"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// toolPolicyRejectionFormat is the model-visible text of a rejected call: the extension, the handler, and the reason.
const toolPolicyRejectionFormat = "tool call rejected by extension %q handler %q: %s"

// ApplyToolPolicy runs the tool-policy handlers in registration order for one tool call. Each handler receives the
// immutable original call and the call left by preceding handlers, and allows the call, replaces its input, or rejects
// it. validate checks the input of each replacement against the tool schema before the next handler runs. The result
// is the final call, or a rejection message with the complete reason of the handler that ended the chain; an error
// reports the failure of a handler or an invalid action and ends the chain. The caller classifies the error.
func (s *Service) ApplyToolPolicy(
	ctx context.Context,
	call model.ToolCall,
	validate func(arguments []byte) error,
) (model.ToolCall, mo.Option[string], error) {
	current := call
	fail := func(err error) (model.ToolCall, mo.Option[string], error) {
		return model.ToolCall{}, mo.None[string](), err
	}
	for _, handler := range s.snapshotHandlers() {
		if handler.Kind != ToolPolicy {
			continue
		}
		binding, err := s.bind(ctx, handler)
		if err != nil {
			return fail(err)
		}
		action, err := s.runtime.HandleToolPolicy(
			ctx,
			handler.ExtensionID,
			handler.HandlerID,
			Transformation[model.ToolCall]{
				Context: binding, Original: call, Current: current,
			},
		)
		if err != nil {
			return fail(fmt.Errorf("middleware handler %q: %w", handler.HandlerID, err))
		}
		//nolint:exhaustive // Acknowledge and Handle belong to other extension points and are invalid here.
		switch action.Kind {
		case Preserve:
			continue
		case Replace:
			replaced, replaceErr := acceptToolPolicyReplacement(current, action.Arguments, validate)
			if replaceErr != nil {
				return fail(fmt.Errorf(
					"middleware handler %q %s replacement: %w", handler.HandlerID, ToolPolicy, replaceErr,
				))
			}
			current = replaced
		case Reject:
			if strings.TrimSpace(action.Reason) == "" {
				return fail(fmt.Errorf(
					"middleware handler %q %s rejection reason is empty", handler.HandlerID, ToolPolicy,
				))
			}
			return current, mo.Some(fmt.Sprintf(
				toolPolicyRejectionFormat, handler.ExtensionID, handler.HandlerID, action.Reason,
			)), nil
		default:
			return fail(fmt.Errorf(
				"middleware handler %q returned invalid %s action %d", handler.HandlerID, ToolPolicy, action.Kind,
			))
		}
	}
	return current, mo.None[string](), nil
}

// acceptToolPolicyReplacement returns the call with the replacement input after the input passes validation.
func acceptToolPolicyReplacement(
	current model.ToolCall,
	arguments []byte,
	validate func([]byte) error,
) (model.ToolCall, error) {
	parsed, err := model.NewToolCallArguments(arguments)
	if err != nil {
		return model.ToolCall{}, err
	}
	if validateErr := validate(arguments); validateErr != nil {
		return model.ToolCall{}, validateErr
	}
	current.Arguments = parsed
	return current, nil
}
