package main

import (
	"context"
	"net"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// callerToolName identifies the live public manual-compaction caller.
	callerToolName = "reference_manual_caller"
	// callerToolDescription describes the socket-coordinated public operation.
	callerToolDescription = "Runs manual compaction within a live ordinary invocation"
	// callerToolSchema accepts no tool arguments.
	callerToolSchema = `{"type":"object","properties":{},"additionalProperties":false}`
	// callerToolResultText acknowledges the completed socket exchange.
	callerToolResultText = "manual caller completed"
)

// callerToolOperation keeps the invocation live through the socket-requested compaction terminal.
type callerToolOperation struct {
	// listener receives one test-owned public compaction request.
	listener net.Listener
}

var _ extensionsdk.ExecuteOperation = (*callerToolOperation)(nil)

// Run reports the child outcome before returning the ordinary tool result.
func (o *callerToolOperation) Run(
	ctx context.Context,
	_ *extensionsdk.ProgressReporter,
) (*extensionpb.ToolResult, error) {
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return nil, err
	}
	if callerErr := runCaller(context.WithoutCancel(ctx), binding, o.listener); callerErr != nil {
		return nil, callerErr
	}
	content := new(extensionpb.ToolResultContent)
	content.SetText(callerToolResultText)
	return extensionpb.ToolResult_builder{
		IsError: new(false), Contents: []*extensionpb.ToolResultContent{content},
	}.Build(), nil
}

// Release requires no cleanup because the operation owns no releasable resources.
func (o *callerToolOperation) Release() {}

// newCallerTools registers the caller only in its listener-owning fixture process.
func newCallerTools(enabled bool) []*extensionpb.ToolDescriptor {
	if !enabled {
		return nil
	}
	return []*extensionpb.ToolDescriptor{extensionpb.ToolDescriptor_builder{
		Name: new(callerToolName), Description: new(callerToolDescription),
		InputSchemaJson: []byte(callerToolSchema), ConstrainedSampling: nil,
	}.Build()}
}
