package codex

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

const constrainedToolSchema = `{"type":"object","properties":{"payload":{"type":"string","description":"Input text."}},"required":["payload"],"additionalProperties":false}`

// TestBuildToolsMapsConstrainedSampling verifies provider-owned strict and grammar request conversion.
func TestBuildToolsMapsConstrainedSampling(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		descriptor    tool.Descriptor
		capabilities  toolCapabilities
		expected      string
		errorContains string
	}{
		"existing tool remains strict": {
			descriptor:   tool.Descriptor{Name: "sample", Description: "Sample.", InputSchemaJSON: []byte(constrainedToolSchema), ConstrainedSampling: mo.None[tool.ConstrainedSampling]()},
			capabilities: toolCapabilities{strict: true, lark: true, regex: true},
			expected:     `[{"type":"function","name":"sample","description":"Sample.","parameters":{"additionalProperties":false,"properties":{"payload":{"description":"Input text.","type":"string"}},"required":["payload"],"type":"object"},"strict":true}]`, errorContains: "",
		},
		"strict prefer uses supported strict mode": {
			descriptor:   constrainedDescriptor(tool.JSONSchemaStrictPrefer, tool.GrammarVariants{}),
			capabilities: toolCapabilities{strict: true, lark: true, regex: true},
			expected:     `[{"type":"function","name":"sample","description":"Sample.","parameters":{"additionalProperties":false,"properties":{"payload":{"description":"Input text.","type":"string"}},"required":["payload"],"type":"object"},"strict":true}]`, errorContains: "",
		},
		"strict prefer preserves provider fallback": {
			descriptor:   constrainedDescriptor(tool.JSONSchemaStrictPrefer, tool.GrammarVariants{}),
			capabilities: toolCapabilities{strict: false, lark: true, regex: true},
			expected:     `[{"type":"function","name":"sample","description":"Sample.","parameters":{"additionalProperties":false,"properties":{"payload":{"description":"Input text.","type":"string"}},"required":["payload"],"type":"object"},"strict":false}]`, errorContains: "",
		},
		"strict require rejects unsupported mode": {
			descriptor:    constrainedDescriptor(tool.JSONSchemaStrictRequire, tool.GrammarVariants{}),
			capabilities:  toolCapabilities{strict: false, lark: true, regex: true},
			errorContains: "requires JSON Schema constrained sampling", expected: "",
		},
		"grammar chooses lark before regex": {
			descriptor:   constrainedDescriptor(0, tool.GrammarVariants{Lark: mo.Some("start: /[a-z]+/"), Regex: mo.Some("[a-z]+")}),
			capabilities: toolCapabilities{strict: true, lark: true, regex: true},
			expected:     `[{"type":"custom","name":"sample","description":"Sample.","format":{"type":"grammar","syntax":"lark","definition":"start: /[a-z]+/"}}]`, errorContains: "",
		},
		"grammar uses regex when lark is empty": {
			descriptor:   constrainedDescriptor(0, tool.GrammarVariants{Regex: mo.Some("[a-z]+"), Lark: mo.None[string]()}),
			capabilities: toolCapabilities{strict: true, lark: true, regex: true},
			expected:     `[{"type":"custom","name":"sample","description":"Sample.","format":{"type":"grammar","syntax":"regex","definition":"[a-z]+"}}]`, errorContains: "",
		},
		"grammar chooses a format supported by the model": {
			descriptor:   constrainedDescriptor(0, tool.GrammarVariants{Lark: mo.Some("start: /[a-z]+/"), Regex: mo.Some("[a-z]+")}),
			capabilities: toolCapabilities{strict: true, lark: false, regex: true},
			expected:     `[{"type":"custom","name":"sample","description":"Sample.","format":{"type":"grammar","syntax":"regex","definition":"[a-z]+"}}]`, errorContains: "",
		},
		"grammar rejects when offered formats are unsupported": {
			descriptor:    constrainedDescriptor(0, tool.GrammarVariants{Lark: mo.Some("start: /[a-z]+/"), Regex: mo.None[string]()}),
			capabilities:  toolCapabilities{strict: true, lark: false, regex: true},
			errorContains: "no supported grammar variant", expected: "",
		},
		"grammar rejects unsupported mode": {
			descriptor:    constrainedDescriptor(0, tool.GrammarVariants{Regex: mo.Some("[a-z]+"), Lark: mo.None[string]()}),
			capabilities:  toolCapabilities{strict: true, lark: false, regex: false},
			errorContains: "requires grammar constrained sampling", expected: "",
		},
		"grammar requires a nonempty variant": {
			descriptor: tool.Descriptor{
				Name: "sample", Description: "Sample.", InputSchemaJSON: []byte(constrainedToolSchema),
				ConstrainedSampling: mo.Some(tool.ConstrainedSampling{Kind: tool.ConstrainedSamplingGrammar, JSONSchemaStrictness: mo.None[tool.JSONSchemaStrictness](), Grammar: mo.None[tool.GrammarVariants](), GrammarInputProperty: mo.None[string]()}),
			},
			capabilities:  toolCapabilities{strict: true, lark: true, regex: true},
			errorContains: "no supported grammar variant", expected: "",
		},
		"JSON Schema requires valid strictness": {
			descriptor: tool.Descriptor{
				Name: "sample", Description: "Sample.", InputSchemaJSON: []byte(constrainedToolSchema),
				ConstrainedSampling: mo.Some(tool.ConstrainedSampling{Kind: tool.ConstrainedSamplingJSONSchema, JSONSchemaStrictness: mo.None[tool.JSONSchemaStrictness](), Grammar: mo.None[tool.GrammarVariants](), GrammarInputProperty: mo.None[string]()}),
			},
			capabilities:  toolCapabilities{strict: true, lark: true, regex: true},
			errorContains: "invalid JSON Schema strictness", expected: "",
		},
		"constraint kind must be known": {
			descriptor: tool.Descriptor{
				Name: "sample", Description: "Sample.", InputSchemaJSON: []byte(constrainedToolSchema),
				ConstrainedSampling: mo.Some(tool.ConstrainedSampling{Kind: tool.ConstrainedSamplingKind(99), JSONSchemaStrictness: mo.None[tool.JSONSchemaStrictness](), Grammar: mo.None[tool.GrammarVariants](), GrammarInputProperty: mo.None[string]()}),
			},
			capabilities:  toolCapabilities{strict: true, lark: true, regex: true},
			errorContains: "invalid constrained sampling kind", expected: "",
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			params, err := buildTools([]tool.Descriptor{testCase.descriptor}, testCase.capabilities)
			if testCase.errorContains != "" {
				require.ErrorContains(t, err, testCase.errorContains)
				return
			}
			require.NoError(t, err)
			encoded, err := json.Marshal(params)
			require.NoError(t, err)
			assert.JSONEq(t, testCase.expected, string(encoded))
		})
	}
}

// TestBuildToolsMapsStrictSchemaCompatibility keeps unsupported strict schemas out of the provider request.
func TestBuildToolsMapsStrictSchemaCompatibility(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		schema        string
		strictness    tool.JSONSchemaStrictness
		expectStrict  bool
		errorContains string
		unconstrained bool
	}{
		"compatible object remains strict": {
			schema: constrainedToolSchema, strictness: tool.JSONSchemaStrictPrefer, expectStrict: true, errorContains: "", unconstrained: false,
		},
		"compatible nested and array item objects remain strict": {
			schema:       `{"type":"object","properties":{"options":{"type":"object","properties":{"limit":{"type":"integer"}},"required":["limit"],"additionalProperties":false},"ranges":{"type":"array","items":{"type":"object","properties":{"start":{"type":"integer"}},"required":["start"],"additionalProperties":false}}},"required":["options","ranges"],"additionalProperties":false}`,
			strictness:   tool.JSONSchemaStrictPrefer,
			expectStrict: true, errorContains: "", unconstrained: false,
		},
		"preferred optional root falls back": {
			schema:       `{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer"}},"required":["path"],"additionalProperties":false}`,
			strictness:   tool.JSONSchemaStrictPrefer,
			expectStrict: false, errorContains: "", unconstrained: false,
		},
		"unconstrained optional root falls back": {
			schema:        `{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer"}},"required":["path"],"additionalProperties":false}`,
			expectStrict:  false,
			unconstrained: true, strictness: 0, errorContains: "",
		},
		"duplicate required property falls back": {
			schema:       `{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"string"}},"required":["a","a"],"additionalProperties":false}`,
			strictness:   tool.JSONSchemaStrictPrefer,
			expectStrict: false, errorContains: "", unconstrained: false,
		},
		"required optional root rejects locally": {
			schema:        `{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer"}},"required":["path"],"additionalProperties":false}`,
			strictness:    tool.JSONSchemaStrictRequire,
			errorContains: "not compatible with Codex strict JSON Schema", expectStrict: false, unconstrained: false,
		},
		"preferred nested optional object falls back": {
			schema:       `{"type":"object","properties":{"options":{"type":"object","properties":{"limit":{"type":"integer"},"offset":{"type":"integer"}},"required":["limit"],"additionalProperties":false}},"required":["options"],"additionalProperties":false}`,
			strictness:   tool.JSONSchemaStrictPrefer,
			expectStrict: false, errorContains: "", unconstrained: false,
		},
		"preferred array item optional object falls back": {
			schema:       `{"type":"object","properties":{"ranges":{"type":"array","items":{"type":"object","properties":{"start":{"type":"integer"},"end":{"type":"integer"}},"required":["start"],"additionalProperties":false}}},"required":["ranges"],"additionalProperties":false}`,
			strictness:   tool.JSONSchemaStrictPrefer,
			expectStrict: false, errorContains: "", unconstrained: false,
		},
		"preferred object without additional properties restriction falls back": {
			schema:       `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`,
			strictness:   tool.JSONSchemaStrictPrefer,
			expectStrict: false, errorContains: "", unconstrained: false,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			constraint := mo.Some(tool.ConstrainedSampling{
				Kind:                 tool.ConstrainedSamplingJSONSchema,
				JSONSchemaStrictness: mo.Some(testCase.strictness), Grammar: mo.None[tool.GrammarVariants](), GrammarInputProperty: mo.None[string](),
			})
			if testCase.unconstrained {
				constraint = mo.None[tool.ConstrainedSampling]()
			}
			descriptor := tool.Descriptor{
				Name: "sample", Description: "Sample.", InputSchemaJSON: []byte(testCase.schema),
				ConstrainedSampling: constraint,
			}
			params, err := buildTools([]tool.Descriptor{descriptor}, toolCapabilities{strict: true, lark: false, regex: false})
			if testCase.errorContains != "" {
				require.ErrorContains(t, err, testCase.errorContains)
				return
			}
			require.NoError(t, err)

			encoded, err := json.Marshal(params)
			require.NoError(t, err)
			var mapped []struct {
				Parameters map[string]any `json:"parameters"`
				Strict     bool           `json:"strict"`
			}
			require.NoError(t, json.Unmarshal(encoded, &mapped))
			require.Len(t, mapped, 1)
			assert.Equal(t, testCase.expectStrict, mapped[0].Strict)

			var original map[string]any
			require.NoError(t, json.Unmarshal([]byte(testCase.schema), &original))
			assert.Equal(t, original, mapped[0].Parameters)
		})
	}
}

// TestDriverStreamMapsGrammarToolLifecycle verifies custom request, replay, preview, and final arguments.
func TestDriverStreamMapsGrammarToolLifecycle(t *testing.T) {
	t.Parallel()

	accountID := "account-grammar"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(
		testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil,
	)
	interaction := NewMockInteraction(gomock.NewController(t))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if !assert.NoError(t, json.NewDecoder(request.Body).Decode(&body)) {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		tools := body["tools"].([]any)
		if !assert.Len(t, tools, 1) {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.Equal(t, "custom", tools[0].(map[string]any)["type"])
		input := body["input"].([]any)
		if !assert.Len(t, input, 2) {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.Equal(t, "custom_tool_call", input[0].(map[string]any)["type"])
		assert.Equal(t, "old", input[0].(map[string]any)["input"])
		assert.Equal(t, "custom_tool_call_output", input[1].(map[string]any)["type"])
		writeSSE(writer,
			`{"type":"response.output_item.added","output_index":0,"item":{"id":"ctc-1","type":"custom_tool_call","call_id":"call-1","name":"sample","input":"","status":"in_progress"}}`,
			`{"type":"response.custom_tool_call_input.delta","output_index":0,"item_id":"ctc-1","delta":"ab"}`,
			`{"type":"response.output_item.done","output_index":0,"item":{"id":"ctc-1","type":"custom_tool_call","call_id":"call-1","name":"sample","input":"abc","status":"completed"}}`,
			completedEvent(`[]`),
		)
	}))
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))
	descriptor := constrainedDescriptor(0, tool.GrammarVariants{Regex: mo.Some("[a-z]+"), Lark: mo.None[string]()})
	history := []agent.HistoryEntry{
		{Kind: agent.HistoryEntryModel, Model: mo.Some(model.Response{Content: []model.Content{{
			Kind: model.ContentToolCall,
			ToolCall: mo.Some(model.ToolCall{
				ID: "call-old", Name: "sample", Arguments: map[string]any{"payload": "old"},
			}), Text: mo.None[string](), Final: false, ProviderContext: mo.None[model.ProviderContext](),
		}}, Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
		}), User: mo.None[model.Message](),
			ToolResult: mo.None[agent.ToolResult](),
		},
		{Kind: agent.HistoryEntryToolResult, ToolResult: mo.Some(agent.ToolResult{
			CallID: "call-old", ToolName: "sample", Contents: tool.TextContents("done"), IsError: false,
		}), User: mo.None[model.Message](),
			Model: mo.None[model.Response](),
		},
	}
	events := make([]run.StreamEvent, 0)
	err := service.Stream(t.Context(), run.ModelRequest{ReasoningChoice: model.ReasoningChoiceOn,
		Instructions: "test", Model: model.Descriptor{
			Provider: ProviderID, Model: "gpt-test",
			ToolCapabilities: model.ToolCapabilities{Grammar: model.GrammarCapabilities{Regex: true, Lark: false}, StrictJSONSchema: false}, ReasoningCapabilities: model.ReasoningCapabilities{},
		},
		History: history, Tools: []tool.Descriptor{descriptor},
	}, func(event run.StreamEvent) error {
		events = append(events, event)
		return nil
	})

	require.NoError(t, err)
	require.Len(t, events, 4)
	assert.Equal(t, run.StreamEventToolCallStart, events[0].Kind)
	assert.Equal(t, "sample", events[0].Preview.OrEmpty().Name)
	assert.Equal(t, run.StreamEventToolCallDelta, events[1].Kind)
	require.Len(t, events[1].Preview.OrEmpty().Fields, 1)
	assert.Equal(t, model.ToolCallPreviewField{
		Name: "payload", Kind: model.ToolCallPreviewFieldPrefix,
		Value: mo.None[any](), Prefix: mo.Some("ab"),
	}, events[1].Preview.OrEmpty().Fields[0])
	assert.Equal(t, run.StreamEventToolCallEnd, events[2].Kind)
	assert.Equal(t, map[string]any{"payload": "abc"}, events[2].ToolCall.OrEmpty().Arguments)
	assert.Equal(t, run.StreamEventDone, events[3].Kind)
	require.Len(t, events[3].Response.OrEmpty().Content, 1)
	assert.Equal(t, map[string]any{"payload": "abc"}, events[3].Response.OrEmpty().Content[0].ToolCall.OrEmpty().Arguments)
}

// TestDriverStreamDoesNotInferMissingCapabilities verifies omitted support remains unsupported.
func TestDriverStreamDoesNotInferMissingCapabilities(t *testing.T) {
	t.Parallel()

	accountID := "account-missing-capabilities"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(
		testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil,
	)
	interaction := NewMockInteraction(gomock.NewController(t))
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))
	events := make([]run.StreamEvent, 0)
	err := service.Stream(t.Context(), run.ModelRequest{ReasoningChoice: model.ReasoningChoiceOn,
		Instructions: "test", Model: model.Descriptor{Provider: ProviderID, Model: "gpt-unknown", ReasoningCapabilities: model.ReasoningCapabilities{}, ToolCapabilities: model.ToolCapabilities{}},
		Tools: []tool.Descriptor{constrainedDescriptor(tool.JSONSchemaStrictRequire, tool.GrammarVariants{})}, History: nil,
	}, func(event run.StreamEvent) error {
		events = append(events, event)
		return nil
	})

	require.Error(t, err)
	assert.Zero(t, requests)
	require.Len(t, events, 1)
	assert.Equal(t, run.StreamEventError, events[0].Kind)
}

// TestDriverStreamSendsNonStrictPreferredTool verifies unsupported strict preference degrades to a function tool.
func TestDriverStreamSendsNonStrictPreferredTool(t *testing.T) {
	t.Parallel()

	accountID := "account-prefer-fallback"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(
		testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil,
	)
	interaction := NewMockInteraction(gomock.NewController(t))
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		var body map[string]any
		if !assert.NoError(t, json.NewDecoder(request.Body).Decode(&body)) {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		tools := body["tools"].([]any)
		if !assert.Len(t, tools, 1) {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.Equal(t, false, tools[0].(map[string]any)["strict"])
		writeSSE(writer, completedEvent(`[]`))
	}))
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))
	err := service.Stream(t.Context(), run.ModelRequest{ReasoningChoice: model.ReasoningChoiceOn,
		Instructions: "test", Model: model.Descriptor{Provider: ProviderID, Model: "gpt-unknown", ReasoningCapabilities: model.ReasoningCapabilities{}, ToolCapabilities: model.ToolCapabilities{}},
		Tools: []tool.Descriptor{constrainedDescriptor(tool.JSONSchemaStrictPrefer, tool.GrammarVariants{})}, History: nil,
	}, func(run.StreamEvent) error { return nil })

	require.NoError(t, err)
	assert.Equal(t, 1, requests)
}

// TestDriverStreamRejectsUnsupportedGrammarBeforeDispatch verifies format intersection before HTTP dispatch.
func TestDriverStreamRejectsUnsupportedGrammarBeforeDispatch(t *testing.T) {
	t.Parallel()

	accountID := "account-unsupported-grammar"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(
		testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil,
	)
	interaction := NewMockInteraction(gomock.NewController(t))
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	t.Cleanup(server.Close)
	selectedModel := model.Descriptor{
		Provider: ProviderID, Model: "gpt-regex-only",
		ToolCapabilities: model.ToolCapabilities{Grammar: model.GrammarCapabilities{Regex: true, Lark: false}, StrictJSONSchema: false}, ReasoningCapabilities: model.ReasoningCapabilities{},
	}
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))
	events := make([]run.StreamEvent, 0)
	err := service.Stream(t.Context(), run.ModelRequest{ReasoningChoice: model.ReasoningChoiceOn,
		Instructions: "test", Model: selectedModel,
		Tools: []tool.Descriptor{constrainedDescriptor(0, tool.GrammarVariants{Lark: mo.Some("start: /[a-z]+/"), Regex: mo.None[string]()})}, History: nil,
	}, func(event run.StreamEvent) error {
		events = append(events, event)
		return nil
	})

	require.ErrorContains(t, err, "no supported grammar variant")
	assert.Zero(t, requests)
	require.Len(t, events, 1)
	assert.Equal(t, run.StreamEventError, events[0].Kind)
}

// TestDriverStreamRejectsRequiredConstraintBeforeDispatch verifies capability failure has no HTTP side effect.
func TestDriverStreamRejectsRequiredConstraintBeforeDispatch(t *testing.T) {
	t.Parallel()

	accountID := "account-unsupported"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(
		testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil,
	)
	interaction := NewMockInteraction(gomock.NewController(t))
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))
	events := make([]run.StreamEvent, 0)
	err := service.Stream(t.Context(), run.ModelRequest{ReasoningChoice: model.ReasoningChoiceOn,
		Instructions: "test", Model: model.Descriptor{Provider: ProviderID, Model: "gpt-test", ReasoningCapabilities: model.ReasoningCapabilities{}, ToolCapabilities: model.ToolCapabilities{}},
		Tools: []tool.Descriptor{constrainedDescriptor(tool.JSONSchemaStrictRequire, tool.GrammarVariants{})}, History: nil,
	}, func(event run.StreamEvent) error {
		events = append(events, event)
		return nil
	})

	require.Error(t, err)
	assert.Zero(t, requests)
	require.Len(t, events, 1)
	assert.Equal(t, run.StreamEventError, events[0].Kind)
	assert.Equal(t, model.OutcomeFailed, events[0].Response.OrEmpty().Outcome.OrEmpty())
	assert.NotEmpty(t, events[0].Response.OrEmpty().ErrorMessage.OrEmpty())
}

func constrainedDescriptor(strictness tool.JSONSchemaStrictness, variants tool.GrammarVariants) tool.Descriptor {
	constraint := tool.ConstrainedSampling{
		Kind:                 tool.ConstrainedSamplingJSONSchema,
		JSONSchemaStrictness: mo.Some(strictness),
		Grammar:              mo.None[tool.GrammarVariants](),
		GrammarInputProperty: mo.None[string](),
	}
	if variants != (tool.GrammarVariants{}) {
		constraint = tool.ConstrainedSampling{
			Kind:                 tool.ConstrainedSamplingGrammar,
			JSONSchemaStrictness: mo.None[tool.JSONSchemaStrictness](),
			Grammar:              mo.Some(variants),
			GrammarInputProperty: mo.Some("payload"),
		}
	}
	return tool.Descriptor{
		Name: "sample", Description: "Sample.", InputSchemaJSON: []byte(constrainedToolSchema),
		ConstrainedSampling: mo.Some(constraint),
	}
}
