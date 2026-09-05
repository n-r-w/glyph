//go:build !integration

package extension

import (
	"encoding/json/v2"
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// TestConfiguredModelRequestMapsPublicTerminalResponse verifies request validation and provider-private omission.
func TestConfiguredModelRequestMapsPublicTerminalResponse(t *testing.T) {
	t.Parallel()

	// Arrange one explicit request and a terminal response with every public content kind and private reasoning context.
	controller := gomock.NewController(t)
	contexts := NewMockContextOperations(controller)
	runtime := NewMockRuntimeOperations(controller)
	reference := extensiondomain.ContextRef{ID: "context", RuntimeInstanceID: "runtime", SessionID: "session"}
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh}
	history := []agent.HistoryEntry{
		{
			Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("question")),
			Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
		},
		{
			Kind: agent.HistoryEntryModel, User: mo.None[model.Message](),
			Model: mo.Some(model.Response{
				Content: []model.Content{{
					Kind: model.ContentText, Text: mo.Some("prior answer"), Final: true,
					ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
				}},
				Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
				Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](),
				ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
				Usage: mo.None[model.Usage](), Diagnostics: nil,
			}), ToolResult: mo.None[agent.ToolResult](),
		},
	}
	privateContext := model.ProviderContext{
		Source: model.ProviderContextSource{
			ProviderID: "provider", API: "private-api", Model: "model", CompatibilityKey: mo.None[string](),
		},
		Payload: []byte("private-reasoning-context"),
	}
	response := model.Response{
		Content: []model.Content{
			{
				Kind: model.ContentText, Text: mo.Some("answer"), Final: true,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind: model.ContentRefusal, Text: mo.Some("refusal"), Final: true,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind: model.ContentReasoning, Text: mo.Some("visible reasoning"), Final: true,
				ProviderContext: mo.Some(privateContext), ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind: model.ContentReasoning, Text: mo.None[string](), Final: true,
				ProviderContext: mo.Some(privateContext), ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind: model.ContentToolCall, Text: mo.None[string](), Final: true,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.Some(model.ToolCall{
					ID: "call", Name: "do-not-run", Arguments: map[string]any{"value": "exact"},
				}),
			},
		},
		Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.Some("terminal detail"),
		Provider: mo.Some(model.ProviderID("provider")), Model: mo.Some(model.ID("model")),
		ResponseModel: mo.Some(model.ID("reported-model")), ResponseID: mo.Some("response"),
		Usage: mo.Some(model.Usage{
			InputTokens: 3, OutputTokens: 5, CachedInputTokens: 2,
			CacheWriteTokens: 1, ReasoningTokens: 2, TotalTokens: 8,
		}),
		Diagnostics: []model.Diagnostic{{Code: "notice", Message: "complete diagnostic"}},
	}
	contexts.EXPECT().ValidateContext("extension", "runtime", reference).Return(nil)
	released := false
	runtime.EXPECT().BeginContextOperation(gomock.Any(), "extension", "runtime").Return(func() { released = true }, nil)
	contexts.EXPECT().Request(gomock.Any(), "extension", "runtime", reference, selection, "", history).
		Return(response, nil)
	service := New(contexts, runtime, "extension", "runtime")
	request := new(extensionpb.ExtensionRequest)
	request.SetConfiguredModel(extensionpb.ConfiguredModelRequest_builder{
		Context: extensionpb.ExtensionContextRef_builder{
			ContextId: new("context"), RuntimeInstanceId: new("runtime"), SessionId: new("session"),
		}.Build(),
		Selection: extensionpb.ModelSelection_builder{
			ProviderId: new("provider"), ModelId: new("model"), ReasoningChoice: new("high"),
		}.Build(),
		Instructions: new(""),
		Messages: []*extensionpb.ConfiguredModelMessage{
			extensionpb.ConfiguredModelMessage_builder{
				Role: new(extensionpb.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_USER), Text: new("question"),
			}.Build(),
			extensionpb.ConfiguredModelMessage_builder{
				Role: new(extensionpb.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_ASSISTANT), Text: new("prior answer"),
			}.Build(),
		},
	}.Build())

	// Act through the production extension request controller.
	prepared, err := service.Prepare(t.Context(), "operation", request)
	require.NoError(t, err)
	completed, err := prepared.Run(t.Context())
	require.NoError(t, err)
	prepared.Release()

	// Assert ordered public content, metadata, and accounting omit private reasoning context.
	require.True(t, released)
	result := completed.GetConfiguredModel()
	require.Len(t, result.GetContent(), 4)
	assert.Equal(t, "answer", result.GetContent()[0].GetText().GetText())
	assert.Equal(t, "refusal", result.GetContent()[1].GetRefusal().GetText())
	assert.Equal(t, "visible reasoning", result.GetContent()[2].GetReasoning().GetText())
	assert.Equal(t, "call", result.GetContent()[3].GetToolCall().GetId())
	assert.Equal(t, "do-not-run", result.GetContent()[3].GetToolCall().GetName())
	var arguments map[string]any
	require.NoError(t, json.Unmarshal(result.GetContent()[3].GetToolCall().GetArgumentsJson(), &arguments))
	assert.Equal(t, map[string]any{"value": "exact"}, arguments)
	assert.Equal(t, extensionpb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_TOOL_USE, result.GetOutcome())
	assert.Equal(t, "terminal detail", result.GetErrorMessage())
	assert.Equal(t, "provider", result.GetProviderId())
	assert.Equal(t, "model", result.GetModelId())
	assert.Equal(t, "reported-model", result.GetResponseModelId())
	assert.Equal(t, "response", result.GetResponseId())
	assert.Equal(t, int64(8), result.GetUsage().GetTotalTokens())
	require.Len(t, result.GetDiagnostics(), 1)
	assert.Equal(t, "complete diagnostic", result.GetDiagnostics()[0].GetMessage())
	assert.NotContains(t, result.String(), "private-api")
	assert.NotContains(t, result.String(), "private-reasoning-context")
}

// TestHiddenAppendAndRecoveryMapExactStoredEntry verifies public session operations preserve metadata and bytes.
func TestHiddenAppendAndRecoveryMapExactStoredEntry(t *testing.T) {
	t.Parallel()

	// Arrange one valid context and a stored entry whose parent is omitted from recovery.
	controller := gomock.NewController(t)
	contexts := NewMockContextOperations(controller)
	runtime := NewMockRuntimeOperations(controller)
	reference := extensiondomain.ContextRef{ID: "context", RuntimeInstanceID: "runtime", SessionID: "session"}
	contexts.EXPECT().ValidateContext("extension", "runtime", reference).Return(nil).Times(2)
	runtime.EXPECT().BeginContextOperation(gomock.Any(), "extension", "runtime").Return(func() {}, nil).Times(2)
	payload := []byte(`{ "escaped": "\u0061" }`)
	stored := session.Entry{
		ID:            "entry",
		ParentID:      mo.Some("foreign-parent"),
		CreatedAt:     time.Unix(7, 8).UTC(),
		Information:   mo.None[session.Information](),
		User:          mo.None[session.UserMessage](),
		Model:         mo.None[session.ModelResponse](),
		EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult:    mo.None[session.ToolResult](),
		Extension: mo.Some(
			session.ExtensionEnvelope{ExtensionID: "extension", EntryType: "checkpoint", Data: payload},
		),
		BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
	contexts.EXPECT().AppendExtension(
		gomock.Any(), "extension", "runtime", reference, "checkpoint", payload,
	).Return(stored, nil)
	contexts.EXPECT().ReadSessionState(gomock.Any(), "extension", "runtime", reference).Return(
		session.ExtensionStateSnapshot{
			SessionID: "session", ActiveLeafID: mo.Some("entry"), Entries: []session.Entry{stored},
		}, nil,
	)
	service := New(contexts, runtime, "extension", "runtime")
	contextRef := extensionpb.ExtensionContextRef_builder{
		ContextId: new("context"), RuntimeInstanceId: new("runtime"), SessionId: new("session"),
	}.Build()
	appendEnvelope := new(extensionpb.ExtensionRequest)
	appendEnvelope.SetAppendExtension(extensionpb.AppendExtensionRequest_builder{
		Context: contextRef, EntryType: new("checkpoint"), Data: payload,
	}.Build())
	stateEnvelope := new(extensionpb.ExtensionRequest)
	stateEnvelope.SetGetSessionState(extensionpb.GetSessionStateRequest_builder{Context: contextRef}.Build())

	// Act through both admitted controller operations.
	appendOperation, err := service.Prepare(t.Context(), "append", appendEnvelope)
	require.NoError(t, err)
	appendResult, err := appendOperation.Run(t.Context())
	require.NoError(t, err)
	appendOperation.Release()
	stateOperation, err := service.Prepare(t.Context(), "state", stateEnvelope)
	require.NoError(t, err)
	stateResult, err := stateOperation.Run(t.Context())
	require.NoError(t, err)
	stateOperation.Release()

	// Assert exact identity, ancestry, timestamp, type, and payload cross the contract.
	appended := appendResult.GetAppendExtension().GetEntry()
	recovered := stateResult.GetGetSessionState().GetEntries()[0]
	assert.Equal(t, "entry", appended.GetId())
	assert.Equal(t, "foreign-parent", recovered.GetParentId())
	assert.Equal(t, stored.CreatedAt, recovered.GetCreatedTime().AsTime())
	assert.Equal(t, "extension", recovered.GetExtensionId())
	assert.Equal(t, "checkpoint", recovered.GetEntryType())
	assert.Equal(t, payload, recovered.GetData())
}

// TestConfiguredModelRequestPreservesClosedFailures verifies every owner category and complete cause reach the SDK.
func TestConfiguredModelRequestPreservesClosedFailures(t *testing.T) {
	t.Parallel()

	for _, code := range []string{
		"MODEL_UNAVAILABLE", "CREDENTIAL_UNAVAILABLE", "MODEL_FAILED", "STALE_CONTEXT", "INTERNAL",
	} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()

			// Arrange one admitted request and a generated classified failure from its context owner.
			controller := gomock.NewController(t)
			contexts := NewMockContextOperations(controller)
			runtime := NewMockRuntimeOperations(controller)
			failure := NewMockContextFailure(controller)
			failure.EXPECT().ContextCode().Return(code)
			failure.EXPECT().Error().Return("complete configured request cause").AnyTimes()
			reference := extensiondomain.ContextRef{ID: "context", RuntimeInstanceID: "runtime", SessionID: "session"}
			contexts.EXPECT().ValidateContext("extension", "runtime", reference).Return(nil)
			runtime.EXPECT().BeginContextOperation(gomock.Any(), "extension", "runtime").Return(func() {}, nil)
			contexts.EXPECT().Request(
				gomock.Any(), "extension", "runtime", reference, gomock.Any(), "", gomock.Any(),
			).Return(model.Response{}, failure)
			service := New(contexts, runtime, "extension", "runtime")
			request := new(extensionpb.ExtensionRequest)
			request.SetConfiguredModel(extensionpb.ConfiguredModelRequest_builder{
				Context: extensionpb.ExtensionContextRef_builder{
					ContextId: new("context"), RuntimeInstanceId: new("runtime"), SessionId: new("session"),
				}.Build(),
				Selection: validConfiguredSelection(), Instructions: new(""), Messages: validConfiguredMessages(),
			}.Build())
			prepared, err := service.Prepare(t.Context(), "operation", request)
			require.NoError(t, err)
			defer prepared.Release()

			// Act through the admitted controller operation.
			_, err = prepared.Run(t.Context())

			// Assert the public code supplements rather than replaces the complete owner error text.
			publicFailure, present := errors.AsType[*extensionsdk.FailureError](err)
			require.True(t, present)
			assert.Equal(t, code, publicFailure.Code())
			assert.Contains(t, err.Error(), "complete configured request cause")
		})
	}
}

// TestConfiguredModelRequestRejectsInvalidTextHistory verifies every required public input boundary.
func TestConfiguredModelRequestRejectsInvalidTextHistory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		// name identifies the invalid field.
		name string
		// selection supplies the request selection.
		selection *extensionpb.ModelSelection
		// messages supplies the request history.
		messages []*extensionpb.ConfiguredModelMessage
		// text identifies the expected complete rejection cause.
		text string
	}{
		{
			name: "missing model", selection: extensionpb.ModelSelection_builder{
				ProviderId: new("provider"), ModelId: new(""), ReasoningChoice: new("off"),
			}.Build(),
			messages: validConfiguredMessages(), text: "complete configured model selection is required",
		},
		{
			name: "empty messages", selection: validConfiguredSelection(), messages: nil,
			text: "requires at least one message",
		},
		{
			name: "unspecified role", selection: validConfiguredSelection(),
			messages: []*extensionpb.ConfiguredModelMessage{extensionpb.ConfiguredModelMessage_builder{
				Role: new(extensionpb.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_UNSPECIFIED), Text: new("text"),
			}.Build()},
			text: "role is unspecified",
		},
		{
			name: "empty text", selection: validConfiguredSelection(),
			messages: []*extensionpb.ConfiguredModelMessage{extensionpb.ConfiguredModelMessage_builder{
				Role: new(extensionpb.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_USER), Text: new(""),
			}.Build()},
			text: "requires nonempty text",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one malformed request without admitting runtime work.
			controller := gomock.NewController(t)
			service := New(
				NewMockContextOperations(controller),
				NewMockRuntimeOperations(controller),
				"extension",
				"runtime",
			)
			request := new(extensionpb.ExtensionRequest)
			request.SetConfiguredModel(extensionpb.ConfiguredModelRequest_builder{
				Context: extensionpb.ExtensionContextRef_builder{
					ContextId: new("context"), RuntimeInstanceId: new("runtime"), SessionId: new("session"),
				}.Build(),
				Selection: test.selection, Instructions: new(""), Messages: test.messages,
			}.Build())

			// Act before Host operation acceptance.
			_, err := service.Prepare(t.Context(), "operation", request)

			// Assert the closed rejection category supplements the exact validation cause.
			var rejection *extensionsdk.RejectionError
			require.ErrorAs(t, err, &rejection)
			assert.Equal(t, "INVALID_ARGUMENT", rejection.Code())
			assert.Contains(t, err.Error(), test.text)
		})
	}
}

// validConfiguredSelection supplies one complete explicit selection.
func validConfiguredSelection() *extensionpb.ModelSelection {
	return extensionpb.ModelSelection_builder{
		ProviderId: new("provider"), ModelId: new("model"), ReasoningChoice: new("off"),
	}.Build()
}

// validConfiguredMessages supplies one valid nonempty user message.
func validConfiguredMessages() []*extensionpb.ConfiguredModelMessage {
	return []*extensionpb.ConfiguredModelMessage{extensionpb.ConfiguredModelMessage_builder{
		Role: new(extensionpb.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_USER), Text: new("text"),
	}.Build()}
}

// TestModelCatalogueMapsCompleteDescriptor verifies full neutral capability and active-selection projection.
func TestModelCatalogueMapsCompleteDescriptor(t *testing.T) {
	t.Parallel()

	// Arrange: provide all neutral descriptor fields through generated consumer-interface mocks.
	controller := gomock.NewController(t)
	contexts := NewMockContextOperations(controller)
	runtime := NewMockRuntimeOperations(controller)
	reference := extensiondomain.ContextRef{ID: "context", RuntimeInstanceID: "runtime", SessionID: "session"}
	contexts.EXPECT().ValidateContext("extension", "runtime", reference).Return(nil)
	released := false
	runtime.EXPECT().BeginContextOperation(gomock.Any(), "extension", "runtime").Return(func() { released = true }, nil)
	descriptor := model.Descriptor{
		Provider:      "provider",
		Model:         "model",
		Input:         []model.InputModality{model.InputModalityText, model.InputModalityImage},
		ContextWindow: 200000,
		MaxTokens:     32000,
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: true,
			Choices:   []model.ReasoningChoice{model.ReasoningChoiceOff, model.ReasoningChoiceHigh},
			Default:   model.ReasoningChoiceHigh,
		},
		ToolCapabilities: model.ToolCapabilities{
			StrictJSONSchema: true,
			Grammar:          model.GrammarCapabilities{Lark: true, Regex: true},
		},
		Pricing: mo.Some(
			model.Pricing{Input: 1.5, Output: 6, CacheRead: 0.25, CacheWrite: 2, Tiers: []model.PricingTier{
				{InputTokensAbove: 100000, Input: 3, Output: 12, CacheRead: 0.5, CacheWrite: 4},
			}},
		),
	}
	contexts.EXPECT().ReadModels(gomock.Any(), "extension", "runtime", reference).Return(ModelCatalog{
		Models: []model.Descriptor{
			descriptor,
		},
		Selection: model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh},
	}, nil)
	service := New(contexts, runtime, "extension", "runtime")
	request := new(extensionpb.ExtensionRequest)
	request.SetGetModels(extensionpb.GetModelsRequest_builder{Context: extensionpb.ExtensionContextRef_builder{
		ContextId: new("context"), RuntimeInstanceId: new("runtime"), SessionId: new("session"),
	}.Build()}.Build())

	// Act: admit, run, and release one catalog read.
	prepared, err := service.Prepare(t.Context(), "operation", request)
	require.NoError(t, err)
	result, err := prepared.Run(t.Context())
	require.NoError(t, err)
	prepared.Release()

	// Assert: every descriptor field and selected value crosses the public contract.
	require.True(t, released)
	catalog := result.GetGetModels()
	require.Len(t, catalog.GetModels(), 1)
	mapped := catalog.GetModels()[0]
	assert.Equal(t, "provider", mapped.GetProviderId())
	assert.Equal(t, "model", mapped.GetModelId())
	assert.Equal(
		t,
		[]extensionpb.InputModality{
			extensionpb.InputModality_INPUT_MODALITY_TEXT,
			extensionpb.InputModality_INPUT_MODALITY_IMAGE,
		},
		mapped.GetInputModalities(),
	)
	assert.Equal(t, int64(200000), mapped.GetContextWindow())
	assert.Equal(t, int64(32000), mapped.GetMaxTokens())
	assert.True(t, mapped.GetReasoning().GetSupported())
	assert.Equal(t, []string{"off", "high"}, mapped.GetReasoning().GetChoices())
	assert.Equal(t, "high", mapped.GetReasoning().GetDefaultChoice())
	assert.True(t, mapped.GetTools().GetStrictJsonSchema())
	assert.True(t, mapped.GetTools().GetLark())
	assert.True(t, mapped.GetTools().GetRegex())
	assert.Equal(t, 1.5, mapped.GetPricing().GetInput())
	assert.Equal(t, 6.0, mapped.GetPricing().GetOutput())
	assert.Equal(t, 0.25, mapped.GetPricing().GetCacheRead())
	assert.Equal(t, 2.0, mapped.GetPricing().GetCacheWrite())
	require.Len(t, mapped.GetPricing().GetTiers(), 1)
	tier := mapped.GetPricing().GetTiers()[0]
	assert.Equal(t, int64(100000), tier.GetInputTokensAbove())
	assert.Equal(t, 3.0, tier.GetInput())
	assert.Equal(t, 12.0, tier.GetOutput())
	assert.Equal(t, 0.5, tier.GetCacheRead())
	assert.Equal(t, 4.0, tier.GetCacheWrite())
	assert.Equal(t, "provider", catalog.GetActiveSelection().GetProviderId())
	assert.Equal(t, "model", catalog.GetActiveSelection().GetModelId())
	assert.Equal(t, "high", catalog.GetActiveSelection().GetReasoningChoice())
}
