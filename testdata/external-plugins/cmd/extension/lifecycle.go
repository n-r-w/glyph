package main

import (
	"context"
	"errors"
	"strings"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// lifecycleMessageEntryType identifies the model-assisted agent-start result.
	lifecycleMessageEntryType = "agent-start-result"
	// lifecycleRequestText is the configured request made during agent-start observation.
	lifecycleRequestText = "Create one short lifecycle note."
)

// observeAgentStart recovers state, requests configured model work, and appends its public result.
func observeAgentStart(
	ctx context.Context,
	binding *extensionsdk.ExtensionContext,
	invocation *extensionv1.LifecycleInvocation,
) error {
	if invocation == nil || invocation.GetAgentStart() == nil {
		return errors.New("agent-start lifecycle payload is required")
	}
	recovery, err := binding.StartGetSessionState(ctx)
	if err != nil {
		return err
	}
	if _, err = recovery.Wait(ctx); err != nil {
		return err
	}
	request, err := binding.StartConfiguredModel(ctx, extensionv1.ConfiguredModelRequest_builder{
		Context: nil,
		Selection: extensionv1.ModelSelection_builder{
			ProviderId: new("openai-codex"), ModelId: new("gpt-test"), ReasoningChoice: new("off"),
		}.Build(),
		Instructions: new("extension lifecycle observer"),
		Messages: []*extensionv1.ConfiguredModelMessage{extensionv1.ConfiguredModelMessage_builder{
			Role: new(extensionv1.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_USER), Text: new(lifecycleRequestText),
		}.Build()},
	}.Build())
	if err != nil {
		return err
	}
	result, err := request.Wait(ctx)
	if err != nil {
		return err
	}
	text := lifecycleResultText(result)
	if text == "" {
		return errors.New("configured lifecycle request returned no visible text")
	}
	appendOperation, err := binding.StartAppendExtensionMessage(ctx, extensionv1.AppendExtensionMessageRequest_builder{
		Context: nil, EntryType: new(lifecycleMessageEntryType), Text: new(text),
		Visibility: new(extensionv1.ClientVisibility_CLIENT_VISIBILITY_HIDDEN),
	}.Build())
	if err != nil {
		return err
	}
	appended, err := appendOperation.Wait(ctx)
	if err != nil {
		return err
	}
	if len(appended.GetIssues()) != 0 {
		return errors.New("agent-start append returned a delivery issue")
	}
	return nil
}

// lifecycleResultText joins ordered visible text, refusal, and reasoning blocks.
func lifecycleResultText(result *extensionv1.ConfiguredModelResult) string {
	if result == nil {
		return ""
	}
	parts := make([]string, 0, len(result.GetContent()))
	for _, content := range result.GetContent() {
		switch content.WhichContent() {
		case extensionv1.ConfiguredModelContent_Text_case:
			parts = append(parts, content.GetText().GetText())
		case extensionv1.ConfiguredModelContent_Refusal_case:
			parts = append(parts, content.GetRefusal().GetText())
		case extensionv1.ConfiguredModelContent_Reasoning_case:
			parts = append(parts, content.GetReasoning().GetText())
		case extensionv1.ConfiguredModelContent_ToolCall_case,
			extensionv1.ConfiguredModelContent_Content_not_set_case:
		}
	}
	return strings.Join(parts, "\n")
}
