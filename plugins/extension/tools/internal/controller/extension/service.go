// Package extension maps the public extension contract to standard tool use cases.
package extension

import (
	"github.com/santhosh-tekuri/jsonschema/v6"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// Service exposes standard tools through Extension Contract v1.
type Service struct {
	// UnimplementedExtensionServiceServer provides forward-compatible gRPC defaults.
	extensionv1.UnimplementedExtensionServiceServer
	// readTool executes bounded project-file reads.
	readTool ReadTool
	// writeTool replaces complete project files.
	writeTool WriteTool
	// editTool applies exact project-file replacements.
	editTool EditTool
	// bashTool executes shell commands.
	bashTool BashTool
	// searchTool executes project discovery operations.
	searchTool SearchTool
	// schemas contains compiled input schemas by tool name.
	schemas map[string]*jsonschema.Schema
}

var _ extensionv1.ExtensionServiceServer = (*Service)(nil)

// New creates an extension controller for the standard tools.
func New(
	readTool ReadTool,
	writeTool WriteTool,
	editTool EditTool,
	bashTool BashTool,
	searchTool SearchTool,
) (*Service, error) {
	schemas := make(map[string]*jsonschema.Schema, standardToolCount)
	for name, source := range map[string]string{
		readToolName:  readInputSchemaJSON,
		writeToolName: writeInputSchemaJSON,
		editToolName:  editInputSchemaJSON,
		grepToolName:  grepInputSchemaJSON,
		findToolName:  findInputSchemaJSON,
		listToolName:  listInputSchemaJSON,
		bashToolName:  bashInputSchemaJSON,
	} {
		schema, err := compileSchema(name, source)
		if err != nil {
			return nil, err
		}
		schemas[name] = schema
	}
	return &Service{
		UnimplementedExtensionServiceServer: extensionv1.UnimplementedExtensionServiceServer{},
		readTool:                            readTool,
		writeTool:                           writeTool,
		editTool:                            editTool,
		bashTool:                            bashTool,
		searchTool:                          searchTool,
		schemas:                             schemas,
	}, nil
}
