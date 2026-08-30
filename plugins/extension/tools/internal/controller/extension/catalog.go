package extension

import (
	"context"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// Register returns the complete standard tool catalog without session-tree handlers.
func (s *Service) Register(
	_ context.Context,
	_ *extensionv1.RegisterRequest,
) (*extensionv1.RegisterResponse, error) {
	return extensionv1.RegisterResponse_builder{
		Tools: []*extensionv1.ToolDescriptor{
			extensionv1.ToolDescriptor_builder{
				Name: new(readToolName),
				Description: new(
					"Read bounded text or supported image contents from a file in the working project.",
				),
				InputSchemaJson:     []byte(readInputSchemaJSON),
				ConstrainedSampling: strictPreferSampling(),
			}.Build(),
			extensionv1.ToolDescriptor_builder{
				Name:                new(writeToolName),
				Description:         new("Create or replace a file in the working project."),
				InputSchemaJson:     []byte(writeInputSchemaJSON),
				ConstrainedSampling: strictPreferSampling(),
			}.Build(),
			extensionv1.ToolDescriptor_builder{
				Name:                new(editToolName),
				Description:         new("Apply ordered unique exact text replacements to a project file."),
				InputSchemaJson:     []byte(editInputSchemaJSON),
				ConstrainedSampling: strictPreferSampling(),
			}.Build(),
			extensionv1.ToolDescriptor_builder{
				Name:                new(grepToolName),
				Description:         new("Search project files for matching lines."),
				InputSchemaJson:     []byte(grepInputSchemaJSON),
				ConstrainedSampling: strictPreferSampling(),
			}.Build(),
			extensionv1.ToolDescriptor_builder{
				Name:                new(findToolName),
				Description:         new("Find project paths matching a glob."),
				InputSchemaJson:     []byte(findInputSchemaJSON),
				ConstrainedSampling: strictPreferSampling(),
			}.Build(),
			extensionv1.ToolDescriptor_builder{
				Name:                new(listToolName),
				Description:         new("List direct project directory entries."),
				InputSchemaJson:     []byte(listInputSchemaJSON),
				ConstrainedSampling: strictPreferSampling(),
			}.Build(),
			extensionv1.ToolDescriptor_builder{
				Name:                new(bashToolName),
				Description:         new("Execute a bash command with optional timeout and bounded combined output."),
				InputSchemaJson:     []byte(bashInputSchemaJSON),
				ConstrainedSampling: strictPreferSampling(),
			}.Build(),
		},
		Handlers: nil,
	}.Build(), nil
}

// strictPreferSampling requests strict schema generation while permitting provider fallback.
func strictPreferSampling() *extensionv1.ConstrainedSampling {
	//nolint:exhaustruct_v5 // extensionv1.ConstrainedSampling_builder sets only the active JsonSchema field.
	return extensionv1.ConstrainedSampling_builder{
		JsonSchema: extensionv1.JsonSchemaConstrainedSampling_builder{
			Strictness: new(extensionv1.JsonSchemaStrictness_JSON_SCHEMA_STRICTNESS_PREFER),
		}.Build(),
	}.Build()
}
