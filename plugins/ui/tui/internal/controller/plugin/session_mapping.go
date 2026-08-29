package plugin

import (
	"bytes"

	"encoding/json/v2"
	"errors"
	"fmt"

	"strings"

	"github.com/samber/mo"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// sessionEvent initializes fields that are absent from session lifecycle frames.
func sessionEvent(
	kind presentationdomain.EventKind,
	info mo.Option[presentationdomain.SessionInfo],
	sessions []presentationdomain.SessionSummary,
	restored []presentationdomain.Line,
	statistics mo.Option[presentationdomain.SessionStatistics],
) presentationdomain.Event {
	return presentationdomain.Event{
		RestoredTranscript:   restored,
		Kind:                 kind,
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Text:                 mo.None[string](),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          info,
		Sessions:             sessions,
		SessionStatistics:    statistics,
	}
}

// mapRestoredTranscript rebuilds public transcript lines without replaying lifecycle events.
func mapRestoredTranscript(entries []*uiv1.SessionEntry) ([]presentationdomain.Line, error) {
	lines := make([]presentationdomain.Line, 0, len(entries))
	for _, entry := range entries {
		if user := entry.GetUser(); user != nil {
			contents, text, err := mapRestoredContents(user.GetContent())
			if err != nil {
				return nil, err
			}
			lines = append(lines, presentationdomain.Line{
				Kind: presentationdomain.LineUser, ToolName: mo.None[string](), Status: mo.None[string](),
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
		}
	}
	return lines, nil
}

// mapRestoredContents maps ordered user text and images with owned image bytes.
func mapRestoredContents(contents []*uiv1.UserContent) ([]presentationdomain.Content, string, error) {
	mapped := make([]presentationdomain.Content, 0, len(contents))
	var text strings.Builder
	for index, content := range contents {
		if content == nil {
			return nil, "", fmt.Errorf("restored user content %d is missing", index)
		}
		switch content.WhichContent() {
		case uiv1.UserContent_Text_case:
			value := content.GetText()
			mapped = append(mapped, presentationdomain.Content{
				Text: mo.Some(value), MediaType: mo.None[string](), Data: mo.None[[]byte](),
			})
			text.WriteString(value)
		case uiv1.UserContent_Image_case:
			image := content.GetImage()
			if image == nil || image.GetMediaType() == "" {
				return nil, "", fmt.Errorf("restored user image %d is invalid", index)
			}
			data := bytes.Clone(image.GetData())
			mapped = append(mapped, presentationdomain.Content{
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

// mapRestoredModelResponse keeps stored model content and diagnostics in display order.
func mapRestoredModelResponse(response *uiv1.ModelResponse) ([]presentationdomain.Line, error) {
	lines := make([]presentationdomain.Line, 0, len(response.GetContent()))
	for _, content := range response.GetContent() {
		if call := content.GetToolCall(); call != nil {
			arguments, err := json.Marshal(call.GetArguments().AsMap())
			if err != nil {
				return nil, fmt.Errorf("map restored tool call: %w", err)
			}
			lines = append(lines, presentationdomain.Line{
				Kind: presentationdomain.LineToolStatus, ToolName: mo.Some(call.GetName()),
				Status: mo.Some("arguments"), Text: mo.Some(string(arguments)),
				Contents: mo.None[[]presentationdomain.Content](),
			})
			continue
		}
		kind := presentationdomain.LineModel
		switch content.GetKind() {
		case uiv1.ModelContentKind_MODEL_CONTENT_KIND_UNSPECIFIED,
			uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT:
		case uiv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL:
			kind = presentationdomain.LineRefusal
		case uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING:
			kind = presentationdomain.LineReasoning
		}
		lines = append(lines, presentationdomain.Line{
			Kind: kind, ToolName: mo.None[string](), Status: mo.None[string](),
			Text: mo.Some(content.GetText()), Contents: mo.None[[]presentationdomain.Content](),
		})
	}
	for _, diagnostic := range response.GetDiagnostics() {
		lines = append(lines, presentationdomain.Line{
			Kind: presentationdomain.LineInformation, ToolName: mo.None[string](), Status: mo.None[string](),
			Text:     mo.Some(diagnostic.GetCode() + ": " + diagnostic.GetMessage()),
			Contents: mo.None[[]presentationdomain.Content](),
		})
	}
	if outcome := response.GetOutcome(); outcome == "aborted" || outcome == "failed" {
		if response.HasErrorMessage() {
			lines = append(lines, presentationdomain.Line{
				Kind: presentationdomain.LineError, ToolName: mo.None[string](), Status: mo.None[string](),
				Text: mo.Some(response.GetErrorMessage()), Contents: mo.None[[]presentationdomain.Content](),
			})
		}
	}
	return lines, nil
}

// mapRestoredToolResult uses the same terminal line kinds as live tool completion.
func mapRestoredToolResult(result *uiv1.ToolResult) (presentationdomain.Line, error) {
	contents, err := mapContents(result.GetContents(), true)
	if err != nil {
		return presentationdomain.Line{}, fmt.Errorf("map restored tool result: %w", err)
	}
	kind := presentationdomain.LineToolDone
	if result.GetIsError() {
		kind = presentationdomain.LineToolError
	}
	return presentationdomain.Line{
		Kind: kind, ToolName: mo.Some(result.GetToolName()), Status: mo.None[string](),
		Text: mo.Some(restoredToolResultText(contents)), Contents: mo.Some(contents),
	}, nil
}

func restoredToolResultText(contents []presentationdomain.Content) string {
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

func imagePlaceholder(mediaType string, size int) string {
	return fmt.Sprintf("[image %s, %d bytes]", mediaType, size)
}

// mapSessionStatistics validates and reconstructs optional token and cost values from the UI wire boundary.
func mapSessionStatistics(statistics *uiv1.SessionStatistics) (presentationdomain.SessionStatistics, error) {
	if statistics == nil {
		return presentationdomain.SessionStatistics{}, errors.New("map session statistics: value is required")
	}
	result := presentationdomain.SessionStatistics{
		UserMessages: int(statistics.GetUserMessages()), ModelResponses: int(statistics.GetModelResponses()),
		ToolCalls: int(statistics.GetToolCalls()), ToolResults: int(statistics.GetToolResults()),
		TotalMessages: int(statistics.GetTotalMessages()), TokenUsage: mo.None[presentationdomain.TokenUsage](),
		EstimatedCost: mo.None[presentationdomain.EstimatedCost](), CostBreakdown: nil,
	}
	if tokens := statistics.GetTokens(); tokens != nil {
		result.TokenUsage = mo.Some(presentationdomain.TokenUsage{
			InputTokens: tokens.GetInputTokens(), OutputTokens: tokens.GetOutputTokens(),
			CacheReadTokens: tokens.GetCacheReadTokens(), CacheWriteTokens: tokens.GetCacheWriteTokens(),
			ReasoningTokens: tokens.GetReasoningTokens(), TotalTokens: tokens.GetTotalTokens(),
		})
	}
	if cost := statistics.GetEstimatedCost(); cost != nil {
		mapped, err := mapEstimatedCost(cost)
		if err != nil {
			return presentationdomain.SessionStatistics{}, err
		}
		result.EstimatedCost = mo.Some(mapped)
	}
	result.CostBreakdown = make([]presentationdomain.ProviderModelCost, len(statistics.GetCostBreakdown()))
	for groupIndex, group := range statistics.GetCostBreakdown() {
		if group == nil || !group.HasProviderId() || !group.HasModelId() {
			return presentationdomain.SessionStatistics{}, errors.New("map provider-model cost: identity is required")
		}
		mapped := presentationdomain.ProviderModelCost{
			ProviderID: group.GetProviderId(), ModelID: group.GetModelId(),
			EstimatedCost: mo.None[presentationdomain.EstimatedCost](),
		}
		if cost := group.GetEstimatedCost(); cost != nil {
			groupCost, err := mapEstimatedCost(cost)
			if err != nil {
				return presentationdomain.SessionStatistics{}, err
			}
			mapped.EstimatedCost = mo.Some(groupCost)
		}
		result.CostBreakdown[groupIndex] = mapped
	}
	return result, nil
}

// mapEstimatedCost requires all five persisted values and preserves configured zero.
func mapEstimatedCost(cost *uiv1.EstimatedCost) (presentationdomain.EstimatedCost, error) {
	if cost == nil || !cost.HasInput() || !cost.HasOutput() || !cost.HasCacheRead() ||
		!cost.HasCacheWrite() || !cost.HasTotal() {
		return presentationdomain.EstimatedCost{}, errors.New("map estimated cost: all values are required")
	}
	return presentationdomain.EstimatedCost{
		Input: cost.GetInput(), Output: cost.GetOutput(), CacheRead: cost.GetCacheRead(),
		CacheWrite: cost.GetCacheWrite(), Total: cost.GetTotal(),
	}, nil
}

// mapSessionInfo validates required identity, project, and timestamp fields while preserving optional values.
func mapSessionInfo(value *uiv1.SessionInfo) (presentationdomain.SessionInfo, error) {
	if value == nil || !value.HasId() || !value.HasWorkingDirectory() ||
		!value.HasCreatedTime() || !value.HasUpdateTime() {
		return presentationdomain.SessionInfo{}, errors.New("session information is incomplete")
	}
	return presentationdomain.SessionInfo{
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
func mapSessionSummary(value *uiv1.SessionSummary) (presentationdomain.SessionSummary, error) {
	if value == nil || !value.HasInfo() || !value.HasTotalMessages() {
		return presentationdomain.SessionSummary{}, errors.New("session summary is incomplete")
	}
	info, err := mapSessionInfo(value.GetInfo())
	if err != nil {
		return presentationdomain.SessionSummary{}, err
	}
	return presentationdomain.SessionSummary{
		Info:          info,
		FirstUserText: value.GetFirstUserText(),
		TextPresent:   value.HasFirstUserText(),
		TotalMessages: value.GetTotalMessages(),
	}, nil
}
