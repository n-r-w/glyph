package compatible

import (
	"errors"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// validateConstrainedSampling keeps malformed descriptors and unsupported guarantees out of provider requests.
func validateConstrainedSampling(descriptor tool.Descriptor, strictSupported bool) error {
	constraint, present := descriptor.ConstrainedSampling.Get()
	if !present {
		return nil
	}

	switch constraint.Kind {
	case tool.ConstrainedSamplingJSONSchema:
		return validateJSONSchemaConstraint(constraint, strictSupported)
	case tool.ConstrainedSamplingGrammar:
		return validateGrammarConstraint(constraint)
	default:
		return fmt.Errorf("unknown constrained sampling kind %d", constraint.Kind)
	}
}

func validateJSONSchemaConstraint(constraint tool.ConstrainedSampling, strictSupported bool) error {
	strictness, hasStrictness := constraint.JSONSchemaStrictness.Get()
	if !hasStrictness || constraint.Grammar.IsSome() || constraint.GrammarInputProperty.IsSome() {
		return errors.New("JSON Schema constraint options are inconsistent")
	}
	switch strictness {
	case tool.JSONSchemaStrictPrefer:
		return nil
	case tool.JSONSchemaStrictRequire:
		if !strictSupported {
			return errors.New("strict JSON Schema generation is required but unsupported")
		}
		return nil
	default:
		return fmt.Errorf("unknown JSON Schema strictness %d", strictness)
	}
}

func validateGrammarConstraint(constraint tool.ConstrainedSampling) error {
	grammar, hasGrammar := constraint.Grammar.Get()
	_, hasInputProperty := constraint.GrammarInputProperty.Get()
	if constraint.JSONSchemaStrictness.IsSome() || !hasGrammar || !hasInputProperty {
		return errors.New("grammar constraint options are inconsistent")
	}
	lark, hasLark := grammar.Lark.Get()
	regex, hasRegex := grammar.Regex.Get()
	if (!hasLark || lark == "") && (!hasRegex || regex == "") {
		return errors.New("grammar constraint has no nonempty supported definition")
	}
	return errors.New("grammar constrained generation is unsupported")
}
