package plugin

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"

	"github.com/samber/mo"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// mapRestoredTranscript rebuilds public transcript lines without replaying lifecycle events.
//
//nolint:gocyclo // The closed transcript union maps each payload to distinct presentation lines.
func mapRestoredTranscript(entries []*uiv1.SessionEntry) ([]Transcript, error) {
	lines := make([]Transcript, 0, len(entries))
	for _, entry := range entries {
		if user := entry.GetUser(); user != nil {
			contents, text, err := mapRestoredContents(user.GetContent())
			if err != nil {
				return nil, err
			}
			lines = append(lines, Transcript{
				Kind: TranscriptUser, ToolName: mo.None[string](), Status: mo.None[string](),
				Text: mo.Some(text), Contents: mo.Some(contents),
			})
			continue
		}
		if response := entry.GetModel(); response != nil {
			mapped, err := mapRestoredModelResponse(response)
			if err != nil {
				return nil, err
			}
			lines = append(lines, mapped...)
			continue
		}
		if result := entry.GetToolResult(); result != nil {
			mapped, err := mapRestoredToolResult(result)
			if err != nil {
				return nil, err
			}
			lines = append(lines, mapped)
			continue
		}
		if message := entry.GetExtensionMessage(); message != nil {
			if !message.HasExtensionId() || !message.HasEntryType() || !message.HasText() || !message.HasVisibility() {
				return nil, errors.New("restored extension message is incomplete")
			}
			switch message.GetVisibility() {
			case uiv1.ClientVisibility_CLIENT_VISIBILITY_VISIBLE:
				lines = append(lines, Transcript{
					Kind: TranscriptUser, ToolName: mo.None[string](), Status: mo.None[string](),
					Text: mo.Some(message.GetText()), Contents: mo.Some([]Content{{
						Text: mo.Some(message.GetText()), MediaType: mo.None[string](), Data: mo.None[[]byte](),
					}}),
				})
			case uiv1.ClientVisibility_CLIENT_VISIBILITY_HIDDEN:
				continue
			case uiv1.ClientVisibility_CLIENT_VISIBILITY_UNSPECIFIED:
				return nil, errors.New("restored extension message visibility is unspecified")
			default:
				return nil, errors.New("restored extension message visibility is unknown")
			}
			continue
		}
		if summary := entry.GetBranchSummary(); summary != nil {
			lines = append(lines, Transcript{
				Kind: TranscriptBranchSummary, ToolName: mo.None[string](), Status: mo.None[string](),
				Text: mo.Some(summary.GetSummary()), Contents: mo.None[[]Content](),
			})
		}
	}
	return lines, nil
}

// mapRestoredContents maps ordered user text and images with owned image bytes.
func mapRestoredContents(contents []*uiv1.UserContent) ([]Content, string, error) {
	mapped := make([]Content, 0, len(contents))
	var text strings.Builder
	for index, content := range contents {
		if content == nil {
			return nil, "", fmt.Errorf("restored user content %d is missing", index)
		}
		switch content.WhichContent() {
		case uiv1.UserContent_Text_case:
			value := content.GetText()
			mapped = append(mapped, Content{
				Text: mo.Some(value), MediaType: mo.None[string](), Data: mo.None[[]byte](),
			})
			text.WriteString(value)
		case uiv1.UserContent_Image_case:
			image := content.GetImage()
			if image == nil || image.GetMediaType() == "" {
				return nil, "", fmt.Errorf("restored user image %d is invalid", index)
			}
			data := bytes.Clone(image.GetData())
			mapped = append(mapped, Content{
				Text: mo.None[string](), MediaType: mo.Some(image.GetMediaType()), Data: mo.Some(data),
			})
			text.WriteString(imagePlaceholder(image.GetMediaType(), len(data)))
		case uiv1.UserContent_Content_not_set_case:
			return nil, "", fmt.Errorf("restored user content %d is missing", index)
		default:
			return nil, "", fmt.Errorf("restored user content %d is invalid", index)
		}
	}
	return mapped, text.String(), nil
}

// mapRestoredModelResponse keeps stored visible model content and terminal failures in display order.
func mapRestoredModelResponse(response *uiv1.ModelResponse) ([]Transcript, error) {
	lines := make([]Transcript, 0, len(response.GetContent()))
	for _, content := range response.GetContent() {
		if call := content.GetToolCall(); call != nil {
			arguments, err := json.Marshal(call.GetArguments().AsMap())
			if err != nil {
				return nil, fmt.Errorf("map restored tool call: %w", err)
			}
			lines = append(lines, Transcript{
				Kind: TranscriptToolStatus, ToolName: mo.Some(call.GetName()),
				Status: mo.Some("arguments"), Text: mo.Some(string(arguments)),
				Contents: mo.None[[]Content](),
			})
			continue
		}
		kind := TranscriptModel
		switch content.GetKind() {
		case uiv1.ModelContentKind_MODEL_CONTENT_KIND_UNSPECIFIED,
			uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT:
		case uiv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL:
			kind = TranscriptRefusal
		case uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING:
			kind = TranscriptReasoning
		}
		lines = append(lines, Transcript{
			Kind: kind, ToolName: mo.None[string](), Status: mo.None[string](),
			Text: mo.Some(content.GetText()), Contents: mo.None[[]Content](),
		})
	}
	if outcome := response.GetOutcome(); outcome == "aborted" || outcome == "failed" {
		if response.HasErrorMessage() {
			lines = append(lines, Transcript{
				Kind: TranscriptError, ToolName: mo.None[string](), Status: mo.None[string](),
				Text: mo.Some(response.GetErrorMessage()), Contents: mo.None[[]Content](),
			})
		}
	}
	return lines, nil
}

// mapRestoredToolResult uses the same terminal line kinds as live tool completion.
func mapRestoredToolResult(result *uiv1.ToolResult) (Transcript, error) {
	contents, err := mapContents(result.GetContents(), true)
	if err != nil {
		return Transcript{}, fmt.Errorf("map restored tool result: %w", err)
	}
	kind := TranscriptToolDone
	if result.GetIsError() {
		kind = TranscriptToolError
	}
	return Transcript{
		Kind: kind, ToolName: mo.Some(result.GetToolName()), Status: mo.None[string](),
		Text: mo.Some(restoredToolResultText(contents)), Contents: mo.Some(contents),
	}, nil
}

// restoredToolResultText combines public tool-result content for transcript display.
func restoredToolResultText(contents []Content) string {
	var result strings.Builder
	for _, content := range contents {
		if text, present := content.Text.Get(); present {
			result.WriteString(text)
			continue
		}
		mediaType, hasMediaType := content.MediaType.Get()
		data, hasData := content.Data.Get()
		if hasMediaType && hasData {
			result.WriteString(imagePlaceholder(mediaType, len(data)))
		}
	}
	return result.String()
}

// imagePlaceholder describes image content that a plain terminal cannot display.
func imagePlaceholder(mediaType string, size int) string {
	return fmt.Sprintf("[image %s, %d bytes]", mediaType, size)
}

// mapSessionStatistics validates and reconstructs optional token and cost values from the UI wire boundary.
func mapSessionStatistics(statistics *uiv1.SessionStatistics) (SessionStatistics, error) {
	if statistics == nil {
		return SessionStatistics{}, errors.New("map session statistics: value is required")
	}
	result := SessionStatistics{
		UserMessages: int(statistics.GetUserMessages()), ModelResponses: int(statistics.GetModelResponses()),
		ToolCalls: int(statistics.GetToolCalls()), ToolResults: int(statistics.GetToolResults()),
		TotalMessages: int(statistics.GetTotalMessages()), TokenUsage: mo.None[TokenUsage](),
		EstimatedCost: mo.None[EstimatedCost](), CostBreakdown: nil,
	}
	if tokens := statistics.GetTokens(); tokens != nil {
		result.TokenUsage = mo.Some(TokenUsage{
			InputTokens: tokens.GetInputTokens(), OutputTokens: tokens.GetOutputTokens(),
			CacheReadTokens: tokens.GetCacheReadTokens(), CacheWriteTokens: tokens.GetCacheWriteTokens(),
			ReasoningTokens: tokens.GetReasoningTokens(), TotalTokens: tokens.GetTotalTokens(),
		})
	}
	if cost := statistics.GetEstimatedCost(); cost != nil {
		mapped, err := mapEstimatedCost(cost)
		if err != nil {
			return SessionStatistics{}, err
		}
		result.EstimatedCost = mo.Some(mapped)
	}
	result.CostBreakdown = make([]ProviderModelCost, len(statistics.GetCostBreakdown()))
	for groupIndex, group := range statistics.GetCostBreakdown() {
		if group == nil || !group.HasProviderId() || !group.HasModelId() {
			return SessionStatistics{}, errors.New("map provider-model cost: identity is required")
		}
		mapped := ProviderModelCost{
			ProviderID: group.GetProviderId(), ModelID: group.GetModelId(),
			EstimatedCost: mo.None[EstimatedCost](),
		}
		if cost := group.GetEstimatedCost(); cost != nil {
			groupCost, err := mapEstimatedCost(cost)
			if err != nil {
				return SessionStatistics{}, err
			}
			mapped.EstimatedCost = mo.Some(groupCost)
		}
		result.CostBreakdown[groupIndex] = mapped
	}
	return result, nil
}

// mapEstimatedCost requires all five persisted values and preserves configured zero.
func mapEstimatedCost(cost *uiv1.EstimatedCost) (EstimatedCost, error) {
	if cost == nil || !cost.HasInput() || !cost.HasOutput() || !cost.HasCacheRead() ||
		!cost.HasCacheWrite() || !cost.HasTotal() {
		return EstimatedCost{}, errors.New("map estimated cost: all values are required")
	}
	return EstimatedCost{
		Input: cost.GetInput(), Output: cost.GetOutput(), CacheRead: cost.GetCacheRead(),
		CacheWrite: cost.GetCacheWrite(), Total: cost.GetTotal(),
	}, nil
}

// mapSessionInfo validates required identity, project, and timestamp fields while preserving optional values.
func mapSessionInfo(value *uiv1.SessionInfo) (SessionInfo, error) {
	if value == nil || !value.HasId() || !value.HasWorkingDirectory() ||
		!value.HasCreatedTime() || !value.HasUpdateTime() {
		return SessionInfo{}, errors.New("session information is incomplete")
	}
	return SessionInfo{
		ID:               value.GetId(),
		Name:             value.GetName(),
		NamePresent:      value.HasName(),
		WorkingDirectory: value.GetWorkingDirectory(),
		StoragePath:      value.GetStoragePath(),
		StoragePresent:   value.HasStoragePath(),
		CreatedAt:        value.GetCreatedTime().AsTime(),
		UpdatedAt:        value.GetUpdateTime().AsTime(),
	}, nil
}

// mapSessionSummary validates one selector row and preserves first-user-text presence.
func mapSessionSummary(value *uiv1.SessionSummary) (SessionSummary, error) {
	if value == nil || !value.HasInfo() || !value.HasTotalMessages() {
		return SessionSummary{}, errors.New("session summary is incomplete")
	}
	info, err := mapSessionInfo(value.GetInfo())
	if err != nil {
		return SessionSummary{}, err
	}
	return SessionSummary{
		Info:          info,
		FirstUserText: value.GetFirstUserText(),
		TextPresent:   value.HasFirstUserText(),
		TotalMessages: value.GetTotalMessages(),
	}, nil
}
