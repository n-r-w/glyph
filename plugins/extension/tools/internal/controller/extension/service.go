// Package extension maps the public extension contract to standard tool use cases.
package extension

import (
	"context"
	"errors"

	"github.com/santhosh-tekuri/jsonschema/v6"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const rejectionCodeInvalidArgument = "INVALID_ARGUMENT"

// Service exposes standard tools through Extension Contract v1.
type Service struct {
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

var _ extensionsdk.Service = (*Service)(nil)

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
		readTool:   readTool,
		writeTool:  writeTool,
		editTool:   editTool,
		bashTool:   bashTool,
		searchTool: searchTool,
		schemas:    schemas,
	}, nil
}

// registrationOperation returns the fixed bundled tool catalog.
type registrationOperation struct{}

var _ extensionsdk.RegisterOperation = (*registrationOperation)(nil)

// Run returns the complete standard tool catalog.
func (operation *registrationOperation) Run(
	ctx context.Context,
) (*extensionv1.RegisterResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return standardRegistration(), nil
}

// Release has no admission reservation to free.
func (operation *registrationOperation) Release() {}

// toolOperation owns one validated bundled tool invocation.
type toolOperation struct {
	// service dispatches the selected standard tool.
	service *Service
	// request identifies the selected tool.
	request *extensionv1.ExecuteRequest
	// arguments contains the original JSON arguments.
	arguments []byte
	// validationErr contains an ordinary model-visible validation failure.
	validationErr error
}

var _ extensionsdk.ExecuteOperation = (*toolOperation)(nil)

// Run dispatches the validated tool invocation.
func (operation *toolOperation) Run(
	ctx context.Context,
	reporter *extensionsdk.ProgressReporter,
) (*extensionv1.ToolResult, error) {
	if result := operation.validationResult(); result != nil {
		return result, nil
	}
	return operation.service.execute(
		ctx,
		operation.request.GetToolName(),
		operation.arguments,
		func(reportContext context.Context, progress *extensionv1.ToolProgress) error {
			return reporter.Report(reportContext, progress)
		},
	)
}

// validationResult returns ordinary validation failure data when preparation found invalid arguments.
func (operation *toolOperation) validationResult() *extensionv1.ToolResult {
	if operation.validationErr == nil {
		return nil
	}
	return textResult(
		"invalid "+operation.request.GetToolName()+" arguments: "+operation.validationErr.Error(),
		true,
	)
}

// Release has no admission reservation to free.
func (operation *toolOperation) Release() {}

// PrepareRegister admits the fixed startup registration.
func (s *Service) PrepareRegister(
	ctx context.Context,
	_ *extensionv1.RegisterRequest,
) (extensionsdk.RegisterOperation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &registrationOperation{}, nil
}

// PrepareHandle rejects handler work because the bundled extension registers no handlers.
func (s *Service) PrepareHandle(
	context.Context,
	*extensionv1.HandleRequest,
) (extensionsdk.HandleOperation, error) {
	return nil, extensionsdk.Reject(rejectionCodeInvalidArgument, errors.New("bundled tools register no handlers"))
}

// PrepareExecute validates tool identity and arguments before admitting execution.
func (s *Service) PrepareExecute(
	ctx context.Context,
	request *extensionv1.ExecuteRequest,
) (extensionsdk.ExecuteOperation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, extensionsdk.Reject(rejectionCodeInvalidArgument, errors.New("tool request is required"))
	}
	arguments := request.GetArgumentsJson()
	var validationErr error
	if schema, exists := s.schemas[request.GetToolName()]; exists {
		arguments, validationErr = validateArguments(schema, request.GetArgumentsJson())
	}
	return &toolOperation{
		service: s, request: request, arguments: arguments, validationErr: validationErr,
	}, nil
}
