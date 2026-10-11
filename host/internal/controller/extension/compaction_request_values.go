package extension

import (
	"errors"
	"fmt"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// DecodeCompactionRequestModel maps a supplied model descriptor.
func DecodeCompactionRequestModel(value *extensionpb.ModelDescriptor) model.Descriptor {
	if value == nil {
		return model.Descriptor{}
	}
	modalities := make([]model.InputModality, len(value.GetInputModalities()))
	for index, input := range value.GetInputModalities() {
		switch input {
		case extensionpb.InputModality_INPUT_MODALITY_TEXT:
			modalities[index] = model.InputModalityText
		case extensionpb.InputModality_INPUT_MODALITY_IMAGE:
			modalities[index] = model.InputModalityImage
		case extensionpb.InputModality_INPUT_MODALITY_UNSPECIFIED:
			modalities[index] = model.InputModality("")
		}
	}
	reasoning := value.GetReasoning()
	choices := make([]model.ReasoningChoice, len(reasoning.GetChoices()))
	for index, choice := range reasoning.GetChoices() {
		choices[index] = model.ReasoningChoice(choice)
	}
	descriptor := model.Descriptor{
		Provider: model.ProviderID(value.GetProviderId()), Model: model.ID(value.GetModelId()), Input: modalities,
		ContextWindow: value.GetContextWindow(), MaxTokens: value.GetMaxTokens(),
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: reasoning.GetSupported(), Choices: choices,
			Default: model.ReasoningChoice(reasoning.GetDefaultChoice()),
		},
		ToolCapabilities: model.ToolCapabilities{
			StrictJSONSchema: value.GetTools().GetStrictJsonSchema(),
			Grammar: model.GrammarCapabilities{
				Lark:  value.GetTools().GetLark(),
				Regex: value.GetTools().GetRegex(),
			},
		},
		Pricing: mo.None[model.Pricing](),
	}
	if pricing := value.GetPricing(); pricing != nil {
		tiers := make([]model.PricingTier, len(pricing.GetTiers()))
		for index, tier := range pricing.GetTiers() {
			tiers[index] = model.PricingTier{
				InputTokensAbove: tier.GetInputTokensAbove(), Input: tier.GetInput(), Output: tier.GetOutput(),
				CacheRead: tier.GetCacheRead(), CacheWrite: tier.GetCacheWrite(),
			}
		}
		descriptor.Pricing = mo.Some(model.Pricing{
			Input: pricing.GetInput(), Output: pricing.GetOutput(), CacheRead: pricing.GetCacheRead(),
			CacheWrite: pricing.GetCacheWrite(), Tiers: tiers,
		})
	}
	return descriptor
}

// decodeRequestContentKind maps the closed public model-content kind.
func decodeRequestContentKind(value extensionpb.SessionTreeModelContentKind) model.ContentKind {
	switch value {
	case extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_TEXT:
		return model.ContentText
	case extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_REFUSAL:
		return model.ContentRefusal
	case extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_REASONING:
		return model.ContentReasoning
	case extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_TOOL_CALL:
		return model.ContentToolCall
	case extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_UNSPECIFIED:
		return 0
	default:
		return 0
	}
}

// DecodeCompactionRequestEntry maps a handler-supplied public branch entry.
func DecodeCompactionRequestEntry(value *extensionpb.SessionTreeEntry) (session.Entry, error) {
	// publicProjectionExtensionID identifies the synthetic producer of a public summary projection.
	const publicProjectionExtensionID = "public-projection"
	if value == nil {
		return session.Entry{}, errors.New("session tree entry is missing")
	}
	entry := session.Entry{
		ID: value.GetId(), ParentID: mo.None[string](), CreatedAt: time.Time{},
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](),
		Extension: mo.None[session.ExtensionEnvelope](), ExtensionMessage: mo.None[session.ExtensionMessage](),
		EstimatedCost: mo.None[session.EstimatedCost](), BranchSummary: mo.None[session.BranchSummaryEntry](),
		Compaction: mo.None[session.CompactionEntry](),
	}
	if user := value.GetUser(); user != nil {
		message, err := decodeRequestUser(user)
		if err != nil {
			return session.Entry{}, err
		}
		entry.User = mo.Some(message)
	}
	if response := value.GetModel(); response != nil {
		contents, err := decodeRequestModelContents(response.GetContent())
		if err != nil {
			return session.Entry{}, err
		}
		entry.Model = mo.Some(model.Response{
			Content: contents, Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
			Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
			ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
		})
	}
	if result := value.GetToolResult(); result != nil {
		mapped, err := decodeRequestToolResult(result)
		if err != nil {
			return session.Entry{}, err
		}
		entry.ToolResult = mo.Some(mapped)
	}
	if summary := value.GetBranchSummary(); summary != nil {
		entry.BranchSummary = mo.Some(session.BranchSummaryEntry{
			Summary: summary.GetSummary(), FirstEntryID: value.GetId(), LastEntryID: value.GetId(),
			Source: session.BranchSummarySource{
				ExtensionID: mo.Some(publicProjectionExtensionID), Model: mo.None[session.BranchSummaryModelSource](),
			}, EstimatedCost: mo.None[session.EstimatedCost](),
		})
	}
	if compaction := value.GetCompaction(); compaction != nil {
		entry.Compaction = DecodeCompactionPrevious(compaction)
	}
	if extensionEntry := value.GetExtension(); extensionEntry != nil {
		entry.Extension = mo.Some(session.ExtensionEnvelope{
			ExtensionID: extensionEntry.GetExtensionId(), EntryType: extensionEntry.GetEntryType(), Data: nil,
		})
	}
	if message := value.GetExtensionMessage(); message != nil {
		entry.ExtensionMessage = mo.Some(session.ExtensionMessage{
			ExtensionID: message.GetExtensionId(), EntryType: message.GetEntryType(), Text: message.GetText(),
			Visibility: session.ClientVisibility(message.GetVisibility()),
		})
	}
	if value.WhichContent() == extensionpb.SessionTreeEntry_Content_not_set_case {
		return session.Entry{}, errors.New("session tree entry content is missing")
	}
	return entry, nil
}

// decodeRequestModelContents validates and maps closed public model content variants.
func decodeRequestModelContents(values []*extensionpb.SessionTreeModelContent) ([]model.Content, error) {
	contents := make([]model.Content, len(values))
	for index, content := range values {
		if content == nil {
			return nil, errors.New("model content payload is missing")
		}
		kind := decodeRequestContentKind(content.GetKind())
		if kind == 0 {
			return nil, errors.New("model content kind is unspecified")
		}
		hasText := content.HasText()
		call := content.GetToolCall()
		contents[index] = model.Content{
			Kind: kind, Text: mo.None[string](), Final: true,
			ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
		}
		if kind == model.ContentToolCall {
			if call == nil || hasText {
				return nil, errors.New("model tool-call payload is missing or ambiguous")
			}
			arguments, err := model.NewToolCallArguments(call.GetArgumentsJson())
			if err != nil {
				return nil, fmt.Errorf("validate tool call arguments: %w", err)
			}
			contents[index].ToolCall = mo.Some(
				model.ToolCall{ID: call.GetId(), Name: call.GetName(), Arguments: arguments},
			)
		} else {
			if !hasText || call != nil {
				return nil, errors.New("model text payload is missing or ambiguous")
			}
			contents[index].Text = mo.Some(content.GetText())
		}
	}
	return contents, nil
}

// DecodeCompactionSource decodes producer metadata with its supplied field presence.
func DecodeCompactionSource(wire *extensionpb.BranchSummarySource) session.BranchSummarySource {
	source := session.BranchSummarySource{
		ExtensionID: mo.None[string](),
		Model:       mo.None[session.BranchSummaryModelSource](),
	}
	if wire == nil {
		return source
	}
	if wire.HasExtensionId() {
		source.ExtensionID = mo.Some(wire.GetExtensionId())
	} else if value := wire.GetModel(); value != nil {
		selection := value.GetSelection()
		usage := mo.None[session.TokenUsage]()
		if value.GetUsage() != nil {
			usage = mo.Some(decodeCompactionUsage(value.GetUsage()))
		}
		source.Model = mo.Some(session.BranchSummaryModelSource{
			Selection: model.Selection{
				Provider: model.ProviderID(selection.GetProviderId()), Model: model.ID(selection.GetModelId()),
				ReasoningChoice: model.ReasoningChoice(selection.GetReasoningChoice()),
			}, Usage: usage,
		})
	}
	return source
}

// decodeCompactionUsage maps all normalized usage buckets from protobuf.
func decodeCompactionUsage(usage *extensionpb.TokenUsage) session.TokenUsage {
	return session.TokenUsage{
		InputTokens: usage.GetInputTokens(), OutputTokens: usage.GetOutputTokens(),
		CacheReadTokens: usage.GetCacheReadTokens(), CacheWriteTokens: usage.GetCacheWriteTokens(),
		ReasoningTokens: usage.GetReasoningTokens(), TotalTokens: usage.GetTotalTokens(),
	}
}

// decodeRequestUser supplies detached public user content.
func decodeRequestUser(user *extensionpb.SessionTreeUserMessage) (model.Message, error) {
	contents := make([]model.InputContent, len(user.GetContent()))
	for index, content := range user.GetContent() {
		if content == nil || content.WhichContent() == extensionpb.SessionTreeUserContent_Content_not_set_case {
			return model.Message{}, errors.New("user content payload is missing")
		}
		contents[index] = model.InputContent{
			Kind:      model.InputContentText,
			Text:      mo.None[string](),
			MediaType: mo.None[string](),
			Data:      mo.None[[]byte](),
		}
		if content.HasText() {
			contents[index].Text = mo.Some(content.GetText())
		} else if image := content.GetImage(); image != nil {
			contents[index].Kind = model.InputContentImage
			contents[index].MediaType = mo.Some(image.GetMediaType())
			contents[index].Data = mo.Some(append([]byte(nil), image.GetData()...))
		}
	}
	return model.Message{Content: contents}, nil
}

// decodeRequestToolResult supplies detached public tool-result content.
func decodeRequestToolResult(result *extensionpb.SessionTreeToolResult) (agent.ToolResult, error) {
	contents := make([]tool.ResultContent, len(result.GetContents()))
	for index, content := range result.GetContents() {
		if content == nil || content.WhichContent() == extensionpb.ToolResultContent_Content_not_set_case {
			return agent.ToolResult{}, errors.New("tool result content payload is missing")
		}
		contents[index] = tool.ResultContent{
			Kind: tool.ResultContentText, Text: mo.None[string](), Image: mo.None[tool.ResultImage](),
		}
		if content.HasText() {
			contents[index].Text = mo.Some(content.GetText())
		} else if image := content.GetImage(); image != nil {
			contents[index].Kind = tool.ResultContentImage
			contents[index].Image = mo.Some(tool.ResultImage{
				MediaType: image.GetMediaType(), Data: append([]byte(nil), image.GetData()...),
			})
		}
	}
	return agent.ToolResult{
		CallID:   result.GetCallId(),
		ToolName: result.GetToolName(),
		Contents: contents,
		IsError:  result.GetIsError(),
	}, nil
}
