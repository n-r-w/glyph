package main

import (
	"context"
	"fmt"
	"os"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	protocolv1 "github.com/n-r-w/glyph/experiments/plugin-runtime-spike/protocol/v1"
)

const (
	executionInputWait  = "wait"
	executionInputCrash = "crash"
	crashExitCode       = 23
)

// extensionService implements one fixed extension catalog and streamed execution.
type extensionService struct {
	protocolv1.UnimplementedExtensionServiceServer
	extensionID string
	tools       []string
}

// ListTools returns the complete startup catalog owned by this process.
func (s *extensionService) ListTools(
	_ context.Context,
	_ *protocolv1.ListToolsRequest,
) (*protocolv1.ListToolsResponse, error) {
	tools := make([]*protocolv1.Tool, 0, len(s.tools))
	for _, toolName := range s.tools {
		tools = append(tools, &protocolv1.Tool{Name: toolName})
	}
	return &protocolv1.ListToolsResponse{Tools: tools}, nil
}

// Execute emits progress before its terminal result, cancellation, or deliberate process crash.
func (s *extensionService) Execute(
	request *protocolv1.ExecuteRequest,
	stream protocolv1.ExtensionService_ExecuteServer,
) error {
	if request.Input == executionInputCrash {
		// A direct process exit proves that Host isolation does not depend on a graceful RPC return.
		os.Exit(crashExitCode)
	}

	if err := stream.Send(&protocolv1.ExecuteResponse{
		Content: &protocolv1.ExecuteResponse_Progress{
			Progress: fmt.Sprintf("%s:%s:started", s.extensionID, request.ToolName),
		},
	}); err != nil {
		return fmt.Errorf("send extension progress: %w", err)
	}

	if request.Input == executionInputWait {
		<-stream.Context().Done()
		return status.Error(codes.Canceled, "extension execution canceled")
	}

	if err := stream.Send(&protocolv1.ExecuteResponse{
		Content: &protocolv1.ExecuteResponse_Result{
			Result: &protocolv1.ToolResult{
				Content: fmt.Sprintf("%s:%s:%s", s.extensionID, request.ToolName, request.Input),
			},
		},
	}); err != nil {
		return fmt.Errorf("send extension result: %w", err)
	}
	return nil
}
