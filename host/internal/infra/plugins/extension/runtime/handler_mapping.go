package runtime

import (
	"encoding/json/v2"
	"fmt"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// mapNavigationRequest maps navigation behavior and configured summary model selection.
func mapNavigationRequest(request sessiontree.HandlerNavigationRequest) *extensionpb.SessionTreeNavigationRequest {
	builder := extensionpb.SessionTreeNavigationRequest_builder{
		TargetEntryId: new(request.Navigation.TargetEntryID),
		SummaryMode:   new(mapSummaryMode(request.Navigation.SummaryMode)),
		CustomFocus:   nil,
		SummaryModel:  mapModelSelection(request.SummaryModel),
	}
	if customFocus, ok := request.Navigation.CustomFocus.Get(); ok {
		builder.CustomFocus = new(customFocus)
	}
	return builder.Build()
}

// mapNavigationRequestFromProto maps a handler-provided replacement request.
func mapNavigationRequestFromProto(
	request *extensionpb.SessionTreeNavigationRequest,
) sessiontree.HandlerNavigationRequest {
	customFocus := mo.None[string]()
	if request.HasCustomFocus() {
		customFocus = mo.Some(request.GetCustomFocus())
	}
	selection := request.GetSummaryModel()
	mappedSelection := model.Selection{Provider: "", Model: "", ReasoningChoice: ""}
	if selection != nil {
		mappedSelection = model.Selection{
			Provider: model.ProviderID(selection.GetProviderId()), Model: model.ID(selection.GetModelId()),
			ReasoningChoice: model.ReasoningChoice(selection.GetReasoningChoice()),
		}
	}
	return sessiontree.HandlerNavigationRequest{
		Navigation: sessionnavigation.Request{
			TargetEntryID: request.GetTargetEntryId(),
			SummaryMode:   mapSummaryModeFromProto(request.GetSummaryMode()),
			CustomFocus:   customFocus,
		},
		SummaryModel: mappedSelection,
	}
}

// mapSummaryMode maps the internal closed summary mode.
func mapSummaryMode(mode sessionnavigation.SummaryMode) extensionpb.SummaryMode {
	switch mode {
	case sessionnavigation.SummaryModeNoSummary:
		return extensionpb.SummaryMode_SUMMARY_MODE_NO_SUMMARY
	case sessionnavigation.SummaryModeSummarize:
		return extensionpb.SummaryMode_SUMMARY_MODE_SUMMARIZE
	case sessionnavigation.SummaryModeSummarizeWithCustomPrompt:
		return extensionpb.SummaryMode_SUMMARY_MODE_SUMMARIZE_WITH_CUSTOM_PROMPT
	default:
		return extensionpb.SummaryMode_SUMMARY_MODE_UNSPECIFIED
	}
}

// mapSummaryModeFromProto maps known modes and leaves invalid values for composition validation.
func mapSummaryModeFromProto(mode extensionpb.SummaryMode) sessionnavigation.SummaryMode {
	switch mode {
	case extensionpb.SummaryMode_SUMMARY_MODE_NO_SUMMARY:
		return sessionnavigation.SummaryModeNoSummary
	case extensionpb.SummaryMode_SUMMARY_MODE_SUMMARIZE:
		return sessionnavigation.SummaryModeSummarize
	case extensionpb.SummaryMode_SUMMARY_MODE_SUMMARIZE_WITH_CUSTOM_PROMPT:
		return sessionnavigation.SummaryModeSummarizeWithCustomPrompt
	case extensionpb.SummaryMode_SUMMARY_MODE_UNSPECIFIED:
		return 0
	default:
		return 0
	}
}

// mapModelSelection maps one configured model identity without credentials.
func mapModelSelection(selection model.Selection) *extensionpb.ModelSelection {
	return extensionpb.ModelSelection_builder{
		ProviderId: new(string(selection.Provider)), ModelId: new(string(selection.Model)),
		ReasoningChoice: new(string(selection.ReasoningChoice)),
	}.Build()
}

// mapPreparation maps immutable Host-computed navigation context.
func mapPreparation(state sessiontree.HandlerNavigationState) (*extensionpb.SessionTreePreparation, error) {
	entries, err := mapSessionEntries(state.Preparation.AbandonedPath)
	if err != nil {
		return nil, err
	}
	builder := extensionpb.SessionTreePreparation_builder{
		SessionId:               new(state.SessionID),
		PrecedingActiveLeafId:   nil,
		NavigationDestinationId: nil,
		CommonAncestorId:        nil,
		AbandonedEntries:        entries,
	}
	if value, ok := state.PrecedingActiveLeafID.Get(); ok {
		builder.PrecedingActiveLeafId = new(value)
	}
	if value, ok := state.Preparation.DestinationID.Get(); ok {
		builder.NavigationDestinationId = new(value)
	}
	if value, ok := state.Preparation.CommonAncestorID.Get(); ok {
		builder.CommonAncestorId = new(value)
	}
	return builder.Build(), nil
}

// mapSessionEntries maps abandoned entries in their declared root-first order.
func mapSessionEntries(entries []session.Entry) ([]*extensionpb.SessionTreeEntry, error) {
	result := make([]*extensionpb.SessionTreeEntry, 0, len(entries))
	for index := range entries {
		mapped, err := mapSessionEntry(entries[index])
		if err != nil {
			return nil, err
		}
		result = append(result, mapped)
	}
	return result, nil
}

// mapSessionEntry maps one supported provider-neutral or opaque entry projection.
func mapSessionEntry(entry session.Entry) (*extensionpb.SessionTreeEntry, error) {
	//nolint:exhaustruct_v5 // The entry builder sets only the active projected payload.
	builder := extensionpb.SessionTreeEntry_builder{Id: new(entry.ID)}
	if user, present := entry.User.Get(); present {
		builder.User = mapUserMessage(user)
		return builder.Build(), nil
	}
	if response, present := entry.Model.Get(); present {
		mapped, err := mapModelResponse(response)
		if err != nil {
			return nil, fmt.Errorf("map abandoned model entry %q: %w", entry.ID, err)
		}
		builder.Model = mapped
		return builder.Build(), nil
	}
	if result, present := entry.ToolResult.Get(); present {
		builder.ToolResult = mapTreeToolResult(result)
		return builder.Build(), nil
	}
	if summary, present := entry.BranchSummary.Get(); present {
		builder.BranchSummary = extensionpb.SessionTreeBranchSummary_builder{Summary: new(summary.Summary)}.Build()
		return builder.Build(), nil
	}
	if extension, present := entry.Extension.Get(); present {
		builder.Extension = extensionpb.SessionTreeExtensionEntry_builder{
			ExtensionId: new(extension.ExtensionID), EntryType: new(extension.EntryType),
		}.Build()
	}
	return builder.Build(), nil
}

// mapUserMessage maps ordered text and image input blocks.
func mapUserMessage(message session.UserMessage) *extensionpb.SessionTreeUserMessage {
	content := make([]*extensionpb.SessionTreeUserContent, 0, len(message.Content))
	for index := range message.Content {
		item := message.Content[index]
		builder := extensionpb.SessionTreeUserContent_builder{}
		switch item.Kind {
		case model.InputContentText:
			if text, ok := item.Text.Get(); ok {
				builder.Text = new(text)
			}
		case model.InputContentImage:
			mediaType, _ := item.MediaType.Get()
			data, _ := item.Data.Get()
			builder.Image = extensionpb.SessionTreeImage_builder{MediaType: new(mediaType), Data: data}.Build()
		}
		content = append(content, builder.Build())
	}
	return extensionpb.SessionTreeUserMessage_builder{Content: content}.Build()
}

// mapModelResponse maps ordered finalized model content without provider-owned replay context.
func mapModelResponse(response session.ModelResponse) (*extensionpb.SessionTreeModelResponse, error) {
	content := make([]*extensionpb.SessionTreeModelContent, 0, len(response.Content))
	for index := range response.Content {
		item := response.Content[index]
		kind := mapModelContentKind(item.Kind)
		builder := extensionpb.SessionTreeModelContent_builder{
			Kind: new(kind), Text: nil, ToolCall: nil,
		}
		if text, ok := item.Text.Get(); ok {
			builder.Text = new(text)
		}
		if call, ok := item.ToolCall.Get(); ok {
			arguments, err := json.Marshal(call.Arguments, json.Deterministic(true))
			if err != nil {
				return nil, fmt.Errorf("encode tool call %q arguments: %w", call.ID, err)
			}
			builder.ToolCall = extensionpb.SessionTreeToolCall_builder{
				Id: new(call.ID), Name: new(call.Name), ArgumentsJson: arguments,
			}.Build()
		}
		content = append(content, builder.Build())
	}
	return extensionpb.SessionTreeModelResponse_builder{Content: content}.Build(), nil
}

// mapModelContentKind maps provider-neutral response content kinds.
func mapModelContentKind(kind model.ContentKind) extensionpb.SessionTreeModelContentKind {
	switch kind {
	case model.ContentText:
		return extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_TEXT
	case model.ContentRefusal:
		return extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_REFUSAL
	case model.ContentReasoning:
		return extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_REASONING
	case model.ContentToolCall:
		return extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_TOOL_CALL
	default:
		return extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_UNSPECIFIED
	}
}

// mapTreeToolResult maps one terminal result and all model-visible blocks.
func mapTreeToolResult(result session.ToolResult) *extensionpb.SessionTreeToolResult {
	return extensionpb.SessionTreeToolResult_builder{
		CallId: new(result.CallID), ToolName: new(result.ToolName),
		Contents: mapToolResultContentsToProto(result.Contents), IsError: new(result.IsError),
	}.Build()
}

// mapToolResultContentsToProto maps ordered text and image terminal blocks.
func mapToolResultContentsToProto(contents []tool.ResultContent) []*extensionpb.ToolResultContent {
	result := make([]*extensionpb.ToolResultContent, 0, len(contents))
	for index := range contents {
		content := contents[index]
		builder := extensionpb.ToolResultContent_builder{}
		if text, present := content.Text.Get(); present {
			builder.Text = new(text)
			result = append(result, builder.Build())
			continue
		}
		if image, present := content.Image.Get(); present {
			builder.Image = extensionpb.ToolResultImage_builder{
				MediaType: new(image.MediaType), Data: image.Data,
			}.Build()
		}
		result = append(result, builder.Build())
	}
	return result
}

// mapOptionalSummaryResult maps summary-result presence.
func mapOptionalSummaryResult(
	result mo.Option[sessiontree.HandlerBranchSummaryResult],
) *extensionpb.BranchSummaryResult {
	value, ok := result.Get()
	if !ok {
		return nil
	}
	return mapSummaryResult(value)
}

// mapSummaryResult maps extension-produced summary text and optional usage.
func mapSummaryResult(result sessiontree.HandlerBranchSummaryResult) *extensionpb.BranchSummaryResult {
	builder := extensionpb.BranchSummaryResult_builder{Summary: new(result.Summary), Usage: nil}
	if usage, ok := result.Usage.Get(); ok {
		builder.Usage = mapTokenUsage(usage)
	}
	return builder.Build()
}

// mapOptionalSummaryResultFromProto maps summary-result presence from protobuf.
func mapOptionalSummaryResultFromProto(
	result *extensionpb.BranchSummaryResult,
) mo.Option[sessiontree.HandlerBranchSummaryResult] {
	if result == nil {
		return mo.None[sessiontree.HandlerBranchSummaryResult]()
	}
	usage := mo.None[session.TokenUsage]()
	if result.GetUsage() != nil {
		usage = mo.Some(mapTokenUsageFromProto(result.GetUsage()))
	}
	return mo.Some(sessiontree.HandlerBranchSummaryResult{Summary: result.GetSummary(), Usage: usage})
}

// mapTokenUsage maps all normalized usage buckets.
func mapTokenUsage(usage session.TokenUsage) *extensionpb.TokenUsage {
	return extensionpb.TokenUsage_builder{
		InputTokens: new(usage.InputTokens), OutputTokens: new(usage.OutputTokens),
		CacheReadTokens: new(usage.CacheReadTokens), CacheWriteTokens: new(usage.CacheWriteTokens),
		ReasoningTokens: new(usage.ReasoningTokens), TotalTokens: new(usage.TotalTokens),
	}.Build()
}

// mapTokenUsageFromProto maps all normalized usage buckets from protobuf.
func mapTokenUsageFromProto(usage *extensionpb.TokenUsage) session.TokenUsage {
	return session.TokenUsage{
		InputTokens: usage.GetInputTokens(), OutputTokens: usage.GetOutputTokens(),
		CacheReadTokens: usage.GetCacheReadTokens(), CacheWriteTokens: usage.GetCacheWriteTokens(),
		ReasoningTokens: usage.GetReasoningTokens(), TotalTokens: usage.GetTotalTokens(),
	}
}

// mapSessionTreeInvocation maps committed navigation and optional complete summary data.
func mapSessionTreeInvocation(invocation sessiontree.TreeObserverInvocation) *extensionpb.SessionTreeInvocation {
	builder := extensionpb.SessionTreeInvocation_builder{
		SessionId: new(invocation.SessionID), TargetEntryId: new(invocation.TargetEntryID),
		PrecedingActiveLeafId: nil, NavigationDestinationId: nil,
		CommittedActiveLeafId: nil, CreatedSummary: nil,
	}
	if value, ok := invocation.PrecedingActiveLeafID.Get(); ok {
		builder.PrecedingActiveLeafId = new(value)
	}
	if value, ok := invocation.NavigationDestinationID.Get(); ok {
		builder.NavigationDestinationId = new(value)
	}
	if value, ok := invocation.CommittedActiveLeafID.Get(); ok {
		builder.CommittedActiveLeafId = new(value)
	}
	if value, ok := invocation.CreatedSummary.Get(); ok {
		builder.CreatedSummary = mapCommittedSummary(value)
	}
	return builder.Build()
}

// mapCommittedSummary maps one complete committed branch-summary entry.
func mapCommittedSummary(entry session.Entry) *extensionpb.CommittedBranchSummary {
	summary, ok := entry.BranchSummary.Get()
	if !ok {
		return nil
	}
	builder := extensionpb.CommittedBranchSummary_builder{
		EntryId: new(entry.ID), Summary: new(summary.Summary), FirstEntryId: new(summary.FirstEntryID),
		LastEntryId: new(summary.LastEntryID), SummaryModel: mapModelSelection(model.Selection{
			Provider: summary.Provider, Model: summary.Model, ReasoningChoice: summary.ReasoningChoice,
		}),
		Usage: nil, EstimatedCost: nil,
	}
	if usage, present := summary.Usage.Get(); present {
		builder.Usage = mapTokenUsage(usage)
	}
	if cost, present := summary.EstimatedCost.Get(); present {
		builder.EstimatedCost = extensionpb.EstimatedCost_builder{
			Input: new(cost.Input), Output: new(cost.Output), CacheRead: new(cost.CacheRead),
			CacheWrite: new(cost.CacheWrite), Total: new(cost.Total),
		}.Build()
	}
	return builder.Build()
}
