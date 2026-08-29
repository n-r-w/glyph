package extension

import (
	"encoding/json/v2"
	"errors"
	"fmt"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// Execute validates and executes one standard tool call.
func (s *Service) Execute(
	request *extensionv1.ExecuteRequest,
	stream extensionv1.ExtensionService_ExecuteServer,
) error {
	schema, exists := s.schemas[request.GetToolName()]
	if !exists {
		return sendResult(stream, fmt.Sprintf("unknown tool %q", request.GetToolName()), true)
	}
	arguments, err := validateArguments(schema, request.GetArgumentsJson())
	if err != nil {
		return sendResult(stream, fmt.Sprintf("invalid %s arguments: %v", request.GetToolName(), err), true)
	}

	switch request.GetToolName() {
	case readToolName:
		return s.executeRead(arguments, stream)
	case writeToolName:
		return s.executeWrite(arguments, stream)
	case editToolName:
		return s.executeEdit(arguments, stream)
	case grepToolName:
		return s.executeGrep(arguments, stream)
	case findToolName:
		return s.executeFind(arguments, stream)
	case listToolName:
		return s.executeList(arguments, stream)
	case bashToolName:
		return s.executeBash(arguments, stream)
	default:
		return sendResult(stream, fmt.Sprintf("unknown tool %q", request.GetToolName()), true)
	}
}

// executeRead decodes and executes read.
func (s *Service) executeRead(arguments []byte, stream extensionv1.ExtensionService_ExecuteServer) error {
	var input readArguments
	if err := json.Unmarshal(arguments, &input); err != nil {
		return sendResult(stream, fmt.Sprintf("decode read arguments: %v", err), true)
	}
	result, err := s.readTool.Read(stream.Context(), input.Path, input.Offset, input.Limit)
	if err != nil {
		return operationResult(stream, "", err)
	}
	if image, ok := result.Image.Get(); ok {
		return sendImageResult(stream, image.MediaType, image.Data)
	}
	text, present := result.Text.Get()
	if !present {
		return operationResult(stream, "", errors.New("read result has no payload"))
	}
	return operationResult(stream, text, nil)
}

// executeWrite decodes and executes write.
func (s *Service) executeWrite(arguments []byte, stream extensionv1.ExtensionService_ExecuteServer) error {
	return executeDecoded(arguments, stream, "write", func(input writeArguments) (string, error) {
		return mutationResult(input.Path, "wrote file ", s.writeTool.Write(stream.Context(), input.Path, input.Content))
	})
}

// executeEdit decodes and executes edit.
func (s *Service) executeEdit(arguments []byte, stream extensionv1.ExtensionService_ExecuteServer) error {
	return executeDecoded(arguments, stream, "edit", func(input editArguments) (string, error) {
		return mutationResult(input.Path, "replaced text in ", s.editTool.Edit(stream.Context(), input.Path, input.Edits))
	})
}

// executeGrep decodes and executes grep.
func (s *Service) executeGrep(arguments []byte, stream extensionv1.ExtensionService_ExecuteServer) error {
	return executeDecoded(arguments, stream, "grep", func(input grepArguments) (string, error) {
		return s.searchTool.Grep(stream.Context(), GrepArguments(input))
	})
}

// executeFind decodes and executes find.
func (s *Service) executeFind(arguments []byte, stream extensionv1.ExtensionService_ExecuteServer) error {
	return executeDecoded(arguments, stream, "find", func(input findArguments) (string, error) {
		return s.searchTool.Find(stream.Context(), FindArguments(input))
	})
}

// executeList decodes and executes ls.
// mutationResult converts a file mutation result to user-facing operation output.
func mutationResult(path, successPrefix string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	return successPrefix + path, nil
}

// executeDecoded decodes typed arguments, executes one operation, and sends its terminal result.
func executeDecoded[T any](
	arguments []byte,
	stream extensionv1.ExtensionService_ExecuteServer,
	operation string,
	execute func(T) (string, error),
) error {
	var input T
	if err := json.Unmarshal(arguments, &input); err != nil {
		return sendResult(stream, fmt.Sprintf("decode %s arguments: %v", operation, err), true)
	}
	result, err := execute(input)
	return operationResult(stream, result, err)
}

func (s *Service) executeList(arguments []byte, stream extensionv1.ExtensionService_ExecuteServer) error {
	var input listArguments
	if err := json.Unmarshal(arguments, &input); err != nil {
		return sendResult(stream, fmt.Sprintf("decode ls arguments: %v", err), true)
	}
	result, err := s.searchTool.List(stream.Context(), ListArguments(input))
	return operationResult(stream, result, err)
}
