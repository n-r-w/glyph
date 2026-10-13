package main

import (
	"context"
	"os"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// conversationToolEnvironment is the environment variable name for the tool execution artifact path.
const conversationToolEnvironment = "GLYPH_REFERENCE_CONVERSATION_TOOL"

// conversationToolName identifies the reference tool capability.
const conversationToolName = "reference_observation_tool"

// conversationToolOperation records a model-requested execution.
type conversationToolOperation struct{}

var _ extensionsdk.ExecuteOperation = (*conversationToolOperation)(nil)

// Run records the model-requested tool result.
func (o *conversationToolOperation) Run(
	ctx context.Context,
	_ *extensionsdk.ProgressReporter,
) (*extensionpb.ToolResult, error) {
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	content := new(extensionpb.ToolResultContent)
	const resultText = "reference tool completed"
	content.SetText(resultText)
	result := extensionpb.ToolResult_builder{
		IsError:  new(false),
		Contents: []*extensionpb.ToolResultContent{content},
	}.Build()
	if err := writePolicyArtifact(os.Getenv(conversationToolEnvironment), result); err != nil {
		return nil, err
	}
	return result, nil
}

// Release ends tool ownership.
func (o *conversationToolOperation) Release() {}

// newConversationObservationTools supplies the optional reference tool descriptor.
func newConversationObservationTools() []*extensionpb.ToolDescriptor {
	if os.Getenv(conversationToolEnvironment) == "" {
		return nil
	}
	const description = "Records reference tool execution"
	const inputSchema = `{"type":"object","properties":{},"additionalProperties":false}`
	return []*extensionpb.ToolDescriptor{extensionpb.ToolDescriptor_builder{
		Name: new(
			conversationToolName,
		),
		Description:         new(description),
		InputSchemaJson:     []byte(inputSchema),
		ConstrainedSampling: nil,
	}.Build()}
}
