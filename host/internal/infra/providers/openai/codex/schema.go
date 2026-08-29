package codex

import (
	"fmt"
	"strings"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// validateCodexConstraint rejects malformed constraint variants at the provider request boundary.
func validateCodexConstraint(schema map[string]any, constraint tool.ConstrainedSampling, toolName string) error {
	switch constraint.Kind {
	case tool.ConstrainedSamplingJSONSchema:
		strictness, present := constraint.JSONSchemaStrictness.Get()
		if constraint.Grammar.IsSome() || constraint.GrammarInputProperty.IsSome() {
			return fmt.Errorf("tool %q has inconsistent JSON Schema constraint options", toolName)
		}
		if !present {
			return fmt.Errorf("tool %q has invalid JSON Schema strictness", toolName)
		}
		switch strictness {
		case tool.JSONSchemaStrictPrefer, tool.JSONSchemaStrictRequire:
			return nil
		default:
			return fmt.Errorf("tool %q has invalid JSON Schema strictness", toolName)
		}
	case tool.ConstrainedSamplingGrammar:
		grammar, hasGrammar := constraint.Grammar.Get()
		property, hasProperty := constraint.GrammarInputProperty.Get()
		if constraint.JSONSchemaStrictness.IsSome() {
			return fmt.Errorf("tool %q has inconsistent grammar constraint options", toolName)
		}
		if !hasGrammar {
			return fmt.Errorf(
				"tool %q requires grammar constrained sampling, but no supported grammar variant was provided",
				toolName,
			)
		}
		if !hasProperty || property == "" {
			return fmt.Errorf(
				"tool %q requires grammar constrained sampling, but no grammar input property was provided",
				toolName,
			)
		}
		lark, hasLark := grammar.Lark.Get()
		regex, hasRegex := grammar.Regex.Get()
		if (!hasLark || strings.TrimSpace(lark) == "") && (!hasRegex || strings.TrimSpace(regex) == "") {
			return fmt.Errorf(
				"tool %q requires grammar constrained sampling, but no supported grammar variant was provided",
				toolName,
			)
		}
		return validateCodexGrammarSchema(schema, property, toolName)
	default:
		return fmt.Errorf("tool %q has invalid constrained sampling kind", toolName)
	}
}

// validateCodexGrammarSchema checks the single direct string property used as custom tool input.
func validateCodexGrammarSchema(schema map[string]any, property, toolName string) error {
	const rule = "grammar schema must have exactly one required string property"

	properties, hasProperties := schema["properties"].(map[string]any)
	required, hasRequired := schema["required"].([]any)
	if !hasProperties || len(properties) != 1 || !hasRequired || len(required) != 1 {
		return fmt.Errorf("tool %q %s", toolName, rule)
	}
	requiredProperty, isString := required[0].(string)
	if !isString || requiredProperty == "" || requiredProperty != property {
		return fmt.Errorf("tool %q %s", toolName, rule)
	}
	propertySchema, exists := properties[property].(map[string]any)
	if !exists {
		return fmt.Errorf("tool %q %s", toolName, rule)
	}
	propertyType, isString := propertySchema["type"].(string)
	if !isString || propertyType != "string" {
		return fmt.Errorf("tool %q %s", toolName, rule)
	}
	return nil
}

// codexStrict selects provider strictness without changing the Glyph-owned schema.
func codexStrict(
	schema map[string]any,
	constraintOption mo.Option[tool.ConstrainedSampling],
	capabilities toolCapabilities,
	toolName string,
) (bool, error) {
	compatible := codexStrictSchemaCompatible(schema)
	strict := capabilities.strict && compatible
	constraint, constrained := constraintOption.Get()
	if !constrained {
		return strict, nil
	}
	if constraint.Kind != tool.ConstrainedSamplingJSONSchema {
		return false, fmt.Errorf("tool %q has invalid constrained sampling kind", toolName)
	}
	strictness, present := constraint.JSONSchemaStrictness.Get()
	if !present {
		return false, fmt.Errorf("tool %q has invalid JSON Schema strictness", toolName)
	}
	switch strictness {
	case tool.JSONSchemaStrictPrefer:
		return strict, nil
	case tool.JSONSchemaStrictRequire:
		if !capabilities.strict {
			return false, fmt.Errorf(
				"tool %q requires JSON Schema constrained sampling, but the selected Codex model does not support it",
				toolName,
			)
		}
		if !compatible {
			return false, fmt.Errorf(
				"tool %q requires JSON Schema constrained sampling, "+
					"but its input schema is not compatible with Codex strict JSON Schema",
				toolName,
			)
		}
		return true, nil
	default:
		return false, fmt.Errorf("tool %q has invalid JSON Schema strictness", toolName)
	}
}

// codexStrictSchemaCompatible checks every object nested in a JSON Schema for Codex strict requirements.
func codexStrictSchemaCompatible(value any) bool {
	switch schema := value.(type) {
	case map[string]any:
		if codexObjectSchema(schema) && !codexStrictObjectSchema(schema) {
			return false
		}
		for _, child := range schema {
			if !codexStrictSchemaCompatible(child) {
				return false
			}
		}
	case []any:
		for _, child := range schema {
			if !codexStrictSchemaCompatible(child) {
				return false
			}
		}
	}
	return true
}

// codexObjectSchema reports whether a schema node declares object-specific keywords.
func codexObjectSchema(schema map[string]any) bool {
	typeName, isTyped := schema["type"].(string)
	_, hasProperties := schema["properties"]
	_, hasAdditionalProperties := schema["additionalProperties"]
	return isTyped && typeName == "object" || hasProperties || hasAdditionalProperties
}

// codexStrictObjectSchema checks that an object meets Codex strict requirements.
func codexStrictObjectSchema(schema map[string]any) bool {
	additionalProperties, hasAdditionalProperties := schema["additionalProperties"].(bool)
	if !hasAdditionalProperties || additionalProperties {
		return false
	}
	properties, hasProperties := schema["properties"].(map[string]any)
	if !hasProperties {
		return false
	}
	required, hasRequired := schema["required"].([]any)
	if !hasRequired || len(required) != len(properties) {
		return false
	}
	seen := make(map[string]struct{}, len(required))
	for _, name := range required {
		propertyName, isString := name.(string)
		if !isString {
			return false
		}
		if _, duplicate := seen[propertyName]; duplicate {
			return false
		}
		if _, isProperty := properties[propertyName]; !isProperty {
			return false
		}
		seen[propertyName] = struct{}{}
	}
	return true
}

// grammarInputProperties indexes custom input properties for request replay and stream conversion.
func grammarInputProperties(descriptors []tool.Descriptor) map[string]string {
	properties := make(map[string]string)
	for index := range descriptors {
		descriptor := &descriptors[index]
		constraint, constrained := descriptor.ConstrainedSampling.Get()
		if !constrained || constraint.Kind != tool.ConstrainedSamplingGrammar {
			continue
		}
		property, present := constraint.GrammarInputProperty.Get()
		if present {
			properties[descriptor.Name] = property
		}
	}
	return properties
}

// preferredGrammar selects the first model-supported nonempty format in provider preference order.
func preferredGrammar(variants tool.GrammarVariants, capabilities toolCapabilities) (definition, syntax string) {
	if lark, ok := variants.Lark.Get(); capabilities.lark && ok && strings.TrimSpace(lark) != "" {
		return lark, "lark"
	}
	if regex, ok := variants.Regex.Get(); capabilities.regex && ok && strings.TrimSpace(regex) != "" {
		return regex, "regex"
	}
	return "", ""
}
