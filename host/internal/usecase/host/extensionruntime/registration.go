package extensionruntime

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// Registration contains only process declarations, never trusted discovered identity.
type Registration struct {
	// Tools preserves descriptor order and absent descriptor payloads.
	Tools []mo.Option[ToolDeclaration]
	// Handlers preserves handler order and absent descriptor payloads.
	Handlers []mo.Option[HandlerDeclaration]
}

// ToolDeclaration contains unvalidated process tool data.
type ToolDeclaration struct {
	// Name is the process-declared local tool name.
	Name string
	// Description is model-visible tool documentation.
	Description string
	// InputSchemaJSON retains exact schema bytes for capability validation.
	InputSchemaJSON []byte
	// Constraint retains optional sampling configuration without applying policy.
	Constraint mo.Option[ConstraintDeclaration]
}

// ConstraintKind identifies the decoded configuration variant.
type ConstraintKind uint8

const (
	// ConstraintMissing reports no selected configuration.
	ConstraintMissing ConstraintKind = iota
	// ConstraintJSONSchema reports the JSON Schema configuration variant.
	ConstraintJSONSchema
	// ConstraintGrammar reports the grammar configuration variant.
	ConstraintGrammar
	// ConstraintInvalid reports an unknown configuration variant.
	ConstraintInvalid
)

// ConstraintDeclaration preserves configuration presence independently from its values.
type ConstraintDeclaration struct {
	// Kind identifies the decoded configuration variant.
	Kind ConstraintKind
	// Present reports whether the selected configuration message exists.
	Present bool
	// Strictness retains the raw JSON Schema enum value.
	Strictness int32
	// Lark retains the optional exact Lark grammar.
	Lark mo.Option[string]
	// Regex retains the optional exact regular expression.
	Regex mo.Option[string]
}

// HandlerDeclaration contains unvalidated process handler data.
type HandlerDeclaration struct {
	// ID is the process-declared extension-local handler identifier.
	ID string
	// Kind retains the raw process handler enum value.
	Kind int32
}

// bindRegistration attaches trusted discovery identity before startup validates capability declarations.
func (s *Service) bindRegistration(candidate Candidate, raw Registration) startup.PendingRegistration {
	tools := make([]startup.RawToolDescriptor, len(raw.Tools))
	for index, descriptor := range raw.Tools {
		value, present := descriptor.Get()
		constraint := mo.None[startup.RawConstrainedSampling]()
		if decoded, ok := value.Constraint.Get(); ok {
			constraint = mo.Some(startup.RawConstrainedSampling{
				Kind:                 startup.RawConstrainedSamplingKind(decoded.Kind),
				JSONSchemaPresent:    decoded.Kind == ConstraintJSONSchema && decoded.Present,
				JSONSchemaStrictness: startup.RawJSONSchemaStrictness(decoded.Strictness),
				Grammar: startup.RawGrammar{
					Present: decoded.Kind == ConstraintGrammar && decoded.Present,
					Lark:    decoded.Lark,
					Regex:   decoded.Regex,
				},
			})
		}
		tools[index] = startup.RawToolDescriptor{
			Present:             present,
			Name:                value.Name,
			Description:         value.Description,
			InputSchemaJSON:     value.InputSchemaJSON,
			ConstrainedSampling: constraint,
		}
	}
	handlers := make([]startup.RawHandlerDescriptor, len(raw.Handlers))
	for index, descriptor := range raw.Handlers {
		value, present := descriptor.Get()
		kind := startup.RawHandlerKindUnspecified
		if value.Kind >= int32(startup.RawHandlerKindSessionBeforeTreeRequest) &&
			value.Kind <= int32(startup.RawHandlerKindToolExecutionEnd) {
			kind = startup.RawHandlerKind(value.Kind)
		}
		handlers[index] = startup.RawHandlerDescriptor{Present: present, ID: value.ID, Kind: kind}
	}
	return startup.PendingRegistration{ID: candidate.ID, Path: candidate.Path, Tools: tools, Handlers: handlers}
}
