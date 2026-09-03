package extension

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// execute dispatches one prepared standard tool call.
func (s *Service) execute(
	ctx context.Context,
	toolName string,
	arguments []byte,
	report func(context.Context, *extensionv1.ToolProgress) error,
) (*extensionv1.ToolResult, error) {
	switch toolName {
	case readToolName:
		return s.executeRead(ctx, arguments)
	case writeToolName:
		return s.executeWrite(ctx, arguments)
	case editToolName:
		return s.executeEdit(ctx, arguments)
	case grepToolName:
		return s.executeGrep(ctx, arguments)
	case findToolName:
		return s.executeFind(ctx, arguments)
	case listToolName:
		return s.executeList(ctx, arguments)
	case bashToolName:
		return s.executeBash(ctx, arguments, report)
	default:
		return textResult(fmt.Sprintf("unknown tool %q", toolName), true), nil
	}
}

// executeRead decodes and executes read.
func (s *Service) executeRead(ctx context.Context, arguments []byte) (*extensionv1.ToolResult, error) {
	var input readArguments
	if err := json.Unmarshal(arguments, &input); err != nil {
		return textResult(fmt.Sprintf("decode read arguments: %v", err), true), nil
	}
	result, err := s.readTool.Read(ctx, input.Path, input.Offset, input.Limit)
	if err != nil {
		return operationResult("", err)
	}
	if image, ok := result.Image.Get(); ok {
		return imageResult(image.MediaType, image.Data), nil
	}
	text, present := result.Text.Get()
	if !present {
		return operationResult("", errors.New("read result has no payload"))
	}
	return operationResult(text, nil)
}

// executeWrite decodes and executes write.
func (s *Service) executeWrite(ctx context.Context, arguments []byte) (*extensionv1.ToolResult, error) {
	return executeDecoded(arguments, "write", func(input writeArguments) (string, error) {
		return mutationResult(input.Path, "wrote file ", s.writeTool.Write(ctx, input.Path, input.Content))
	})
}

// executeEdit decodes and executes edit.
func (s *Service) executeEdit(ctx context.Context, arguments []byte) (*extensionv1.ToolResult, error) {
	return executeDecoded(arguments, "edit", func(input editArguments) (string, error) {
		return mutationResult(input.Path, "replaced text in ", s.editTool.Edit(ctx, input.Path, input.Edits))
	})
}

// executeGrep decodes and executes grep.
func (s *Service) executeGrep(ctx context.Context, arguments []byte) (*extensionv1.ToolResult, error) {
	return executeDecoded(arguments, "grep", func(input grepArguments) (string, error) {
		return s.searchTool.Grep(ctx, GrepArguments(input))
	})
}

// executeFind decodes and executes find.
func (s *Service) executeFind(ctx context.Context, arguments []byte) (*extensionv1.ToolResult, error) {
	return executeDecoded(arguments, "find", func(input findArguments) (string, error) {
		return s.searchTool.Find(ctx, FindArguments(input))
	})
}

// mutationResult converts a file mutation result to user-facing operation output.
func mutationResult(path, successPrefix string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	return successPrefix + path, nil
}

// executeDecoded decodes typed arguments and executes one operation.
func executeDecoded[T any](
	arguments []byte,
	operationName string,
	execute func(T) (string, error),
) (*extensionv1.ToolResult, error) {
	var input T
	if err := json.Unmarshal(arguments, &input); err != nil {
		return textResult(fmt.Sprintf("decode %s arguments: %v", operationName, err), true), nil
	}
	result, err := execute(input)
	return operationResult(result, err)
}

// executeList decodes and executes ls.
func (s *Service) executeList(ctx context.Context, arguments []byte) (*extensionv1.ToolResult, error) {
	var input listArguments
	if err := json.Unmarshal(arguments, &input); err != nil {
		return textResult(fmt.Sprintf("decode ls arguments: %v", err), true), nil
	}
	result, err := s.searchTool.List(ctx, ListArguments(input))
	return operationResult(result, err)
}
