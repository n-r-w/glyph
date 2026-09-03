// Package tools owns Host tool registration, policy, and Agent Core execution.
package tools

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/samber/mo"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

//go:generate go tool mockgen -source=service.go -destination=service_mock.go -package=tools

// Runtime exposes the extension runtime operations required by tool policy.
type Runtime interface {
	ToolRuntimeAvailable(extensionID string) bool
	ExecuteTool(
		ctx context.Context,
		extensionID, name string,
		argumentsJSON []byte,
		handleProgress tool.ProgressHandler,
	) (tool.Result, error)
}

// Service owns accepted tools, schemas, ownership, and model-visible results.
type Service struct {
	// runtime invokes one tool through its owning extension process.
	runtime Runtime
	// mutex protects prepared and accepted registration state.
	mutex sync.RWMutex
	// prepared contains locally validated schemas before startup commits registrations.
	prepared map[string]map[string]*jsonschema.Schema
	// owners contains accepted tool state by globally unique name.
	owners map[string]*owner
}

// owner contains one accepted descriptor, schema, and extension identity.
type owner struct {
	// extensionID identifies the owning extension runtime.
	extensionID string
	// descriptor is the model-visible accepted descriptor.
	descriptor tool.Descriptor
	// schema validates deterministic invocation arguments.
	schema *jsonschema.Schema
}

var (
	_ run.ToolRuntime       = (*Service)(nil)
	_ startup.ToolRegistrar = (*Service)(nil)
)

// New creates the Host tool capability service.
func New(runtime Runtime) *Service {
	return &Service{
		runtime:  runtime,
		mutex:    sync.RWMutex{},
		prepared: make(map[string]map[string]*jsonschema.Schema),
		owners:   make(map[string]*owner),
	}
}

// ValidateLocal validates one extension-local tool registration and retains compiled schemas for commit.
func (s *Service) ValidateLocal(registration startup.PendingRegistration) ([]tool.Descriptor, error) {
	descriptors := make([]tool.Descriptor, 0, len(registration.Tools))
	schemas := make(map[string]*jsonschema.Schema, len(registration.Tools))
	for index := range registration.Tools {
		raw := &registration.Tools[index]
		if !raw.Present {
			return nil, fmt.Errorf("descriptor %d is missing", index)
		}
		if raw.Name == "" {
			return nil, fmt.Errorf("descriptor %d has an empty name", index)
		}
		if raw.Description == "" {
			return nil, fmt.Errorf("tool %q has an empty description", raw.Name)
		}
		if _, duplicate := schemas[raw.Name]; duplicate {
			return nil, fmt.Errorf("tool name %q is duplicated", raw.Name)
		}
		schema, err := compileToolSchema(raw.InputSchemaJSON)
		if err != nil {
			return nil, fmt.Errorf("tool %q input schema: %w", raw.Name, err)
		}
		constraint, err := mapConstrainedSampling(raw.ConstrainedSampling, raw.InputSchemaJSON)
		if err != nil {
			return nil, fmt.Errorf("tool %q constrained sampling: %w", raw.Name, err)
		}
		descriptors = append(
			descriptors,
			tool.Descriptor{
				Name:                raw.Name,
				Description:         raw.Description,
				InputSchemaJSON:     bytes.Clone(raw.InputSchemaJSON),
				ConstrainedSampling: constraint,
			},
		)
		schemas[raw.Name] = schema
	}
	s.mutex.Lock()
	s.prepared[registration.ID] = schemas
	s.mutex.Unlock()
	return descriptors, nil
}

// Conflicts returns deterministic issues for every name owned by multiple extensions.
func (s *Service) Conflicts(registrations []startup.AcceptedRegistration) []startup.Issue {
	owners := make(map[string][]string)
	for _, registration := range registrations {
		for index := range registration.Tools {
			descriptor := &registration.Tools[index]
			owners[descriptor.Name] = append(owners[descriptor.Name], registration.ID)
		}
	}
	const minimumConflictOwners = 2
	issues := make([]startup.Issue, 0)
	for name, pluginIDs := range owners {
		if len(pluginIDs) < minimumConflictOwners {
			continue
		}
		slices.Sort(pluginIDs)
		issues = append(
			issues,
			startup.Issue{PluginIDs: pluginIDs, Path: "", Err: fmt.Errorf("tool name %q conflicts", name)},
		)
	}
	slices.SortFunc(
		issues,
		func(left, right startup.Issue) int { return cmp.Compare(left.Err.Error(), right.Err.Error()) },
	)
	return issues
}

// Commit publishes tools from registrations accepted by every startup validator.
func (s *Service) Commit(registrations []startup.AcceptedRegistration) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for _, registration := range registrations {
		for index := range registration.Tools {
			descriptor := &registration.Tools[index]
			s.owners[descriptor.Name] = &owner{
				extensionID: registration.ID,
				descriptor:  *descriptor,
				schema:      s.prepared[registration.ID][descriptor.Name],
			}
		}
	}
	s.prepared = make(map[string]map[string]*jsonschema.Schema)
}

// Tools returns accepted descriptors whose owning runtime is available, sorted by name.
func (s *Service) Tools() []tool.Descriptor {
	s.mutex.RLock()
	owners := make([]*owner, 0, len(s.owners))
	for _, accepted := range s.owners {
		owners = append(owners, accepted)
	}
	s.mutex.RUnlock()
	result := make([]tool.Descriptor, 0, len(owners))
	for _, accepted := range owners {
		if s.runtime.ToolRuntimeAvailable(accepted.extensionID) {
			result = append(result, accepted.descriptor)
		}
	}
	slices.SortFunc(result, func(left, right tool.Descriptor) int { return cmp.Compare(left.Name, right.Name) })
	return result
}

// Execute validates one call and invokes its owning available extension runtime.
func (s *Service) Execute(
	ctx context.Context,
	call model.ToolCall,
	handleProgress tool.ProgressHandler,
) (agent.ToolResult, error) {
	argumentsJSON, err := json.Marshal(call.Arguments, json.Deterministic(true))
	if err != nil {
		return agent.ToolResult{}, fmt.Errorf("encode tool %q arguments: %w", call.Name, err)
	}
	s.mutex.RLock()
	accepted, exists := s.owners[call.Name]
	s.mutex.RUnlock()
	if !exists || !s.runtime.ToolRuntimeAvailable(accepted.extensionID) {
		return unavailableResult(call), nil
	}
	if validationErr := validateArguments(accepted.schema, argumentsJSON); validationErr != nil {
		return agent.ToolResult{
			CallID:   call.ID,
			ToolName: call.Name,
			Contents: tool.TextContents(fmt.Sprintf("invalid arguments for tool %q: %v", call.Name, validationErr)),
			IsError:  true,
		}, nil
	}
	result, executeErr := s.runtime.ExecuteTool(ctx, accepted.extensionID, call.Name, argumentsJSON, handleProgress)
	return agent.ToolResult{
		CallID:   call.ID,
		ToolName: call.Name,
		Contents: slices.Clone(result.Contents),
		IsError:  result.IsError,
	}, executeErr
}

// unavailableResult returns the existing model-visible unavailable tool result.
func unavailableResult(call model.ToolCall) agent.ToolResult {
	return agent.ToolResult{
		CallID:   call.ID,
		ToolName: call.Name,
		Contents: tool.TextContents(fmt.Sprintf("tool %q is unavailable", call.Name)),
		IsError:  true,
	}
}

// mapConstrainedSampling validates one raw constraint and preserves its presence.
func mapConstrainedSampling(
	raw mo.Option[startup.RawConstrainedSampling],
	schemaJSON []byte,
) (mo.Option[tool.ConstrainedSampling], error) {
	constraint, present := raw.Get()
	if !present {
		return mo.None[tool.ConstrainedSampling](), nil
	}
	var mapped tool.ConstrainedSampling
	var err error
	switch constraint.Kind {
	case startup.RawConstrainedSamplingJSONSchema:
		mapped, err = mapJSONSchemaSampling(constraint)
	case startup.RawConstrainedSamplingGrammar:
		mapped, err = mapGrammarSampling(constraint.Grammar, schemaJSON)
	case startup.RawConstrainedSamplingMissing:
		err = errors.New("config is missing")
	case startup.RawConstrainedSamplingInvalid:
		err = errors.New("config is invalid")
	}
	if err != nil {
		return mo.None[tool.ConstrainedSampling](), err
	}
	return mo.Some(mapped), nil
}

// mapJSONSchemaSampling validates one raw JSON Schema constraint.
func mapJSONSchemaSampling(raw startup.RawConstrainedSampling) (tool.ConstrainedSampling, error) {
	if !raw.JSONSchemaPresent {
		return tool.ConstrainedSampling{}, errors.New("JSON Schema config is missing")
	}
	var strictness tool.JSONSchemaStrictness
	switch raw.JSONSchemaStrictness {
	case startup.RawJSONSchemaStrictnessPrefer:
		strictness = tool.JSONSchemaStrictPrefer
	case startup.RawJSONSchemaStrictnessRequire:
		strictness = tool.JSONSchemaStrictRequire
	case startup.RawJSONSchemaStrictnessUnspecified:
		return tool.ConstrainedSampling{}, errors.New("JSON Schema strictness is unspecified")
	default:
		return tool.ConstrainedSampling{}, errors.New("JSON Schema strictness is invalid")
	}
	return tool.ConstrainedSampling{
		Kind:                 tool.ConstrainedSamplingJSONSchema,
		JSONSchemaStrictness: mo.Some(strictness),
		Grammar:              mo.None[tool.GrammarVariants](),
		GrammarInputProperty: mo.None[string](),
	}, nil
}

// mapGrammarSampling validates grammar variants and retains the schema input property.
func mapGrammarSampling(raw startup.RawGrammar, schemaJSON []byte) (tool.ConstrainedSampling, error) {
	if !raw.Present {
		return tool.ConstrainedSampling{}, errors.New("grammar config is missing")
	}
	lark, hasLark := raw.Lark.Get()
	regex, hasRegex := raw.Regex.Get()
	if (!hasLark || strings.TrimSpace(lark) == "") && (!hasRegex || strings.TrimSpace(regex) == "") {
		return tool.ConstrainedSampling{}, errors.New("grammar requires at least one nonempty grammar variant")
	}
	var schema struct {
		Properties map[string]jsontext.Value `json:"properties"`
		Required   []string                  `json:"required"`
	}
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		return tool.ConstrainedSampling{}, fmt.Errorf("parse grammar schema: %w", err)
	}
	if len(schema.Properties) != 1 || len(schema.Required) != 1 || schema.Required[0] == "" {
		return tool.ConstrainedSampling{}, errors.New("grammar schema must have exactly one required string property")
	}
	if err := validateGrammarInputProperty(schema.Properties, schema.Required[0]); err != nil {
		return tool.ConstrainedSampling{}, err
	}
	return tool.ConstrainedSampling{
		Kind:                 tool.ConstrainedSamplingGrammar,
		JSONSchemaStrictness: mo.None[tool.JSONSchemaStrictness](),
		Grammar:              mo.Some(tool.GrammarVariants{Lark: raw.Lark, Regex: raw.Regex}),
		GrammarInputProperty: mo.Some(schema.Required[0]),
	}, nil
}

// validateGrammarInputProperty enforces the direct single-string input contract.
func validateGrammarInputProperty(properties map[string]jsontext.Value, required string) error {
	const rule = "grammar schema must have exactly one required string property"
	propertyJSON, exists := properties[required]
	if !exists {
		return errors.New(rule)
	}
	var property struct {
		Type jsontext.Value `json:"type"`
	}
	if err := json.Unmarshal(propertyJSON, &property); err != nil {
		return fmt.Errorf("%s: parse property JSON: %w", rule, err)
	}
	if len(property.Type) == 0 {
		return errors.New(rule)
	}
	var propertyType string
	if err := json.Unmarshal(property.Type, &propertyType); err != nil {
		return fmt.Errorf("%s: parse property type JSON: %w", rule, err)
	}
	if propertyType != "string" {
		return errors.New(rule)
	}
	return nil
}

// compileToolSchema compiles a Draft 2020-12 object schema for tool arguments.
func compileToolSchema(schemaJSON []byte) (*jsonschema.Schema, error) {
	const schemaLocation = "glyph://extension/input-schema.json"
	var root struct {
		Type jsontext.Value `json:"type"`
	}
	if err := json.Unmarshal(schemaJSON, &root); err != nil {
		return nil, fmt.Errorf("parse JSON Schema: %w", err)
	}
	if len(root.Type) == 0 {
		return nil, errors.New("schema root type must be object")
	}
	var rootType string
	if err := json.Unmarshal(root.Type, &rootType); err != nil {
		return nil, fmt.Errorf("schema root type must be object: parse root type JSON: %w", err)
	}
	if rootType != "object" {
		return nil, errors.New("schema root type must be object")
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		return nil, fmt.Errorf("parse JSON Schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if registerErr := compiler.AddResource(schemaLocation, document); registerErr != nil {
		return nil, fmt.Errorf("register JSON Schema: %w", registerErr)
	}
	schema, err := compiler.Compile(schemaLocation)
	if err != nil {
		return nil, fmt.Errorf("compile JSON Schema: %w", err)
	}
	return schema, nil
}

// validateArguments parses one JSON value and applies its compiled schema.
func validateArguments(schema *jsonschema.Schema, argumentsJSON []byte) error {
	arguments, err := jsonschema.UnmarshalJSON(bytes.NewReader(argumentsJSON))
	if err != nil {
		return fmt.Errorf("parse arguments JSON: %w", err)
	}
	if validationErr := schema.Validate(arguments); validationErr != nil {
		return fmt.Errorf("validate arguments JSON: %w", validationErr)
	}
	return nil
}
