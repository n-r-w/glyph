//nolint:exhaustruct // Tests set only constrained-sampling fields relevant to each case.
package runtime

import (
	"testing"

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
		expected      tool.ConstrainedSampling
		errorContains string
	}{
		"strict prefer": {
			descriptor: constrainedProtoDescriptor(validSchemaJSON, extensionpb.ConstrainedSampling_builder{
				JsonSchema: extensionpb.JsonSchemaConstrainedSampling_builder{
					Strictness: new(extensionpb.JsonSchemaStrictness_JSON_SCHEMA_STRICTNESS_PREFER),
				}.Build(),
			}.Build()),
			expected: tool.ConstrainedSampling{
				Kind: tool.ConstrainedSamplingJSONSchema, JSONSchemaStrictness: tool.JSONSchemaStrictPrefer,
				Grammar: tool.GrammarVariants{}, GrammarInputProperty: "",
			},
		},
		"strict require": {
			descriptor: constrainedProtoDescriptor(validSchemaJSON, extensionpb.ConstrainedSampling_builder{
				JsonSchema: extensionpb.JsonSchemaConstrainedSampling_builder{
					Strictness: new(extensionpb.JsonSchemaStrictness_JSON_SCHEMA_STRICTNESS_REQUIRE),
				}.Build(),
			}.Build()),
			expected: tool.ConstrainedSampling{
				Kind: tool.ConstrainedSamplingJSONSchema, JSONSchemaStrictness: tool.JSONSchemaStrictRequire,
				Grammar: tool.GrammarVariants{}, GrammarInputProperty: "",
			},
		},
		"strictness is required": {
			descriptor: constrainedProtoDescriptor(validSchemaJSON, extensionpb.ConstrainedSampling_builder{
				JsonSchema: &extensionpb.JsonSchemaConstrainedSampling{},
			}.Build()),
			errorContains: "strictness is unspecified",
		},
		"strictness rejects unknown enum": {
			descriptor: constrainedProtoDescriptor(validSchemaJSON, extensionpb.ConstrainedSampling_builder{
				JsonSchema: extensionpb.JsonSchemaConstrainedSampling_builder{
					Strictness: new(extensionpb.JsonSchemaStrictness(99)),
				}.Build(),
			}.Build()),
			errorContains: "strictness is invalid",
		},
		"constraint config is required": {
			descriptor:    constrainedProtoDescriptor(validSchemaJSON, &extensionpb.ConstrainedSampling{}),
			errorContains: "config is missing",
		},
		"grammar retains input property": {
			descriptor: constrainedProtoDescriptor(validSchemaJSON, extensionpb.ConstrainedSampling_builder{
				Grammar: extensionpb.GrammarConstrainedSampling_builder{
					Lark: new("start: /[a-z]+/"), Regex: new("[a-z]+"),
				}.Build(),
			}.Build()),
			expected: tool.ConstrainedSampling{
				Kind: tool.ConstrainedSamplingGrammar, JSONSchemaStrictness: 0,
				Grammar:              tool.GrammarVariants{Lark: "start: /[a-z]+/", Regex: "[a-z]+"},
				GrammarInputProperty: "path",
			},
		},
		"grammar rejects multiple properties": {
			descriptor: constrainedProtoDescriptor(
				`{"type":"object","properties":{"path":{"type":"string","description":"Path."},"query":{"type":"string","description":"Query."}},"required":["path","query"],"additionalProperties":false}`,
				extensionpb.ConstrainedSampling_builder{Grammar: extensionpb.GrammarConstrainedSampling_builder{Regex: new(".+")}.Build()}.Build(),
			),
			errorContains: "exactly one required string property",
		},
		"grammar rejects empty variants": {
			descriptor: constrainedProtoDescriptor(validSchemaJSON, extensionpb.ConstrainedSampling_builder{
				Grammar: &extensionpb.GrammarConstrainedSampling{},
			}.Build()),
			errorContains: "nonempty grammar variant",
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tools, _, err := validateCatalog(extensionpb.ListToolsResponse_builder{Tools: []*extensionpb.ToolDescriptor{testCase.descriptor}}.Build())
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

func constrainedProtoDescriptor(schema string, constraint *extensionpb.ConstrainedSampling) *extensionpb.ToolDescriptor {
	return extensionpb.ToolDescriptor_builder{
		Name: new("sample"), Description: new("Sample."), InputSchemaJson: []byte(schema), ConstrainedSampling: constraint,
	}.Build()
}
