package main

import (
	"context"
	"errors"
	"os"
	"strconv"

	"google.golang.org/protobuf/proto"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	// userSuffix is appended to every text block of a user message.
	userSuffix = " [final user]"
	// modelSuffix is appended to every text block of a model response.
	modelSuffix = " [final model]"
	// toolSuffix is appended to every text block of a tool result.
	toolSuffix = " [final tool]"
	// failureText is the complete ordinary error of the failure scenarios.
	failureText = "reference finalized handler failed"
	// failToolMode fails the first tool-result invocation of finalized-mark.
	failToolMode = "finalized-fail-tool"
	// failModelMode fails the first model-response invocation of finalized-mark.
	failModelMode = "finalized-fail-model"
	// roleMode replaces the first model response with a user message.
	roleMode = "finalized-role"
	// badReferenceMode adds an unknown replay reference to the first model response.
	badReferenceMode = "finalized-bad-reference"
	// invalidMode returns an empty action for the first model response.
	invalidMode = "finalized-invalid"
	// imageMode appends an image block to the first user message.
	imageMode = "finalized-image"
	// blockMode blocks the first model-response invocation until Host cancels it.
	blockMode = "finalized-block"
	// preserveMode preserves every message.
	preserveMode = "finalized-preserve"
	// failingModelEnvironment names the one-based position of a model response that finalized-mark fails in any mode.
	failingModelEnvironment = "GLYPH_FINALIZED_FAILING_MODEL"
	// redactMode replaces the text of every model text block with redactedText.
	redactMode = "finalized-redact"
	// argumentsMode replaces the arguments of every model tool call with replacedArguments.
	argumentsMode = "finalized-arguments"
	// redactedText replaces model text in the redaction scenario.
	redactedText = "[redacted]"
	// replacedArguments are the tool-call arguments of the arguments scenario.
	replacedArguments = `{"value":"final"}`
	// roleChangeText is the user text that replaces a model response in the role scenario.
	roleChangeText = "role change"
	// unknownReference is the replay reference that no invocation issued.
	unknownReference = "unknown"
	// addedImageMediaType is the media type of the image that the image scenario appends.
	addedImageMediaType = "image/png"
	// signalsEnvironment names a directory that receives the blocked-invocation marker when set.
	signalsEnvironment = "GLYPH_FINALIZED_SIGNALS"
	// blockedSignal is the marker file written when the blocked invocation starts.
	blockedSignal = "finalized-blocked"
)

// mark selects the finalized-mark action for the role of the current message and the scenario.
func (o *operation) mark(
	ctx context.Context,
	current *extensionpb.ModelContextEntry,
) (*extensionpb.HandleResponse, error) {
	if current.WhichContent() == extensionpb.ModelContextEntry_Model_case {
		return o.markModel(ctx, current.GetModel())
	}
	if o.service.mode == preserveMode {
		return preserve(), nil
	}
	switch current.WhichContent() {
	case extensionpb.ModelContextEntry_User_case:
		firstUser := !o.service.userSeen.Swap(true)
		user := markUser(current.GetUser())
		if o.service.mode == imageMode && firstUser {
			user.SetContent(append(user.GetContent(), imageBlock()))
		}
		return replace(userEntry(user)), nil
	case extensionpb.ModelContextEntry_ToolResult_case:
		firstTool := !o.service.toolSeen.Swap(true)
		if o.service.mode == failToolMode && firstTool {
			return handlerError(), nil
		}
		return replace(toolEntry(markTool(current.GetToolResult()))), nil
	case extensionpb.ModelContextEntry_Model_case, extensionpb.ModelContextEntry_Content_not_set_case:
	}
	return nil, errors.New("finalized message has no content")
}

// markModel selects the model-response action of the scenario; the scenarios that change one response change the
// first one, and failingModelEnvironment selects one more response to fail.
func (o *operation) markModel(
	ctx context.Context,
	response *extensionpb.ReplayableModelResponse,
) (*extensionpb.HandleResponse, error) {
	position := o.service.modelResponses.Add(1)
	marked := markResponse(response, o.service.mode)
	switch {
	case os.Getenv(failingModelEnvironment) == strconv.FormatInt(position, 10):
		return handlerError(), nil
	case o.service.mode == preserveMode:
		return preserve(), nil
	case position != 1:
		return replace(modelEntry(marked)), nil
	}
	switch o.service.mode {
	case failModelMode:
		return handlerError(), nil
	case roleMode:
		user := extensionpb.SessionTreeUserMessage_builder{Content: []*extensionpb.SessionTreeUserContent{
			extensionpb.SessionTreeUserContent_builder{Text: new(roleChangeText), Image: nil}.Build(),
		}}.Build()
		return replace(userEntry(user)), nil
	case badReferenceMode:
		if len(marked.GetContent()) > 0 {
			marked.GetContent()[0].SetReplayReference(unknownReference)
		}
	case invalidMode:
		//nolint:exhaustruct_v5 // An empty action is the invalid result of this scenario.
		return extensionpb.HandleResponse_builder{
			FinalizedMessage: extensionpb.FinalizedMessageAction_builder{}.Build(),
		}.Build(), nil
	case blockMode:
		return nil, block(ctx)
	}
	return replace(modelEntry(marked)), nil
}

// markUser appends userSuffix to every text block of a copy of the user message.
func markUser(user *extensionpb.SessionTreeUserMessage) *extensionpb.SessionTreeUserMessage {
	marked := proto.CloneOf(user)
	for _, block := range marked.GetContent() {
		if block.HasText() {
			block.SetText(block.GetText() + userSuffix)
		}
	}
	return marked
}

// markResponse returns a copy of the response in which every text block ends with modelSuffix; the redaction
// scenario replaces the text instead, and the arguments scenario replaces the arguments of every tool call. Replay
// references, tool-call identities, and other blocks stay unchanged.
func markResponse(response *extensionpb.ReplayableModelResponse, mode string) *extensionpb.ReplayableModelResponse {
	marked := proto.CloneOf(response)
	for _, block := range marked.GetContent() {
		content := block.GetContent()
		switch content.GetKind() {
		case extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_TEXT:
			if mode == redactMode {
				content.SetText(redactedText)
			} else {
				content.SetText(content.GetText() + modelSuffix)
			}
		case extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_TOOL_CALL:
			if mode == argumentsMode {
				content.GetToolCall().SetArgumentsJson([]byte(replacedArguments))
			}
		case extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_UNSPECIFIED,
			extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_REFUSAL,
			extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_REASONING:
		}
	}
	return marked
}

// markTool appends toolSuffix to every text block of a copy of the tool result.
func markTool(result *extensionpb.SessionTreeToolResult) *extensionpb.SessionTreeToolResult {
	marked := proto.CloneOf(result)
	for _, block := range marked.GetContents() {
		if block.HasText() {
			block.SetText(block.GetText() + toolSuffix)
		}
	}
	return marked
}

// userEntry wraps a user message in a finalized-message entry.
func userEntry(user *extensionpb.SessionTreeUserMessage) *extensionpb.ModelContextEntry {
	return extensionpb.ModelContextEntry_builder{User: user, Model: nil, ToolResult: nil}.Build()
}

// modelEntry wraps a model response in a finalized-message entry.
func modelEntry(response *extensionpb.ReplayableModelResponse) *extensionpb.ModelContextEntry {
	return extensionpb.ModelContextEntry_builder{User: nil, Model: response, ToolResult: nil}.Build()
}

// toolEntry wraps a tool result in a finalized-message entry.
func toolEntry(result *extensionpb.SessionTreeToolResult) *extensionpb.ModelContextEntry {
	return extensionpb.ModelContextEntry_builder{User: nil, Model: nil, ToolResult: result}.Build()
}

// imageBlock creates one PNG user image block.
func imageBlock() *extensionpb.SessionTreeUserContent {
	image := extensionpb.SessionTreeImage_builder{
		MediaType: new(addedImageMediaType), Data: []byte{0x89, 'P', 'N', 'G'},
	}.Build()
	return extensionpb.SessionTreeUserContent_builder{Text: nil, Image: image}.Build()
}

// handlerError returns the ordinary error of the failure scenarios.
//
//nolint:exhaustruct_v5 // Protobuf oneof builders set only their active error.
func handlerError() *extensionpb.HandleResponse {
	return extensionpb.HandleResponse_builder{
		Error: extensionpb.HandlerError_builder{Message: new(failureText)}.Build(),
	}.Build()
}

// block reports the started invocation and waits until Host cancels it.
func block(ctx context.Context) error {
	if directory := os.Getenv(signalsEnvironment); directory != "" {
		if err := writeSignal(directory); err != nil {
			return err
		}
	}
	<-ctx.Done()
	return ctx.Err()
}

// writeSignal creates the blocked-invocation marker inside the signal directory.
func writeSignal(directory string) (returnErr error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	file, err := root.OpenFile(blockedSignal, os.O_CREATE|os.O_WRONLY, tracePermission)
	if err != nil {
		return err
	}
	return file.Close()
}
