//go:build !integration

package runtime

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/middleware"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// userInputResponse wraps one user-input action in a handler response.
//
//nolint:exhaustruct_v5 // Protobuf oneof builders set only their active action.
func userInputResponse(action *extensionpb.UserInputAction) *extensionpb.HandleResponse {
	return extensionpb.HandleResponse_builder{UserInput: action}.Build()
}

// TestUserInputMapping maps original/current input and every decoded action kind.
func TestUserInputMapping(t *testing.T) {
	t.Parallel()
	// Arrange one invocation whose original holds an image.
	original := model.Message{Content: []model.InputContent{
		model.TextContent("look"), model.ImageContent("image/png", []byte{1}),
	}}
	input := middleware.Transformation[model.Message]{
		Context: middlewareBinding(), Original: original, Current: model.TextMessage("look A"),
	}
	replacement := extensionpb.SessionTreeUserMessage_builder{Content: []*extensionpb.SessionTreeUserContent{
		extensionpb.SessionTreeUserContent_builder{Text: new("look B"), Image: nil}.Build(),
	}}.Build()

	// Act by mapping the invocation and every handler response.
	request := mapUserInputRequest("input", input)
	preserved, preserveErr := mapUserInputAction(userInputResponse(
		extensionpb.UserInputAction_builder{Preserve: new(true), Replace: nil, Handle: nil}.Build(),
	))
	replaced, replaceErr := mapUserInputAction(userInputResponse(
		extensionpb.UserInputAction_builder{Preserve: nil, Replace: replacement, Handle: nil}.Build(),
	))
	handled, handleErr := mapUserInputAction(userInputResponse(
		extensionpb.UserInputAction_builder{Preserve: nil, Replace: nil, Handle: new(true)}.Build(),
	))
	missing, missingErr := mapUserInputAction(userInputResponse(
		extensionpb.UserInputAction_builder{Preserve: nil, Replace: nil, Handle: new(false)}.Build(),
	))

	// Assert the public payload and the actions left for owner validation.
	require.Equal(t, "input", request.GetHandlerId())
	require.Equal(t, "binding", request.GetContext().GetContextId())
	require.Len(t, request.GetUserInput().GetOriginal().GetContent(), 2)
	require.Equal(t, []byte{1}, request.GetUserInput().GetOriginal().GetContent()[1].GetImage().GetData())
	require.Equal(t, "look A", request.GetUserInput().GetCurrent().GetContent()[0].GetText())
	require.NoError(t, errors.Join(preserveErr, replaceErr, handleErr, missingErr))
	require.Equal(t, middleware.Preserve, preserved.Kind)
	require.Equal(
		t,
		middleware.Transformed[model.Message]{Kind: middleware.Replace, Replacement: model.TextMessage("look B")},
		replaced,
	)
	require.Equal(t, middleware.Handle, handled.Kind)
	require.Equal(t, middleware.ActionKind(0), missing.Kind)
}
