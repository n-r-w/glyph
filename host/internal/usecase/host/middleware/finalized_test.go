//go:build !integration

package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// commitFinalizedHandlers registers finalized-message handlers of one extension in the given order.
func commitFinalizedHandlers(service *Service, extensionID string, handlerIDs ...string) {
	handlers := make([]startup.AcceptedHandler, len(handlerIDs))
	for index, id := range handlerIDs {
		handlers[index] = startup.AcceptedHandler{ID: id, Kind: startup.RawHandlerKindFinalizedMessage}
	}
	service.CommitMiddlewareHandlers([]startup.AcceptedRegistration{
		{ID: extensionID, Path: "", Tools: nil, Handlers: handlers},
	})
}

// toolResultEntry creates a completed tool result of the given call and text blocks.
func toolResultEntry(callID, toolName string, texts ...string) agent.HistoryEntry {
	contents := make([]tool.ResultContent, len(texts))
	for index, text := range texts {
		contents[index] = tool.ResultContent{
			Kind: tool.ResultContentText, Text: mo.Some(text), Image: mo.None[tool.ResultImage](),
		}
	}
	return agent.HistoryEntry{
		Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](), Model: mo.None[model.Response](),
		ToolResult: mo.Some(agent.ToolResult{CallID: callID, ToolName: toolName, Contents: contents, IsError: false}),
	}
}

// modelEntry creates a model response entry.
func modelEntry(response model.Response) agent.HistoryEntry {
	return agent.HistoryEntry{
		Kind: agent.HistoryEntryModel, User: mo.None[model.Message](), Model: mo.Some(response),
		ToolResult: mo.None[agent.ToolResult](),
	}
}

// textBlock creates one final model text block.
func textBlock(text string) model.Content {
	return model.Content{
		Kind: model.ContentText, Text: mo.Some(text), Final: true,
		ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
	}
}

// toolCallBlock creates one final model tool call.
func toolCallBlock(t *testing.T, id, name string) model.Content {
	t.Helper()
	arguments, err := model.NewToolCallArguments([]byte(`{"path":"a"}`))
	require.NoError(t, err)
	return model.Content{
		Kind: model.ContentToolCall, Text: mo.None[string](), Final: true,
		ProviderContext: mo.None[model.ProviderContext](),
		ToolCall:        mo.Some(model.ToolCall{ID: id, Name: name, Arguments: arguments}),
	}
}

// providerResponse creates a terminal model response with Host-owned accounting and provider identity.
func providerResponse(content ...model.Content) model.Response {
	return model.Response{
		Content: content, Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](),
		Provider: mo.Some[model.ProviderID]("provider"), Model: mo.Some[model.ID]("configured"),
		ResponseModel: mo.Some[model.ID]("reported"), ResponseID: mo.Some("response"),
		Usage: mo.Some(model.Usage{
			InputTokens: 1, OutputTokens: 2, CachedInputTokens: 3, CacheWriteTokens: 4, ReasoningTokens: 0,
			TotalTokens: 10,
		}),
		Diagnostics: []model.Diagnostic{{Code: "code", Message: "message"}},
	}
}

// visibleResponse creates the model replacement that runtime mapping decodes: content without Host-owned fields.
func visibleResponse(content ...model.Content) model.Response {
	return model.Response{
		Content: content, Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
		Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
		ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
}

// TestFinalizeMessageComposesHandlers verifies registration order, detached original/current values, and skipped
// handlers of other kinds.
func TestFinalizeMessageComposesHandlers(t *testing.T) {
	t.Parallel()
	// Arrange two extensions whose handlers replace from current, preserve, and replace from the original.
	ctrl := gomock.NewController(t)
	runtime := NewMockRuntime(ctrl)
	contexts := NewMockContextIssuer(ctrl)
	service := New(runtime, contexts, nil)
	commitFinalizedHandlers(service, "first", "finalized-a", "finalized-keep")
	commitInputHandlers(service, "first", "input")
	commitFinalizedHandlers(service, "second", "finalized-b")
	finalized := toolResultEntry("call", "read", "file")
	contexts.EXPECT().IssueContext("first").Return(extension.Context{}, nil).Times(2)
	contexts.EXPECT().IssueContext("second").Return(extension.Context{}, nil)
	gomock.InOrder(
		runtime.EXPECT().HandleFinalizedMessage(gomock.Any(), "first", "finalized-a", gomock.Any()).DoAndReturn(
			func(
				_ context.Context, _, _ string, input Transformation[agent.HistoryEntry],
			) (Transformed[agent.HistoryEntry], error) {
				require.Equal(t, finalized, input.Original)
				require.Equal(t, finalized, input.Current)
				input.Original.ToolResult.MustGet().Contents[0].Text = mo.Some("mutated")
				return Transformed[agent.HistoryEntry]{
					Kind: Replace, Replacement: toolResultEntry("call", "read", "file", "A"),
				}, nil
			}),
		runtime.EXPECT().HandleFinalizedMessage(gomock.Any(), "first", "finalized-keep", gomock.Any()).Return(
			Transformed[agent.HistoryEntry]{Kind: Preserve, Replacement: agent.HistoryEntry{}}, nil,
		),
		runtime.EXPECT().HandleFinalizedMessage(gomock.Any(), "second", "finalized-b", gomock.Any()).DoAndReturn(
			func(
				_ context.Context, _, _ string, input Transformation[agent.HistoryEntry],
			) (Transformed[agent.HistoryEntry], error) {
				require.Equal(t, toolResultEntry("call", "read", "file"), input.Original)
				require.Equal(t, toolResultEntry("call", "read", "file", "A"), input.Current)
				return Transformed[agent.HistoryEntry]{
					Kind: Replace, Replacement: toolResultEntry("call", "read", "file", "B"),
				}, nil
			}),
	)

	// Act by finalizing the completed tool result.
	result, err := service.finalizeMessage(t.Context(), failOnCancellation, finalized)

	// Assert the last replacement is final and the caller value stayed unchanged.
	require.NoError(t, err)
	assert.Equal(t, toolResultEntry("call", "read", "file", "B"), result)
	assert.Equal(t, toolResultEntry("call", "read", "file"), finalized)
}

// TestFinalizeMessageKeepsHostOwnedModelFields verifies a model replacement changes only content, replay, and tool-call
// arguments.
func TestFinalizeMessageKeepsHostOwnedModelFields(t *testing.T) {
	t.Parallel()
	// Arrange a tool-use response and a handler that replaces its text, moves its tool call, and changes its arguments.
	ctrl := gomock.NewController(t)
	runtime := NewMockRuntime(ctrl)
	contexts := NewMockContextIssuer(ctrl)
	service := New(runtime, contexts, nil)
	commitFinalizedHandlers(service, "extension", "finalized")
	original := providerResponse(textBlock("answer"), toolCallBlock(t, "call", "read"))
	contexts.EXPECT().IssueContext("extension").Return(extension.Context{}, nil)
	edited := toolCallBlock(t, "call", "read")
	arguments, err := model.NewToolCallArguments([]byte(`{"path":"b"}`))
	require.NoError(t, err)
	edited.ToolCall = mo.Some(model.ToolCall{ID: "call", Name: "read", Arguments: arguments})
	runtime.EXPECT().HandleFinalizedMessage(gomock.Any(), "extension", "finalized", gomock.Any()).Return(
		Transformed[agent.HistoryEntry]{
			Kind: Replace, Replacement: modelEntry(visibleResponse(edited, textBlock("edited"))),
		}, nil,
	)

	// Act by finalizing the model response.
	result, finalizeErr := service.finalizeMessage(t.Context(), failOnCancellation, modelEntry(original))

	// Assert the replaced content and arguments with the original outcome, provider identity, accounting, and
	// diagnostics.
	require.NoError(t, finalizeErr)
	expected := providerResponse(edited, textBlock("edited"))
	assert.Equal(t, modelEntry(expected), result)
}

// TestFinalizeMessageWithoutHandlers verifies that a message without finalized-message handlers stays unchanged.
func TestFinalizeMessageWithoutHandlers(t *testing.T) {
	t.Parallel()
	// Arrange an owner with only a handler of another kind.
	ctrl := gomock.NewController(t)
	service := New(NewMockRuntime(ctrl), NewMockContextIssuer(ctrl), nil)
	commitInputHandlers(service, "extension", "input")
	finalized := userEntry("hello")

	// Act by finalizing the user message.
	result, err := service.finalizeMessage(t.Context(), failOnCancellation, finalized)

	// Assert the message is final unchanged.
	require.NoError(t, err)
	assert.Equal(t, finalized, result)
}

// TestFinalizeMessageChainFailure verifies a failed handler result stops the chain as an extension failure.
func TestFinalizeMessageChainFailure(t *testing.T) {
	t.Parallel()
	source := errors.New("finalized source failed")
	for name, outcome := range map[string]struct {
		original agent.HistoryEntry
		action   func(t *testing.T) Transformed[agent.HistoryEntry]
		err      error
		text     string
	}{
		"handler error": {
			original: userEntry("hello"), err: source,
			action: func(*testing.T) Transformed[agent.HistoryEntry] { return Transformed[agent.HistoryEntry]{} },
			text:   `middleware handler "finalized-a": finalized source failed`,
		},
		"invalid action": {
			original: userEntry("hello"), err: nil,
			action: func(*testing.T) Transformed[agent.HistoryEntry] { return Transformed[agent.HistoryEntry]{} },
			text:   `middleware handler "finalized-a" returned invalid finalized-message action`,
		},
		"handle action": {
			original: userEntry("hello"), err: nil,
			action: func(*testing.T) Transformed[agent.HistoryEntry] {
				return Transformed[agent.HistoryEntry]{Kind: Handle, Replacement: agent.HistoryEntry{}}
			},
			text: `middleware handler "finalized-a" returned invalid finalized-message action`,
		},
		"changed role": {
			original: userEntry("hello"), err: nil,
			action: func(*testing.T) Transformed[agent.HistoryEntry] {
				return Transformed[agent.HistoryEntry]{
					Kind: Replace, Replacement: modelEntry(visibleResponse(textBlock("hello"))),
				}
			},
			text: `middleware handler "finalized-a" finalized-message replacement: ` +
				`message role model differs from original role user`,
		},
		"empty user message": {
			original: userEntry("hello"), err: nil,
			action: func(*testing.T) Transformed[agent.HistoryEntry] {
				return Transformed[agent.HistoryEntry]{Kind: Replace, Replacement: agent.HistoryEntry{
					Kind: agent.HistoryEntryUser, User: mo.Some(model.Message{}), Model: mo.None[model.Response](),
					ToolResult: mo.None[agent.ToolResult](),
				}}
			},
			text: `middleware handler "finalized-a" finalized-message replacement: ` +
				`at least one content block is required`,
		},
		"changed call identifier": {
			original: toolResultEntry("call", "read", "file"), err: nil,
			action: func(*testing.T) Transformed[agent.HistoryEntry] {
				return Transformed[agent.HistoryEntry]{Kind: Replace, Replacement: toolResultEntry("other", "read", "x")}
			},
			text: `middleware handler "finalized-a" finalized-message replacement: ` +
				`tool call "other" differs from finalized call "call"`,
		},
		"changed tool name": {
			original: toolResultEntry("call", "read", "file"), err: nil,
			action: func(*testing.T) Transformed[agent.HistoryEntry] {
				return Transformed[agent.HistoryEntry]{Kind: Replace, Replacement: toolResultEntry("call", "write", "x")}
			},
			text: `middleware handler "finalized-a" finalized-message replacement: ` +
				`tool "write" differs from finalized tool "read"`,
		},
		"tool image without media type": {
			original: toolResultEntry("call", "read", "file"), err: nil,
			action: func(*testing.T) Transformed[agent.HistoryEntry] {
				replacement := toolResultEntry("call", "read")
				replacement.ToolResult = mo.Some(agent.ToolResult{
					CallID: "call", ToolName: "read", IsError: false, Contents: []tool.ResultContent{{
						Kind: tool.ResultContentImage, Text: mo.None[string](),
						Image: mo.Some(tool.ResultImage{MediaType: "", Data: []byte{1}}),
					}},
				})
				return Transformed[agent.HistoryEntry]{Kind: Replace, Replacement: replacement}
			},
			text: `middleware handler "finalized-a" finalized-message replacement: ` +
				`content 0: image media type and data are required`,
		},
		"removed tool call": {
			original: modelEntry(providerResponse(textBlock("answer"), toolCallBlock(t, "call", "read"))), err: nil,
			action: func(*testing.T) Transformed[agent.HistoryEntry] {
				return Transformed[agent.HistoryEntry]{
					Kind: Replace, Replacement: modelEntry(visibleResponse(textBlock("answer"))),
				}
			},
			text: `middleware handler "finalized-a" finalized-message replacement: ` +
				`tool calls [] differ from finalized tool calls [id="call" name="read"]`,
		},
		"added tool call": {
			original: modelEntry(providerResponse(textBlock("answer"))), err: nil,
			action: func(t *testing.T) Transformed[agent.HistoryEntry] {
				return Transformed[agent.HistoryEntry]{
					Kind:        Replace,
					Replacement: modelEntry(visibleResponse(textBlock("answer"), toolCallBlock(t, "call", "read"))),
				}
			},
			text: `middleware handler "finalized-a" finalized-message replacement: ` +
				`tool calls [id="call" name="read"] differ from finalized tool calls []`,
		},
		"renamed tool call": {
			original: modelEntry(providerResponse(toolCallBlock(t, "call", "read"))), err: nil,
			action: func(t *testing.T) Transformed[agent.HistoryEntry] {
				return Transformed[agent.HistoryEntry]{
					Kind: Replace, Replacement: modelEntry(visibleResponse(toolCallBlock(t, "call", "write"))),
				}
			},
			text: `middleware handler "finalized-a" finalized-message replacement: ` +
				`tool calls [id="call" name="write"] differ from finalized tool calls [id="call" name="read"]`,
		},
		"tool calls with colons": {
			original: modelEntry(providerResponse(toolCallBlock(t, "a:b", "c"))), err: nil,
			action: func(t *testing.T) Transformed[agent.HistoryEntry] {
				return Transformed[agent.HistoryEntry]{
					Kind: Replace, Replacement: modelEntry(visibleResponse(toolCallBlock(t, "a", "b:c"))),
				}
			},
			text: `middleware handler "finalized-a" finalized-message replacement: ` +
				`tool calls [id="a" name="b:c"] differ from finalized tool calls [id="a:b" name="c"]`,
		},
		"reordered tool calls": {
			original: modelEntry(providerResponse(toolCallBlock(t, "a", "read"), toolCallBlock(t, "b", "read"))),
			err:      nil,
			action: func(t *testing.T) Transformed[agent.HistoryEntry] {
				return Transformed[agent.HistoryEntry]{Kind: Replace, Replacement: modelEntry(visibleResponse(
					toolCallBlock(t, "b", "read"), toolCallBlock(t, "a", "read"),
				))}
			},
			text: `middleware handler "finalized-a" finalized-message replacement: ` +
				`tool calls [id="b" name="read", id="a" name="read"] differ from finalized tool calls ` +
				`[id="a" name="read", id="b" name="read"]`,
		},
		"inconsistent model content": {
			original: modelEntry(providerResponse(textBlock("answer"))), err: nil,
			action: func(*testing.T) Transformed[agent.HistoryEntry] {
				block := textBlock("answer")
				block.Text = mo.None[string]()
				return Transformed[agent.HistoryEntry]{Kind: Replace, Replacement: modelEntry(visibleResponse(block))}
			},
			text: `middleware handler "finalized-a" finalized-message replacement: ` +
				`terminal model content 0: invalid payload fields for kind 1`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange a failing first handler and a later handler without expectations.
			ctrl := gomock.NewController(t)
			runtime := NewMockRuntime(ctrl)
			contexts := NewMockContextIssuer(ctrl)
			service := New(runtime, contexts, nil)
			commitFinalizedHandlers(service, "extension", "finalized-a", "finalized-later")
			contexts.EXPECT().IssueContext("extension").Return(extension.Context{}, nil)
			runtime.EXPECT().HandleFinalizedMessage(gomock.Any(), "extension", "finalized-a", gomock.Any()).Return(
				outcome.action(t), outcome.err,
			)

			// Act by finalizing the message.
			_, err := service.finalizeMessage(t.Context(), failOnCancellation, outcome.original)

			// Assert the complete cause and the extension-failure category.
			require.ErrorContains(t, err, outcome.text)
			assert.Equal(t, "EXTENSION_FAILED", currentCode(err))
		})
	}
}

// TestFinalizeMessageCancellation verifies owner cancellation keeps its uncategorized cancellation meaning.
func TestFinalizeMessageCancellation(t *testing.T) {
	t.Parallel()
	// Arrange one registered handler and a canceled owner context.
	ctrl := gomock.NewController(t)
	service := New(NewMockRuntime(ctrl), NewMockContextIssuer(ctrl), nil)
	commitFinalizedHandlers(service, "extension", "finalized-a")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// Act by finalizing a user message.
	_, err := service.finalizeMessage(ctx, failOnCancellation, userEntry("hello"))

	// Assert the cancellation names the handler and has no public category.
	require.EqualError(t, err, `middleware handler "finalized-a": context canceled`)
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, currentCode(err))
}

// TestFinalizeCoreValues verifies the Agent Core finalization methods return the final value of their role.
func TestFinalizeCoreValues(t *testing.T) {
	t.Parallel()
	// Arrange one handler that replaces a tool result and then a model response.
	ctrl := gomock.NewController(t)
	runtime := NewMockRuntime(ctrl)
	contexts := NewMockContextIssuer(ctrl)
	service := New(runtime, contexts, nil)
	commitFinalizedHandlers(service, "extension", "finalized")
	contexts.EXPECT().IssueContext("extension").Return(extension.Context{}, nil).Times(2)
	gomock.InOrder(
		runtime.EXPECT().HandleFinalizedMessage(gomock.Any(), "extension", "finalized", gomock.Any()).Return(
			Transformed[agent.HistoryEntry]{Kind: Replace, Replacement: toolResultEntry("call", "read", "final")}, nil,
		),
		runtime.EXPECT().HandleFinalizedMessage(gomock.Any(), "extension", "finalized", gomock.Any()).Return(
			Transformed[agent.HistoryEntry]{
				Kind: Replace, Replacement: modelEntry(visibleResponse(textBlock("final answer"))),
			}, nil,
		),
	)

	// Act by finalizing a completed tool result and a terminal model response.
	result, resultErr := service.FinalizeToolResult(
		t.Context(), toolResultEntry("call", "read", "file").ToolResult.MustGet(),
	)
	response, responseErr := service.FinalizeModelResponse(t.Context(), providerResponse(textBlock("answer")))

	// Assert the final tool result and the final model response.
	require.NoError(t, resultErr)
	assert.Equal(t, toolResultEntry("call", "read", "final").ToolResult.MustGet(), result)
	require.NoError(t, responseErr)
	assert.Equal(t, providerResponse(textBlock("final answer")), response)
}

// TestProcessInputFinalizesUser verifies that finalized-message handlers receive the input-chain result before the
// final model check and that their replacement is the final input.
func TestProcessInputFinalizesUser(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		replacement agent.HistoryEntry
		err         string
		code        string
	}{
		"text replacement": {replacement: userEntry("hello A final"), err: "", code: ""},
		"added image": {
			replacement: agent.HistoryEntry{
				Kind: agent.HistoryEntryUser, User: mo.Some(imageMessage()), Model: mo.None[model.Response](),
				ToolResult: mo.None[agent.ToolResult](),
			},
			err: "model text does not accept image input at content 1", code: "INPUT_INVALID",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange one input handler, one finalized-message handler, and a text-only model.
			ctrl := gomock.NewController(t)
			runtime := NewMockRuntime(ctrl)
			contexts := NewMockContextIssuer(ctrl)
			models := NewMockSelectedModel(ctrl)
			service := New(runtime, contexts, models)
			commitFinalizedHandlers(service, "extension", "finalized")
			commitInputHandlers(service, "extension", "input")
			contexts.EXPECT().IssueContext("extension").Return(extension.Context{}, nil).Times(2)
			gomock.InOrder(
				runtime.EXPECT().HandleUserInput(gomock.Any(), "extension", "input", gomock.Any()).Return(
					Transformed[model.Message]{Kind: Replace, Replacement: model.TextMessage("hello A")}, nil,
				),
				runtime.EXPECT().
					HandleFinalizedMessage(gomock.Any(), "extension", "finalized", gomock.Any()).
					DoAndReturn(
						func(
							_ context.Context, _, _ string, input Transformation[agent.HistoryEntry],
						) (Transformed[agent.HistoryEntry], error) {
							require.Equal(t, userEntry("hello A"), input.Original)
							return Transformed[agent.HistoryEntry]{Kind: Replace, Replacement: test.replacement}, nil
						}),
				models.EXPECT().ActiveBinding().Return(selectedBinding("text", model.InputModalityText)),
			)

			// Act by processing the submitted message.
			final, err := service.ProcessInput(t.Context(), model.TextMessage("hello"))

			// Assert the final input or the category of the final model check.
			if test.err != "" {
				require.EqualError(t, err, test.err)
				assert.Equal(t, test.code, currentCode(err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, mo.Some(test.replacement.User.MustGet()), final)
		})
	}
}

// TestProcessInputFinalizationFailure verifies a failed finalized-message chain fails before the final model check.
func TestProcessInputFinalizationFailure(t *testing.T) {
	t.Parallel()
	// Arrange a failing finalized-message handler and no model expectation.
	ctrl := gomock.NewController(t)
	runtime := NewMockRuntime(ctrl)
	contexts := NewMockContextIssuer(ctrl)
	service := New(runtime, contexts, NewMockSelectedModel(ctrl))
	commitFinalizedHandlers(service, "extension", "finalized", "finalized-later")
	contexts.EXPECT().IssueContext("extension").Return(extension.Context{}, nil)
	runtime.EXPECT().HandleFinalizedMessage(gomock.Any(), "extension", "finalized", gomock.Any()).Return(
		Transformed[agent.HistoryEntry]{}, errors.New("source failed"),
	)

	// Act by processing the submitted message.
	_, err := service.ProcessInput(t.Context(), model.TextMessage("hello"))

	// Assert the complete cause and the extension-failure category.
	require.EqualError(t, err, `middleware handler "finalized": source failed`)
	assert.Equal(t, "EXTENSION_FAILED", currentCode(err))
}

// TestHasFinalizedMessageHandlers verifies that only accepted finalized-message handlers are reported.
func TestHasFinalizedMessageHandlers(t *testing.T) {
	t.Parallel()
	// Arrange an owner with an input handler only.
	ctrl := gomock.NewController(t)
	service := New(NewMockRuntime(ctrl), NewMockContextIssuer(ctrl), nil)
	commitInputHandlers(service, "extension", "input")

	// Act before and after a finalized-message handler is accepted.
	before := service.HasFinalizedMessageHandlers()
	commitFinalizedHandlers(service, "extension", "finalized")
	after := service.HasFinalizedMessageHandlers()

	// Assert only the accepted finalized-message handler is reported.
	assert.False(t, before)
	assert.True(t, after)
}

// TestFinalizeCoreValuesBeforeAdmission verifies that Agent Core values stay unchanged when the run is canceled
// before the first handler is admitted, while a configured response keeps its cancellation failure.
func TestFinalizeCoreValuesBeforeAdmission(t *testing.T) {
	t.Parallel()
	// Arrange a preserve-only handler that must not be admitted and a canceled run.
	ctrl := gomock.NewController(t)
	service := New(NewMockRuntime(ctrl), NewMockContextIssuer(ctrl), nil)
	commitFinalizedHandlers(service, "extension", "preserve")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	response := providerResponse(textBlock("answer"))
	result := toolResultEntry("call", "read", "file").ToolResult.MustGet()

	// Act through the Agent Core and configured-model finalization methods.
	finalResponse, responseErr := service.FinalizeModelResponse(ctx, response)
	finalResult, resultErr := service.FinalizeToolResult(ctx, result)
	_, configuredErr := service.FinalizeConfiguredResponse(ctx, response)

	// Assert the unchanged Agent Core values and the uncategorized configured cancellation.
	require.NoError(t, responseErr)
	assert.Equal(t, response, finalResponse)
	require.NoError(t, resultErr)
	assert.Equal(t, result, finalResult)
	require.ErrorIs(t, configuredErr, context.Canceled)
	assert.Empty(t, currentCode(configuredErr))
}
