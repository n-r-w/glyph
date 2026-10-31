//go:build !integration

package middleware

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	"github.com/n-r-w/glyph/internal/errtree"
)

// selectedBinding creates the active binding of one model with the given input modalities.
func selectedBinding(id model.ID, input ...model.InputModality) modelexecution.CatalogBinding {
	return modelexecution.CatalogBinding{
		Model: model.Descriptor{
			Provider: "provider", Model: id, Input: input, ContextWindow: 1, MaxTokens: 1,
			ReasoningCapabilities: model.ReasoningCapabilities{Supported: false, Choices: nil, Default: ""},
			ToolCapabilities: model.ToolCapabilities{
				StrictJSONSchema: false, Grammar: model.GrammarCapabilities{Lark: false, Regex: false},
			},
			Pricing: mo.None[model.Pricing](),
		},
		ReasoningChoice: model.ReasoningChoiceOff,
		Provider:        nil,
	}
}

// imageMessage creates text followed by one image block.
func imageMessage() model.Message {
	return model.Message{Content: []model.InputContent{
		model.TextContent("look"), model.ImageContent("image/png", []byte{1}),
	}}
}

// TestProcessInputValidatesSelectedModel verifies final input is checked against the selected model descriptor.
func TestProcessInputValidatesSelectedModel(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		binding modelexecution.CatalogBinding
		message model.Message
		err     string
	}{
		{
			name: "image model", binding: selectedBinding("vision", model.InputModalityText, model.InputModalityImage),
			message: imageMessage(), err: "",
		},
		{
			name: "text-only model with text", binding: selectedBinding("text", model.InputModalityText),
			message: model.TextMessage("hello"), err: "",
		},
		{
			name: "text-only model with image", binding: selectedBinding("text", model.InputModalityText),
			message: imageMessage(), err: "model text does not accept image input at content 1",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange the selected model without registered handlers.
			ctrl := gomock.NewController(t)
			models := NewMockSelectedModel(ctrl)
			models.EXPECT().ActiveBinding().Return(test.binding)
			service := New(NewMockRuntime(ctrl), NewMockContextIssuer(ctrl), models)

			// Act by processing the submitted message.
			final, err := service.ProcessInput(t.Context(), test.message)

			// Assert supported input is final and unsupported input has the public category and complete text.
			if test.err == "" {
				require.NoError(t, err)
				assert.Equal(t, mo.Some(test.message), final)
				return
			}
			require.EqualError(t, err, test.err)
			assert.Equal(t, "INPUT_INVALID", currentCode(err))
		})
	}
}

// currentCode returns the first public category among the current error nodes.
func currentCode(err error) string {
	for current := range errtree.IterateCurrentErrors(err) {
		//nolint:errorlint // The test inspects the same current nodes as public failure mapping.
		if coded, classified := current.(errtree.CodedError); classified {
			return coded.Code()
		}
	}
	return ""
}

// commitInputHandlers registers user-input handlers of one extension in the given order.
func commitInputHandlers(service *Service, extensionID string, handlerIDs ...string) {
	handlers := make([]startup.AcceptedHandler, len(handlerIDs))
	for index, id := range handlerIDs {
		handlers[index] = startup.AcceptedHandler{ID: id, Kind: startup.RawHandlerKindUserInput}
	}
	service.CommitMiddlewareHandlers([]startup.AcceptedRegistration{
		{ID: extensionID, Path: "", Tools: nil, Handlers: handlers},
	})
}

// TestProcessInputComposesHandlers verifies registration order, detached original/current values and final validation.
func TestProcessInputComposesHandlers(t *testing.T) {
	t.Parallel()
	// Arrange two extensions whose handlers replace from current, preserve and replace from the original.
	ctrl := gomock.NewController(t)
	runtime := NewMockRuntime(ctrl)
	contexts := NewMockContextIssuer(ctrl)
	models := NewMockSelectedModel(ctrl)
	service := New(runtime, contexts, models)
	commitInputHandlers(service, "first", "input-a", "input-keep")
	commitInputHandlers(service, "second", "input-b")
	submitted := imageMessage()
	textOnly := model.TextMessage("look A")
	final := model.Message{Content: append(slices.Clone(submitted.Content[:1]), model.TextMessage("B").Content...)}
	contexts.EXPECT().IssueContext("first").Return(extension.Context{}, nil).Times(2)
	contexts.EXPECT().IssueContext("second").Return(extension.Context{}, nil)
	gomock.InOrder(
		runtime.EXPECT().HandleUserInput(gomock.Any(), "first", "input-a", gomock.Any()).DoAndReturn(
			func(_ context.Context, _, _ string, input Transformation[model.Message]) (Transformed[model.Message], error) {
				require.Equal(t, submitted, input.Original)
				require.Equal(t, submitted, input.Current)
				input.Original.Content[1].Data.MustGet()[0] = 9
				return Transformed[model.Message]{Kind: Replace, Replacement: textOnly}, nil
			}),
		runtime.EXPECT().HandleUserInput(gomock.Any(), "first", "input-keep", gomock.Any()).Return(
			Transformed[model.Message]{Kind: Preserve, Replacement: model.Message{}}, nil,
		),
		runtime.EXPECT().HandleUserInput(gomock.Any(), "second", "input-b", gomock.Any()).DoAndReturn(
			func(_ context.Context, _, _ string, input Transformation[model.Message]) (Transformed[model.Message], error) {
				require.Equal(t, imageMessage(), input.Original)
				require.Equal(t, textOnly, input.Current)
				return Transformed[model.Message]{Kind: Replace, Replacement: final}, nil
			}),
		models.EXPECT().ActiveBinding().Return(selectedBinding("text", model.InputModalityText)),
	)

	// Act by processing the submitted message.
	result, err := service.ProcessInput(t.Context(), submitted)

	// Assert the last replacement is final and the caller value stayed unchanged.
	require.NoError(t, err)
	assert.Equal(t, mo.Some(final), result)
	assert.Equal(t, imageMessage(), submitted)
}

// TestProcessInputHandled verifies a handled input ends the chain without final validation.
func TestProcessInputHandled(t *testing.T) {
	t.Parallel()
	// Arrange a handling handler followed by a handler that must not run, and no model expectation.
	ctrl := gomock.NewController(t)
	runtime := NewMockRuntime(ctrl)
	contexts := NewMockContextIssuer(ctrl)
	service := New(runtime, contexts, NewMockSelectedModel(ctrl))
	commitInputHandlers(service, "extension", "input-handle", "input-later")
	contexts.EXPECT().IssueContext("extension").Return(extension.Context{}, nil)
	runtime.EXPECT().HandleUserInput(gomock.Any(), "extension", "input-handle", gomock.Any()).Return(
		Transformed[model.Message]{Kind: Handle, Replacement: model.Message{}}, nil,
	)

	// Act by processing an image for any model.
	result, err := service.ProcessInput(t.Context(), imageMessage())

	// Assert no final message remains.
	require.NoError(t, err)
	assert.True(t, result.IsNone())
}

// TestProcessInputChainFailure verifies a failed handler result stops the chain as an extension failure.
func TestProcessInputChainFailure(t *testing.T) {
	t.Parallel()
	source := errors.New("input source failed")
	for name, outcome := range map[string]struct {
		action Transformed[model.Message]
		err    error
		text   string
	}{
		"handler error": {
			action: Transformed[model.Message]{}, err: source,
			text: `middleware handler "input-a": input source failed`,
		},
		"invalid action": {
			action: Transformed[model.Message]{}, err: nil,
			text: `middleware handler "input-a" returned invalid user-input action 0`,
		},
		"malformed replacement": {
			action: Transformed[model.Message]{Kind: Replace, Replacement: model.Message{}}, err: nil,
			text: `middleware handler "input-a" user-input replacement: at least one content block is required`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange a failing first handler and a later handler without expectations.
			ctrl := gomock.NewController(t)
			runtime := NewMockRuntime(ctrl)
			contexts := NewMockContextIssuer(ctrl)
			service := New(runtime, contexts, NewMockSelectedModel(ctrl))
			commitInputHandlers(service, "extension", "input-a", "input-later")
			contexts.EXPECT().IssueContext("extension").Return(extension.Context{}, nil)
			runtime.EXPECT().HandleUserInput(gomock.Any(), "extension", "input-a", gomock.Any()).Return(
				outcome.action, outcome.err,
			)

			// Act by processing the submitted message.
			_, err := service.ProcessInput(t.Context(), model.TextMessage("hello"))

			// Assert the complete cause and the extension-failure category.
			require.EqualError(t, err, outcome.text)
			assert.Equal(t, "EXTENSION_FAILED", currentCode(err))
		})
	}
}

// TestProcessInputCancellation verifies owner cancellation keeps its uncategorized cancellation meaning.
func TestProcessInputCancellation(t *testing.T) {
	t.Parallel()
	// Arrange one registered handler and a canceled owner context.
	ctrl := gomock.NewController(t)
	service := New(NewMockRuntime(ctrl), NewMockContextIssuer(ctrl), NewMockSelectedModel(ctrl))
	commitInputHandlers(service, "extension", "input-a")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// Act by processing the submitted message.
	_, err := service.ProcessInput(ctx, model.TextMessage("hello"))

	// Assert the cancellation names the handler and has no public category.
	require.EqualError(t, err, `middleware handler "input-a": context canceled`)
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, currentCode(err))
}
