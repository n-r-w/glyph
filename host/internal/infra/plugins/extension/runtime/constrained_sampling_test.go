package runtime

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestValidateCatalogGrammarParserErrorsRetainContext verifies catalog errors expose tool, rule, and parser contexts.
func TestValidateCatalogGrammarParserErrorsRetainContext(t *testing.T) {
	t.Parallel()

	const grammarRule = "grammar schema must have exactly one required string property"
	testCases := map[string]struct {
		schemaJSON   string
		parserDetail string
	}{
		"boolean property schema": {
			schemaJSON:   `{"type":"object","properties":{"path":true},"required":["path"],"additionalProperties":false}`,
			parserDetail: "unmarshal JSON boolean",
		},
		"property type array": {
			schemaJSON:   `{"type":"object","properties":{"path":{"type":["string"]}},"required":["path"],"additionalProperties":false}`,
			parserDetail: "unmarshal JSON array into Go string",
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange a complete Draft 2020-12 schema that reaches grammar decoding.
			descriptor := constrainedProtoDescriptor(testCase.schemaJSON, grammarProtoConstraint())

			// Act: validate the schema through the extension catalog boundary.
			_, _, err := validateRegistration(extensionpb.RegisterResponse_builder{
				Tools: []*extensionpb.ToolDescriptor{descriptor}, Handlers: nil,
			}.Build())

			// Assert: the startup error keeps tool, grammar rule, and JSON parser contexts.
			require.ErrorContains(t, err, `tool "sample" constrained sampling`)
			require.ErrorContains(t, err, grammarRule)
			require.ErrorContains(t, err, testCase.parserDetail)
		})
	}
}

// TestValidateCatalogRootTypeParserErrorRetainsContext verifies catalog errors expose tool, root rule, and parser contexts.
func TestValidateCatalogRootTypeParserErrorRetainsContext(t *testing.T) {
	t.Parallel()

	// Arrange a complete schema whose root type is raw JSON but not a JSON string.
	descriptor := constrainedProtoDescriptor(`{"type":{}}`, nil)

	// Act: validate the schema through the extension catalog boundary.
	_, _, err := validateRegistration(extensionpb.RegisterResponse_builder{
		Tools: []*extensionpb.ToolDescriptor{descriptor}, Handlers: nil,
	}.Build())

	// Assert: the startup error keeps tool, schema rule, and JSON parser contexts.
	require.ErrorContains(t, err, `tool "sample" input schema`)
	require.ErrorContains(t, err, "schema root type must be object")
	require.ErrorContains(t, err, "unmarshal JSON object into Go string")
}

// TestValidateCatalogMissingTypesRemainSemanticErrors verifies absent types do not create synthetic parser failures.
func TestValidateCatalogMissingTypesRemainSemanticErrors(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		descriptor   *extensionpb.ToolDescriptor
		semanticRule string
	}{
		"grammar property type": {
			descriptor: constrainedProtoDescriptor(
				`{"type":"object","properties":{"path":{}},"required":["path"],"additionalProperties":false}`,
				grammarProtoConstraint(),
			),
			semanticRule: "grammar schema must have exactly one required string property",
		},
		"root type": {
			descriptor:   constrainedProtoDescriptor(`{}`, nil),
			semanticRule: "schema root type must be object",
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange a complete schema whose applicable type field is absent.
			response := extensionpb.RegisterResponse_builder{
				Tools: []*extensionpb.ToolDescriptor{testCase.descriptor}, Handlers: nil,
			}.Build()

			// Act: validate the schema through the extension catalog boundary.
			_, _, err := validateRegistration(response)

			// Assert: the error reports only the semantic rule for the absent type.
			require.ErrorContains(t, err, testCase.semanticRule)
			assert.NotContains(t, err.Error(), "unexpected EOF")
			assert.NotContains(t, err.Error(), "parse property type JSON")
			assert.NotContains(t, err.Error(), "parse root type JSON")
		})
	}
}

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
			//nolint:exhaustruct_v5 // extensionpb.ConstrainedSampling_builder sets only the active JsonSchema field.
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
			//nolint:exhaustruct_v5 // extensionpb.ConstrainedSampling_builder sets only the active JsonSchema field.
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
			//nolint:exhaustruct_v5 // extensionpb.ConstrainedSampling_builder sets only the active JsonSchema field.
			descriptor: constrainedProtoDescriptor(validSchemaJSON, extensionpb.ConstrainedSampling_builder{
				JsonSchema: &extensionpb.JsonSchemaConstrainedSampling{},
			}.Build()),
			errorContains: "strictness is unspecified",
			expected:      mo.None[tool.ConstrainedSampling](),
		},
		"strictness rejects unknown enum": {
			//nolint:exhaustruct_v5 // extensionpb.ConstrainedSampling_builder sets only the active JsonSchema field.
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
			//nolint:exhaustruct_v5 // extensionpb.ConstrainedSampling_builder sets only the active Grammar field.
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
			//nolint:exhaustruct_v5 // extensionpb.ConstrainedSampling_builder sets only the active Grammar field.
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
				//nolint:exhaustruct_v5 // extensionpb.ConstrainedSampling_builder sets only the active Grammar field.
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
			//nolint:exhaustruct_v5 // extensionpb.ConstrainedSampling_builder sets only the active Grammar field.
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
			registration, _, err := validateRegistration(extensionpb.RegisterResponse_builder{
				Tools: []*extensionpb.ToolDescriptor{testCase.descriptor}, Handlers: nil,
			}.Build())
			if testCase.errorContains != "" {
				require.ErrorContains(t, err, testCase.errorContains)
				return
			}
			require.NoError(t, err)
			require.Len(t, registration.Tools, 1)
			assert.Equal(t, testCase.expected, registration.Tools[0].ConstrainedSampling)
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

func constrainedProtoDescriptor(
	schema string,
	constraint *extensionpb.ConstrainedSampling,
) *extensionpb.ToolDescriptor {
	return extensionpb.ToolDescriptor_builder{
		Name:                new("sample"),
		Description:         new("Sample."),
		InputSchemaJson:     []byte(schema),
		ConstrainedSampling: constraint,
	}.Build()
}
