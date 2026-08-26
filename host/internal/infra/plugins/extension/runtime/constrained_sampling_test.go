package runtime

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestValidateCatalogMapsConstrainedSampling verifies Host-owned contract validation and input mapping.
func TestValidateCatalogMapsConstrainedSampling(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		descriptor    *extensionpb.ToolDescriptor
		expected      mo.Option[tool.ConstrainedSampling]
		errorContains string
	}{
		"constraint absent": {
			descriptor:    constrainedProtoDescriptor(validSchemaJSON, nil),
			expected:      mo.None[tool.ConstrainedSampling](),
			errorContains: "",
		},
		"strict prefer": {
			//nolint:exhaustruct // extensionpb.ConstrainedSampling_builder sets only the active JsonSchema field.
			descriptor: constrainedProtoDescriptor(validSchemaJSON, extensionpb.ConstrainedSampling_builder{
				JsonSchema: extensionpb.JsonSchemaConstrainedSampling_builder{
					Strictness: new(extensionpb.JsonSchemaStrictness_JSON_SCHEMA_STRICTNESS_PREFER),
				}.Build(),
			}.Build()),
			expected: mo.Some(tool.ConstrainedSampling{
				Kind:                 tool.ConstrainedSamplingJSONSchema,
				JSONSchemaStrictness: mo.Some(tool.JSONSchemaStrictPrefer),
				Grammar:              mo.None[tool.GrammarVariants](),
				GrammarInputProperty: mo.None[string](),
			}),
			errorContains: "",
		},
		"strict require": {
			//nolint:exhaustruct // extensionpb.ConstrainedSampling_builder sets only the active JsonSchema field.
			descriptor: constrainedProtoDescriptor(validSchemaJSON, extensionpb.ConstrainedSampling_builder{
				JsonSchema: extensionpb.JsonSchemaConstrainedSampling_builder{
					Strictness: new(extensionpb.JsonSchemaStrictness_JSON_SCHEMA_STRICTNESS_REQUIRE),
				}.Build(),
			}.Build()),
			expected: mo.Some(tool.ConstrainedSampling{
				Kind:                 tool.ConstrainedSamplingJSONSchema,
				JSONSchemaStrictness: mo.Some(tool.JSONSchemaStrictRequire),
				Grammar:              mo.None[tool.GrammarVariants](),
				GrammarInputProperty: mo.None[string](),
			}),
			errorContains: "",
		},
		"strictness is required": {
			//nolint:exhaustruct // extensionpb.ConstrainedSampling_builder sets only the active JsonSchema field.
			descriptor: constrainedProtoDescriptor(validSchemaJSON, extensionpb.ConstrainedSampling_builder{
				JsonSchema: &extensionpb.JsonSchemaConstrainedSampling{},
			}.Build()),
			errorContains: "strictness is unspecified",
			expected:      mo.None[tool.ConstrainedSampling](),
		},
		"strictness rejects unknown enum": {
			//nolint:exhaustruct // extensionpb.ConstrainedSampling_builder sets only the active JsonSchema field.
			descriptor: constrainedProtoDescriptor(validSchemaJSON, extensionpb.ConstrainedSampling_builder{
				JsonSchema: extensionpb.JsonSchemaConstrainedSampling_builder{
					Strictness: new(extensionpb.JsonSchemaStrictness(99)),
				}.Build(),
			}.Build()),
			errorContains: "strictness is invalid",
			expected:      mo.None[tool.ConstrainedSampling](),
		},
		"constraint config is required": {
			descriptor:    constrainedProtoDescriptor(validSchemaJSON, &extensionpb.ConstrainedSampling{}),
			errorContains: "config is missing",
			expected:      mo.None[tool.ConstrainedSampling](),
		},
		"grammar retains input property": {
			//nolint:exhaustruct // extensionpb.ConstrainedSampling_builder sets only the active Grammar field.
			descriptor: constrainedProtoDescriptor(validSchemaJSON, extensionpb.ConstrainedSampling_builder{
				Grammar: extensionpb.GrammarConstrainedSampling_builder{
					Lark:  new("start: /[a-z]+/"),
					Regex: new("[a-z]+"),
				}.Build(),
			}.Build()),
			expected: mo.Some(tool.ConstrainedSampling{
				Kind:                 tool.ConstrainedSamplingGrammar,
				JSONSchemaStrictness: mo.None[tool.JSONSchemaStrictness](),
				Grammar: mo.Some(tool.GrammarVariants{
					Lark:  mo.Some("start: /[a-z]+/"),
					Regex: mo.Some("[a-z]+"),
				}),
				GrammarInputProperty: mo.Some("path"),
			}),
			errorContains: "",
		},
		"grammar preserves independent variant absence": {
			//nolint:exhaustruct // extensionpb.ConstrainedSampling_builder sets only the active Grammar field.
			descriptor: constrainedProtoDescriptor(validSchemaJSON, extensionpb.ConstrainedSampling_builder{
				Grammar: extensionpb.GrammarConstrainedSampling_builder{
					Regex: new("[a-z]+"),
					Lark:  nil,
				}.Build(),
			}.Build()),
			expected: mo.Some(tool.ConstrainedSampling{
				Kind:                 tool.ConstrainedSamplingGrammar,
				JSONSchemaStrictness: mo.None[tool.JSONSchemaStrictness](),
				Grammar: mo.Some(tool.GrammarVariants{
					Lark:  mo.None[string](),
					Regex: mo.Some("[a-z]+"),
				}),
				GrammarInputProperty: mo.Some("path"),
			}),
			errorContains: "",
		},
		"grammar rejects missing property type": {
			descriptor: constrainedProtoDescriptor(
				`{"type":"object","properties":{"path":{}},"required":["path"],"additionalProperties":false}`,
				grammarProtoConstraint(),
			),
			errorContains: "exactly one required string property",
			expected:      mo.None[tool.ConstrainedSampling](),
		},
		"grammar rejects integer property": {
			descriptor: constrainedProtoDescriptor(
				`{"type":"object","properties":{"path":{"type":"integer"}},"required":["path"],"additionalProperties":false}`,
				grammarProtoConstraint(),
			),
			errorContains: "exactly one required string property",
			expected:      mo.None[tool.ConstrainedSampling](),
		},
		"grammar rejects number property": {
			descriptor: constrainedProtoDescriptor(
				`{"type":"object","properties":{"path":{"type":"number"}},"required":["path"],"additionalProperties":false}`,
				grammarProtoConstraint(),
			),
			errorContains: "exactly one required string property",
			expected:      mo.None[tool.ConstrainedSampling](),
		},
		"grammar rejects boolean property": {
			descriptor: constrainedProtoDescriptor(
				`{"type":"object","properties":{"path":{"type":"boolean"}},"required":["path"],"additionalProperties":false}`,
				grammarProtoConstraint(),
			),
			errorContains: "exactly one required string property",
			expected:      mo.None[tool.ConstrainedSampling](),
		},
		"grammar rejects null property": {
			descriptor: constrainedProtoDescriptor(
				`{"type":"object","properties":{"path":{"type":"null"}},"required":["path"],"additionalProperties":false}`,
				grammarProtoConstraint(),
			),
			errorContains: "exactly one required string property",
			expected:      mo.None[tool.ConstrainedSampling](),
		},
		"grammar rejects object property": {
			descriptor: constrainedProtoDescriptor(
				`{"type":"object","properties":{"path":{"type":"object"}},"required":["path"],"additionalProperties":false}`,
				grammarProtoConstraint(),
			),
			errorContains: "exactly one required string property",
			expected:      mo.None[tool.ConstrainedSampling](),
		},
		"grammar rejects array property": {
			descriptor: constrainedProtoDescriptor(
				`{"type":"object","properties":{"path":{"type":"array"}},"required":["path"],"additionalProperties":false}`,
				grammarProtoConstraint(),
			),
			errorContains: "exactly one required string property",
			expected:      mo.None[tool.ConstrainedSampling](),
		},
		"grammar rejects union property including string": {
			descriptor: constrainedProtoDescriptor(
				`{"type":"object","properties":{"path":{"type":["string","null"]}},"required":["path"],"additionalProperties":false}`,
				grammarProtoConstraint(),
			),
			errorContains: "exactly one required string property",
			expected:      mo.None[tool.ConstrainedSampling](),
		},
		"grammar rejects multiple properties": {
			descriptor: constrainedProtoDescriptor(
				`{"type":"object","properties":{"path":{"type":"string","description":"Path."},"query":{"type":"string","description":"Query."}},"required":["path","query"],"additionalProperties":false}`,
				//nolint:exhaustruct // extensionpb.ConstrainedSampling_builder sets only the active Grammar field.
				extensionpb.ConstrainedSampling_builder{
					Grammar: extensionpb.GrammarConstrainedSampling_builder{
						Regex: new(".+"),
						Lark:  nil,
					}.Build(),
				}.Build(),
			),
			errorContains: "exactly one required string property",
			expected:      mo.None[tool.ConstrainedSampling](),
		},
		"grammar rejects empty variants": {
			//nolint:exhaustruct // extensionpb.ConstrainedSampling_builder sets only the active Grammar field.
			descriptor: constrainedProtoDescriptor(validSchemaJSON, extensionpb.ConstrainedSampling_builder{
				Grammar: &extensionpb.GrammarConstrainedSampling{},
			}.Build()),
			errorContains: "nonempty grammar variant",
			expected:      mo.None[tool.ConstrainedSampling](),
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tools, _, err := validateCatalog(extensionpb.ListToolsResponse_builder{
				Tools: []*extensionpb.ToolDescriptor{testCase.descriptor},
			}.Build())
			if testCase.errorContains != "" {
				require.ErrorContains(t, err, testCase.errorContains)
				return
			}
			require.NoError(t, err)
			require.Len(t, tools, 1)
			assert.Equal(t, testCase.expected, tools[0].ConstrainedSampling)
		})
	}
}

// grammarProtoConstraint builds one valid Regex grammar for schema validation cases.
func grammarProtoConstraint() *extensionpb.ConstrainedSampling {
	return extensionpb.ConstrainedSampling_builder{
		JsonSchema: nil,
		Grammar: extensionpb.GrammarConstrainedSampling_builder{
			Regex: new(".+"),
			Lark:  nil,
		}.Build(),
	}.Build()
}

func constrainedProtoDescriptor(schema string, constraint *extensionpb.ConstrainedSampling) *extensionpb.ToolDescriptor {
	return extensionpb.ToolDescriptor_builder{
		Name:                new("sample"),
		Description:         new("Sample."),
		InputSchemaJson:     []byte(schema),
		ConstrainedSampling: constraint,
	}.Build()
}
