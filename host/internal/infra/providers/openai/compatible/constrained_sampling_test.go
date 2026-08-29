package compatible

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

func TestCompatibleToolSerializationPreservesUnconstrainedAndPreferredFallback(t *testing.T) {
	t.Parallel()

	unconstrained := compatibleToolDescriptor(mo.None[tool.ConstrainedSampling]())
	preferred := compatibleToolDescriptor(mo.Some(tool.ConstrainedSampling{
		Kind:                 tool.ConstrainedSamplingJSONSchema,
		JSONSchemaStrictness: mo.Some(tool.JSONSchemaStrictPrefer),
		Grammar:              mo.None[tool.GrammarVariants](),
		GrammarInputProperty: mo.None[string](),
	}))

	chatUnconstrained, err := chatTools([]tool.Descriptor{unconstrained}, false)
	require.NoError(t, err)
	chatPreferred, err := chatTools([]tool.Descriptor{preferred}, false)
	require.NoError(t, err)
	assert.Equal(t, mustJSON(t, chatUnconstrained), mustJSON(t, chatPreferred))

	responsesUnconstrained, err := responsesTools([]tool.Descriptor{unconstrained}, false)
	require.NoError(t, err)
	responsesPreferred, err := responsesTools([]tool.Descriptor{preferred}, false)
	require.NoError(t, err)
	assert.Equal(t, mustJSON(t, responsesUnconstrained), mustJSON(t, responsesPreferred))
}

func TestCompatibleToolBuildersRejectUnsupportedAndInconsistentConstraints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		constraint tool.ConstrainedSampling
	}{
		{
			name: "required JSON Schema without strict capability",
			constraint: tool.ConstrainedSampling{
				Kind:                 tool.ConstrainedSamplingJSONSchema,
				JSONSchemaStrictness: mo.Some(tool.JSONSchemaStrictRequire),
				Grammar:              mo.None[tool.GrammarVariants](),
				GrammarInputProperty: mo.None[string](),
			},
		},
		{
			name: "grammar without grammar capability",
			constraint: tool.ConstrainedSampling{
				Kind:                 tool.ConstrainedSamplingGrammar,
				JSONSchemaStrictness: mo.None[tool.JSONSchemaStrictness](),
				Grammar: mo.Some(tool.GrammarVariants{
					Lark:  mo.Some("start: value"),
					Regex: mo.None[string](),
				}),
				GrammarInputProperty: mo.Some("value"),
			},
		},
		{
			name: "JSON Schema missing strictness",
			constraint: tool.ConstrainedSampling{
				Kind:                 tool.ConstrainedSamplingJSONSchema,
				JSONSchemaStrictness: mo.None[tool.JSONSchemaStrictness](),
				Grammar:              mo.None[tool.GrammarVariants](),
				GrammarInputProperty: mo.None[string](),
			},
		},
		{
			name: "JSON Schema with inactive grammar option",
			constraint: tool.ConstrainedSampling{
				Kind:                 tool.ConstrainedSamplingJSONSchema,
				JSONSchemaStrictness: mo.Some(tool.JSONSchemaStrictPrefer),
				Grammar: mo.Some(tool.GrammarVariants{
					Lark:  mo.Some("start: value"),
					Regex: mo.None[string](),
				}),
				GrammarInputProperty: mo.None[string](),
			},
		},
		{
			name: "grammar missing input property",
			constraint: tool.ConstrainedSampling{
				Kind:                 tool.ConstrainedSamplingGrammar,
				JSONSchemaStrictness: mo.None[tool.JSONSchemaStrictness](),
				Grammar: mo.Some(tool.GrammarVariants{
					Lark:  mo.Some("start: value"),
					Regex: mo.None[string](),
				}),
				GrammarInputProperty: mo.None[string](),
			},
		},
		{
			name: "grammar with inactive JSON Schema option",
			constraint: tool.ConstrainedSampling{
				Kind:                 tool.ConstrainedSamplingGrammar,
				JSONSchemaStrictness: mo.Some(tool.JSONSchemaStrictPrefer),
				Grammar: mo.Some(tool.GrammarVariants{
					Lark:  mo.Some("start: value"),
					Regex: mo.None[string](),
				}),
				GrammarInputProperty: mo.Some("value"),
			},
		},
		{
			name: "grammar with empty definitions",
			constraint: tool.ConstrainedSampling{
				Kind:                 tool.ConstrainedSamplingGrammar,
				JSONSchemaStrictness: mo.None[tool.JSONSchemaStrictness](),
				Grammar: mo.Some(tool.GrammarVariants{
					Lark:  mo.Some(""),
					Regex: mo.None[string](),
				}),
				GrammarInputProperty: mo.Some("value"),
			},
		},
		{
			name: "unknown kind",
			constraint: tool.ConstrainedSampling{
				Kind:                 tool.ConstrainedSamplingKind(99),
				JSONSchemaStrictness: mo.None[tool.JSONSchemaStrictness](),
				Grammar:              mo.None[tool.GrammarVariants](),
				GrammarInputProperty: mo.None[string](),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			descriptor := compatibleToolDescriptor(mo.Some(test.constraint))
			_, chatErr := chatTools([]tool.Descriptor{descriptor}, false)
			_, responsesErr := responsesTools([]tool.Descriptor{descriptor}, false)
			require.Error(t, chatErr)
			require.Error(t, responsesErr)
		})
	}
}

func TestCompatibleConstraintsFailBeforeHTTPDispatch(t *testing.T) {
	t.Parallel()

	constraints := []tool.ConstrainedSampling{
		{
			Kind:                 tool.ConstrainedSamplingJSONSchema,
			JSONSchemaStrictness: mo.Some(tool.JSONSchemaStrictRequire),
			Grammar:              mo.None[tool.GrammarVariants](),
			GrammarInputProperty: mo.None[string](),
		},
		{
			Kind:                 tool.ConstrainedSamplingGrammar,
			JSONSchemaStrictness: mo.None[tool.JSONSchemaStrictness](),
			Grammar: mo.Some(tool.GrammarVariants{
				Lark:  mo.Some("start: value"),
				Regex: mo.None[string](),
			}),
			GrammarInputProperty: mo.Some("value"),
		},
	}

	for _, api := range []API{APIChatCompletions, APIResponses} {
		for _, constraint := range constraints {
			t.Run(string(api), func(t *testing.T) {
				t.Parallel()
				var dispatches atomic.Int64
				server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					dispatches.Add(1)
				}))
				t.Cleanup(server.Close)
				service, err := New(Config{
					ProviderID: "local", BaseURL: server.URL, API: api,
					Models: map[model.ID]API{"demo": api}, APIKey: expectAPIKey(t, "", nil, 1),
					ReasoningFormats: nil, ReasoningCompatibilityKeys: nil,
				})
				require.NoError(t, err)
				request := richRequest("local", "demo")
				request.Tools[0].ConstrainedSampling = mo.Some(constraint)
				err = service.Stream(t.Context(), request, func(run.StreamEvent) error { return nil })
				require.Error(t, err)
				assert.Zero(t, dispatches.Load())
			})
		}
	}
}

func compatibleToolDescriptor(constraint mo.Option[tool.ConstrainedSampling]) tool.Descriptor {
	return tool.Descriptor{
		Name:                "sample",
		Description:         "Sample a value",
		InputSchemaJSON:     []byte(`{"type":"object"}`),
		ConstrainedSampling: constraint,
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}
