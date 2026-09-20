package runtime

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

// mapCompactionMarker projects one preceding or committed compaction marker.
func mapCompactionMarker(value session.CompactionEntry) *extensionpb.SessionTreeCompaction {
	builder := extensionpb.SessionTreeCompaction_builder{
		Summary: new(value.Summary), FirstKeptEntryId: new(value.FirstKeptEntryID),
		Source: mapSummarySource(value.Source), EstimatedCost: nil, Details: nil,
	}
	if cost, present := value.EstimatedCost.Get(); present {
		builder.EstimatedCost = mapCompactionCost(cost)
	}
	if details, present := value.Details.Get(); present {
		builder.Details = append([]byte(nil), details...)
	}
	return builder.Build()
}

// mapOptionalCompactionMarker maps a present preceding marker.
func mapOptionalCompactionMarker(value *extensionpb.SessionTreeCompaction) mo.Option[session.CompactionEntry] {
	if value == nil {
		return mo.None[session.CompactionEntry]()
	}
	entry := session.CompactionEntry{
		Summary: value.GetSummary(), FirstKeptEntryID: value.GetFirstKeptEntryId(),
		Source: mapSummarySourceFromProto(value.GetSource()), EstimatedCost: mo.None[session.EstimatedCost](),
		Details: mo.None[[]byte](),
	}
	if value.GetEstimatedCost() != nil {
		entry.EstimatedCost = mo.Some(mapCompactionCostFromProto(value.GetEstimatedCost()))
	}
	if value.GetDetails() != nil {
		entry.Details = mo.Some(append([]byte(nil), value.GetDetails()...))
	}
	return mo.Some(entry)
}

// mapCompactionCost projects persisted USD buckets.
func mapCompactionCost(value session.EstimatedCost) *extensionpb.EstimatedCost {
	return extensionpb.EstimatedCost_builder{
		Input: new(value.Input), Output: new(value.Output), CacheRead: new(value.CacheRead),
		CacheWrite: new(value.CacheWrite), Total: new(value.Total),
	}.Build()
}

// mapCompactionCostFromProto maps persisted USD buckets without validation.
func mapCompactionCostFromProto(value *extensionpb.EstimatedCost) session.EstimatedCost {
	return session.EstimatedCost{
		Input: value.GetInput(), Output: value.GetOutput(), CacheRead: value.GetCacheRead(),
		CacheWrite: value.GetCacheWrite(), Total: value.GetTotal(),
	}
}

// mapCompactionModel projects every provider-neutral descriptor field.
func mapCompactionModel(descriptor model.Descriptor) *extensionpb.ModelDescriptor {
	modalities := make([]extensionpb.InputModality, len(descriptor.Input))
	for index, input := range descriptor.Input {
		switch input {
		case model.InputModalityText:
			modalities[index] = extensionpb.InputModality_INPUT_MODALITY_TEXT
		case model.InputModalityImage:
			modalities[index] = extensionpb.InputModality_INPUT_MODALITY_IMAGE
		}
	}
	choices := make([]string, len(descriptor.ReasoningCapabilities.Choices))
	for index, choice := range descriptor.ReasoningCapabilities.Choices {
		choices[index] = string(choice)
	}
	var pricing *extensionpb.ModelPricing
	if configured, present := descriptor.Pricing.Get(); present {
		tiers := make([]*extensionpb.PricingTier, len(configured.Tiers))
		for index, tier := range configured.Tiers {
			tiers[index] = extensionpb.PricingTier_builder{
				InputTokensAbove: new(tier.InputTokensAbove), Input: new(tier.Input), Output: new(tier.Output),
				CacheRead: new(tier.CacheRead), CacheWrite: new(tier.CacheWrite),
			}.Build()
		}
		pricing = extensionpb.ModelPricing_builder{
			Input: new(configured.Input), Output: new(configured.Output), CacheRead: new(configured.CacheRead),
			CacheWrite: new(configured.CacheWrite), Tiers: tiers,
		}.Build()
	}
	return extensionpb.ModelDescriptor_builder{
		ProviderId: new(string(descriptor.Provider)), ModelId: new(string(descriptor.Model)),
		InputModalities: modalities, ContextWindow: new(descriptor.ContextWindow), MaxTokens: new(descriptor.MaxTokens),
		Reasoning: extensionpb.ReasoningCapabilities_builder{
			Supported: new(descriptor.ReasoningCapabilities.Supported), Choices: choices,
			DefaultChoice: new(string(descriptor.ReasoningCapabilities.Default)),
		}.Build(),
		Tools: extensionpb.ToolCapabilities_builder{
			StrictJsonSchema: new(descriptor.ToolCapabilities.StrictJSONSchema),
			Lark: new(
				descriptor.ToolCapabilities.Grammar.Lark,
			),
			Regex: new(descriptor.ToolCapabilities.Grammar.Regex),
		}.Build(),
		Pricing: pricing,
	}.Build()
}

// mapCompactionModelFromProto maps one replacement descriptor without applying catalog policy.
func mapCompactionModelFromProto(value *extensionpb.ModelDescriptor) model.Descriptor {
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

// mapCompactionContentKind maps the closed public model-content kind.
func mapCompactionContentKind(value extensionpb.SessionTreeModelContentKind) model.ContentKind {
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

// mapSessionEntryFromProto maps one handler-supplied request entry without repairing malformed values.
//
//nolint:gocognit,gocyclo // The flat closed entry union keeps all public variants explicit.
func mapSessionEntryFromProto(value *extensionpb.SessionTreeEntry) (session.Entry, error) {
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
		contents := make([]model.InputContent, len(user.GetContent()))
		for index, content := range user.GetContent() {
			if content == nil || content.WhichContent() == extensionpb.SessionTreeUserContent_Content_not_set_case {
				return session.Entry{}, errors.New("user content payload is missing")
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
		entry.User = mo.Some(model.Message{Content: contents})
	}
	if response := value.GetModel(); response != nil {
		contents, err := mapCompactionModelContents(response.GetContent())
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
		contents := make([]tool.ResultContent, len(result.GetContents()))
		for index, content := range result.GetContents() {
			if content == nil || content.WhichContent() == extensionpb.ToolResultContent_Content_not_set_case {
				return session.Entry{}, errors.New("tool result content payload is missing")
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
		entry.ToolResult = mo.Some(agent.ToolResult{
			CallID:   result.GetCallId(),
			ToolName: result.GetToolName(),
			Contents: contents,
			IsError:  result.GetIsError(),
		})
	}
	if summary := value.GetBranchSummary(); summary != nil {
		entry.BranchSummary = mo.Some(session.BranchSummaryEntry{
			Summary: summary.GetSummary(), FirstEntryID: value.GetId(), LastEntryID: value.GetId(),
			Source: session.BranchSummarySource{
				ExtensionID: mo.Some("public-projection"), Model: mo.None[session.BranchSummaryModelSource](),
			}, EstimatedCost: mo.None[session.EstimatedCost](),
		})
	}
	if compaction := value.GetCompaction(); compaction != nil {
		entry.Compaction = mapOptionalCompactionMarker(compaction)
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

// mapCompactionModelContents validates and maps closed public model content variants.
func mapCompactionModelContents(values []*extensionpb.SessionTreeModelContent) ([]model.Content, error) {
	contents := make([]model.Content, len(values))
	for index, content := range values {
		if content == nil {
			return nil, errors.New("model content payload is missing")
		}
		kind := mapCompactionContentKind(content.GetKind())
		if kind == 0 {
			return nil, errors.New("model content kind is unspecified")
		}
		if kind == model.ContentToolCall && (content.GetToolCall() == nil || content.HasText()) {
			return nil, errors.New("model tool-call payload is missing or ambiguous")
		}
		if kind != model.ContentToolCall && (!content.HasText() || content.GetToolCall() != nil) {
			return nil, errors.New("model text payload is missing or ambiguous")
		}
		contents[index] = model.Content{
			Kind: kind, Text: mo.None[string](), Final: true,
			ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
		}
		if content.HasText() {
			contents[index].Text = mo.Some(content.GetText())
		}
		if call := content.GetToolCall(); call != nil {
			arguments, err := model.NewToolCallArguments(call.GetArgumentsJson())
			if err != nil {
				return nil, fmt.Errorf("validate tool call arguments: %w", err)
			}
			contents[index].ToolCall = mo.Some(
				model.ToolCall{ID: call.GetId(), Name: call.GetName(), Arguments: arguments},
			)
		}
	}
	return contents, nil
}
