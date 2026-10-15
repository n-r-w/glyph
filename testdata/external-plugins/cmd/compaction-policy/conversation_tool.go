package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"google.golang.org/protobuf/encoding/protojson"

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
	encoded, err := protojson.Marshal(result)
	if err != nil {
		return nil, err
	}
	path := os.Getenv(conversationToolEnvironment)
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	const artifactPermission = 0o600
	artifact, err := root.OpenFile(filepath.Base(path), os.O_CREATE|os.O_WRONLY|os.O_APPEND, artifactPermission)
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	_, writeErr := artifact.Write(append(encoded, '\n'))
	if finishErr := errors.Join(writeErr, artifact.Close(), root.Close()); finishErr != nil {
		return nil, finishErr
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
