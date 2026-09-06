package runtime

import (
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// mapLifecycleResponse maps one terminal response and excludes opaque provider context.
func mapLifecycleResponse(response model.Response) (*extensionpb.ConfiguredModelResult, error) {
	contents := make([]*extensionpb.ConfiguredModelContent, 0, len(response.Content))
	for index := range response.Content {
		mapped, visible, err := mapLifecycleContent(response.Content[index])
		if err != nil {
			return nil, err
		}
		if visible {
			contents = append(contents, mapped)
		}
	}
	result := new(extensionpb.ConfiguredModelResult)
	result.SetContent(contents)
	if outcome, present := response.Outcome.Get(); present {
		mappedOutcome, err := mapLifecycleModelOutcome(outcome)
		if err != nil {
			return nil, err
		}
		result.SetOutcome(mappedOutcome)
	}
	if value, present := response.ErrorMessage.Get(); present {
		result.SetErrorMessage(value)
	}
	if value, present := response.Provider.Get(); present {
		result.SetProviderId(string(value))
	}
	if value, present := response.Model.Get(); present {
		result.SetModelId(string(value))
	}
	if value, present := response.ResponseModel.Get(); present {
		result.SetResponseModelId(string(value))
	}
	if value, present := response.ResponseID.Get(); present {
		result.SetResponseId(value)
	}
	if usage, present := response.Usage.Get(); present {
		result.SetUsage(extensionpb.ConfiguredModelUsage_builder{
			InputTokens: new(usage.InputTokens), OutputTokens: new(usage.OutputTokens),
			CachedInputTokens: new(usage.CachedInputTokens), CacheWriteTokens: new(usage.CacheWriteTokens),
			ReasoningTokens: new(usage.ReasoningTokens), TotalTokens: new(usage.TotalTokens),
		}.Build())
	}
	diagnostics := make([]*extensionpb.ConfiguredModelDiagnostic, len(response.Diagnostics))
	for index, diagnostic := range response.Diagnostics {
		diagnostics[index] = extensionpb.ConfiguredModelDiagnostic_builder{
			Code: new(diagnostic.Code), Message: new(diagnostic.Message),
		}.Build()
	}
	result.SetDiagnostics(diagnostics)
	return result, nil
}

// mapLifecycleContent maps visible response content and excludes provider-context-only reasoning.
func mapLifecycleContent(content model.Content) (*extensionpb.ConfiguredModelContent, bool, error) {
	mapped := new(extensionpb.ConfiguredModelContent)
	switch content.Kind {
	case model.ContentText, model.ContentRefusal, model.ContentReasoning:
		text, present := content.Text.Get()
		if !present {
			if content.Kind == model.ContentReasoning && content.ProviderContext.IsSome() {
				return nil, false, nil
			}
			return nil, false, errors.New("lifecycle public text is missing")
		}
		value := extensionpb.ConfiguredModelText_builder{Text: new(text)}.Build()
		switch content.Kind {
		case model.ContentText:
			mapped.SetText(value)
		case model.ContentRefusal:
			mapped.SetRefusal(value)
		case model.ContentReasoning:
			mapped.SetReasoning(value)
		case model.ContentToolCall:
		}
	case model.ContentToolCall:
		call, present := content.ToolCall.Get()
		if !present {
			return nil, false, errors.New("lifecycle tool call is missing")
		}
		arguments, err := json.Marshal(call.Arguments)
		if err != nil {
			return nil, false, fmt.Errorf("encode lifecycle tool call arguments: %w", err)
		}
		mapped.SetToolCall(extensionpb.ConfiguredModelToolCall_builder{
			Id: new(call.ID), Name: new(call.Name), ArgumentsJson: arguments,
		}.Build())
	default:
		return nil, false, fmt.Errorf("unknown lifecycle content kind %d", content.Kind)
	}
	return mapped, true, nil
}

// mapLifecycleAgentOutcome maps one closed agent outcome to stable public text.
func mapLifecycleAgentOutcome(outcome agent.RunOutcome) (string, error) {
	switch outcome {
	case agent.RunOutcomeCompleted:
		return "completed", nil
	case agent.RunOutcomeAborted:
		return "aborted", nil
	case agent.RunOutcomeFailed:
		return "failed", nil
	default:
		return "", fmt.Errorf("unknown lifecycle agent outcome %d", outcome)
	}
}

// mapLifecycleModelOutcome maps one closed terminal model outcome.
func mapLifecycleModelOutcome(outcome model.Outcome) (extensionpb.ConfiguredModelOutcome, error) {
	switch outcome {
	case model.OutcomeStop:
		return extensionpb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_STOP, nil
	case model.OutcomeToolUse:
		return extensionpb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_TOOL_USE, nil
	case model.OutcomeLength:
		return extensionpb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_LENGTH, nil
	case model.OutcomeAborted:
		return extensionpb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_ABORTED, nil
	case model.OutcomeFailed:
		return extensionpb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_FAILED, nil
	default:
		return extensionpb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_UNSPECIFIED,
			fmt.Errorf("unknown lifecycle model outcome %d", outcome)
	}
}

// mapLifecycleProgressChannel maps one closed progress channel.
func mapLifecycleProgressChannel(channel tool.ProgressChannel) (extensionpb.ProgressChannel, error) {
	switch channel {
	case tool.ProgressChannelStatus:
		return extensionpb.ProgressChannel_PROGRESS_CHANNEL_STATUS, nil
	case tool.ProgressChannelStdout:
		return extensionpb.ProgressChannel_PROGRESS_CHANNEL_STDOUT, nil
	case tool.ProgressChannelStderr:
		return extensionpb.ProgressChannel_PROGRESS_CHANNEL_STDERR, nil
	default:
		return extensionpb.ProgressChannel_PROGRESS_CHANNEL_UNSPECIFIED, fmt.Errorf(
			"unknown progress channel %d",
			channel,
		)
	}
}

// mapLifecycleToolContents maps ordered terminal tool blocks.
func mapLifecycleToolContents(contents []tool.ResultContent) ([]*extensionpb.ToolResultContent, error) {
	mapped := make([]*extensionpb.ToolResultContent, 0, len(contents))
	for _, content := range contents {
		item := new(extensionpb.ToolResultContent)
		switch content.Kind {
		case tool.ResultContentText:
			text, present := content.Text.Get()
			if !present {
				return nil, errors.New("lifecycle tool text is missing")
			}
			item.SetText(text)
		case tool.ResultContentImage:
			image, present := content.Image.Get()
			if !present {
				return nil, errors.New("lifecycle tool image is missing")
			}
			item.SetImage(
				extensionpb.ToolResultImage_builder{MediaType: new(image.MediaType), Data: image.Data}.Build(),
			)
		default:
			return nil, fmt.Errorf("unknown lifecycle tool content kind %d", content.Kind)
		}
		mapped = append(mapped, item)
	}
	return mapped, nil
}
