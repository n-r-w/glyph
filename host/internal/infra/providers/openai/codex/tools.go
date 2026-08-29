package codex

import (
	"encoding/json"

	"fmt"

	"github.com/samber/lo"

	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

type toolCapabilities struct {
	// strict reports whether strict JSON Schema generation is available.
	strict bool
	// lark reports whether Lark grammar generation is available.
	lark bool
	// regex reports whether regular expression generation is available.
	regex bool
}

// buildTools maps provider-neutral schemas into Codex tool request types.
func buildTools(descriptors []tool.Descriptor, capabilities toolCapabilities) ([]responses.ToolUnionParam, error) {
	return lo.MapErr(descriptors, func(descriptor tool.Descriptor, _ int) (responses.ToolUnionParam, error) {
		var schema map[string]any
		if err := json.Unmarshal(descriptor.InputSchemaJSON, &schema); err != nil {
			return responses.ToolUnionParam{}, fmt.Errorf("decode schema for Codex tool %q: %w", descriptor.Name, err)
		}
		constraint, constrained := descriptor.ConstrainedSampling.Get()
		if constrained {
			if err := validateCodexConstraint(schema, constraint, descriptor.Name); err != nil {
				return responses.ToolUnionParam{}, err
			}
		}
		if constrained && constraint.Kind == tool.ConstrainedSamplingGrammar {
			if !capabilities.lark && !capabilities.regex {
				return responses.ToolUnionParam{}, fmt.Errorf(
					"tool %q requires grammar constrained sampling, but the selected Codex model does not support it",
					descriptor.Name,
				)
			}
			grammar, present := constraint.Grammar.Get()
			if !present {
				return responses.ToolUnionParam{}, fmt.Errorf(
					"tool %q requires grammar constrained sampling, but no supported grammar variant was provided",
					descriptor.Name,
				)
			}
			property, hasProperty := constraint.GrammarInputProperty.Get()
			if !hasProperty || property == "" {
				return responses.ToolUnionParam{}, fmt.Errorf(
					"tool %q requires grammar constrained sampling, but no grammar input property was provided",
					descriptor.Name,
				)
			}
			definition, syntax := preferredGrammar(grammar, capabilities)
			if definition == "" {
				return responses.ToolUnionParam{}, fmt.Errorf(
					"tool %q requires grammar constrained sampling, but no supported grammar variant was provided",
					descriptor.Name,
				)
			}
			//nolint:exhaustruct_v5 // responses.ToolUnionParam sets only the active OfCustom field.
			return responses.ToolUnionParam{
				OfCustom: &responses.CustomToolParam{
					Name:           descriptor.Name,
					Description:    param.NewOpt(descriptor.Description),
					Format:         shared.CustomToolInputFormatParamOfGrammar(definition, syntax),
					DeferLoading:   param.Opt[bool]{},
					AllowedCallers: nil,
					Type:           "",
				},
			}, nil
		}

		strict, err := codexStrict(schema, descriptor.ConstrainedSampling, capabilities, descriptor.Name)
		if err != nil {
			return responses.ToolUnionParam{}, err
		}
		//nolint:exhaustruct_v5 // responses.ToolUnionParam sets only the active OfFunction field.
		return responses.ToolUnionParam{
			OfFunction: &responses.FunctionToolParam{
				Name:           descriptor.Name,
				Description:    param.NewOpt(descriptor.Description),
				Parameters:     schema,
				Strict:         param.NewOpt(strict),
				DeferLoading:   param.Opt[bool]{},
				AllowedCallers: nil,
				OutputSchema:   nil,
				Type:           "",
			},
		}, nil
	})
}
