package extension

import (
	"errors"
	"fmt"

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

// DecodeCompactionRequestEntry maps a handler-supplied prepared record and its original source.
func DecodeCompactionRequestEntry(value *extensionpb.CompactionHistoryRecord) (session.PreparedHistoryEntry, error) {
	if value == nil {
		return session.PreparedHistoryEntry{}, errors.New("prepared history record is missing")
	}
	if value.GetSourceEntryId() == "" || value.GetSourcePosition() < 0 || value.GetPrivatePayloadBytes() < 0 {
		return session.PreparedHistoryEntry{}, errors.New("prepared history source or private byte count is invalid")
	}
	sourceKind, sourceErr := decodeHistorySourceKind(value.GetSourceKind())
	if sourceErr != nil {
		return session.PreparedHistoryEntry{}, sourceErr
	}
	history := agent.HistoryEntry{
		Kind:       0,
		User:       mo.None[model.Message](),
		Model:      mo.None[model.Response](),
		ToolResult: mo.None[agent.ToolResult](),
	}
	switch {
	case value.GetUser() != nil:
		message, err := decodeRequestUser(value.GetUser())
		if err != nil {
			return session.PreparedHistoryEntry{}, err
		}
		history.Kind = agent.HistoryEntryUser
		history.User = mo.Some(message)
	case value.GetModel() != nil:
		contents, err := decodeRequestModelContents(value.GetModel().GetContent())
		if err != nil {
			return session.PreparedHistoryEntry{}, err
		}
		history.Kind = agent.HistoryEntryModel
		history.Model = mo.Some(
			model.Response{
				Content:       contents,
				Outcome:       mo.Some(model.OutcomeStop),
				ErrorMessage:  mo.None[string](),
				Provider:      mo.None[model.ProviderID](),
				Model:         mo.None[model.ID](),
				ResponseModel: mo.None[model.ID](),
				ResponseID:    mo.None[string](),
				Usage:         mo.None[model.Usage](),
				Diagnostics:   nil,
			},
		)
	case value.GetToolResult() != nil:
		result, err := decodeRequestToolResult(value.GetToolResult())
		if err != nil {
			return session.PreparedHistoryEntry{}, err
		}
		history.Kind = agent.HistoryEntryToolResult
		history.ToolResult = mo.Some(result)
	default:
		return session.PreparedHistoryEntry{}, errors.New("prepared history content is missing")
	}
	return session.PreparedHistoryEntry{
		History:         history,
		SourceID:        value.GetSourceEntryId(),
		SourcePosition:  value.GetSourcePosition(),
		SourceKind:      sourceKind,
		PreviousSummary: value.GetPreviousSummary(),
	}, nil
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

// DecodeBranchSummarySource decodes producer metadata.
func DecodeBranchSummarySource(wire *extensionpb.BranchSummarySource) session.BranchSummarySource {
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

// DecodeCompactionSource decodes producer selections and reported usage.
func DecodeCompactionSource(wire *extensionpb.CompactionSource) session.CompactionSource {
	source := session.CompactionSource{ExtensionID: mo.None[string](), Requests: nil}
	if wire == nil {
		return source
	}
	if wire.HasExtensionId() {
		source.ExtensionID = mo.Some(wire.GetExtensionId())
	} else if records := wire.GetModelRequests(); records != nil {
		source.Requests = make([]session.CompactionModelRequest, len(records.GetRequests()))
		for index, record := range records.GetRequests() {
			selection := record.GetSelection()
			request := session.CompactionModelRequest{
				Selection: model.Selection{
					Provider: model.ProviderID(selection.GetProviderId()), Model: model.ID(selection.GetModelId()),
					ReasoningChoice: model.ReasoningChoice(selection.GetReasoningChoice()),
				}, Usage: mo.None[session.TokenUsage](), EstimatedCost: mo.None[session.EstimatedCost](),
			}
			if usage := record.GetUsage(); usage != nil {
				request.Usage = mo.Some(decodeCompactionUsage(usage))
			}
			source.Requests[index] = request
		}
	}
	return source
}
