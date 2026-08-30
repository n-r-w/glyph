package sessions

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"time"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// encodeEntry writes one compact record so one append always occupies one JSONL line.
func encodeEntry(entry session.Entry) ([]byte, error) {
	variants := []bool{
		entry.Information.IsSome(), entry.User.IsSome(), entry.Model.IsSome(),
		entry.ToolResult.IsSome(), entry.Extension.IsSome(), entry.BranchSummary.IsSome(),
	}
	selected := 0
	for _, present := range variants {
		if present {
			selected++
		}
	}
	if entry.ID == "" || selected != 1 {
		return nil, errors.New("invalid session entry")
	}
	if information, ok := entry.Information.Get(); ok {
		if information.Name == "" {
			return nil, errors.New("invalid session entry")
		}
		return encodeLine(informationRecord{
			Type: recordTypeSessionInfo, ID: entry.ID,
			CreatedAt: entry.CreatedAt.Format(time.RFC3339Nano), Name: information.Name,
		})
	}
	if summary, ok := entry.BranchSummary.Get(); ok {
		return encodeBranchSummaryEntry(entry, summary)
	}
	if user, ok := entry.User.Get(); ok {
		message, err := encodeUserMessage(user)
		if err != nil {
			return nil, err
		}
		return encodeLine(userRecord{
			Type: recordTypeUser, ID: entry.ID, ParentID: entry.ParentID,
			CreatedAt: entry.CreatedAt.Format(time.RFC3339Nano), Message: &message,
		})
	}
	if response, ok := entry.Model.Get(); ok {
		return encodeModelEntry(entry, response)
	}
	if result, ok := entry.ToolResult.Get(); ok {
		return encodeToolResultEntry(entry, result)
	}
	extension := entry.Extension.MustGet()
	if extension.ExtensionID == "" || extension.EntryType == "" || !jsontext.Value(extension.Data).IsValid() {
		return nil, errors.New("invalid extension entry")
	}
	// Clone opaque extension bytes before framing them as compact JSON.
	return encodeLine(extensionRecord{
		Type: recordTypeExtension, ID: entry.ID, ParentID: entry.ParentID,
		CreatedAt:   entry.CreatedAt.Format(time.RFC3339Nano),
		ExtensionID: extension.ExtensionID, EntryType: extension.EntryType,
		Data: jsontext.Value(bytes.Clone(extension.Data)),
	})
}

// encodeBranchSummaryEntry validates and encodes one complete summary payload.
func encodeBranchSummaryEntry(entry session.Entry, summary session.BranchSummaryEntry) ([]byte, error) {
	if summary.Summary == "" || summary.FirstEntryID == "" || summary.LastEntryID == "" ||
		summary.Provider == "" || summary.Model == "" || !summary.ReasoningChoice.Valid() {
		return nil, errors.New("invalid branch summary entry")
	}
	var usage *sessionUsageRecord
	if value, present := summary.Usage.Get(); present {
		if !value.Valid() {
			return nil, errors.New("invalid branch summary usage")
		}
		usage = &sessionUsageRecord{
			InputTokens: value.InputTokens, OutputTokens: value.OutputTokens,
			CacheReadTokens: value.CacheReadTokens, CacheWriteTokens: value.CacheWriteTokens,
			ReasoningTokens: value.ReasoningTokens, TotalTokens: value.TotalTokens,
		}
	}
	var estimatedCost *estimatedCostRecord
	if cost, present := summary.EstimatedCost.Get(); present {
		if !cost.Valid() {
			return nil, errors.New("invalid branch summary cost")
		}
		estimatedCost = &estimatedCostRecord{
			Input: new(cost.Input), Output: new(cost.Output), CacheRead: new(cost.CacheRead),
			CacheWrite: new(cost.CacheWrite), Total: new(cost.Total),
		}
	}
	return encodeLine(branchSummaryRecord{
		Type: recordTypeBranchSummary, ID: entry.ID, ParentID: entry.ParentID,
		CreatedAt: entry.CreatedAt.Format(time.RFC3339Nano), Summary: summary.Summary,
		FirstEntryID: summary.FirstEntryID, LastEntryID: summary.LastEntryID,
		Provider: string(summary.Provider), Model: string(summary.Model), ReasoningChoice: summary.ReasoningChoice,
		Usage: usage, EstimatedCost: estimatedCost,
	})
}

func encodeUserMessage(message model.Message) (messageRecord, error) {
	var content []inputContentRecord
	if message.Content != nil {
		content = make([]inputContentRecord, 0, len(message.Content))
	}
	for index := range message.Content {
		item := message.Content[index]
		switch item.Kind {
		case model.InputContentText:
			text, present := item.Text.Get()
			if !present || item.MediaType.IsSome() || item.Data.IsSome() {
				return messageRecord{}, errors.New("invalid user text content")
			}
			content = append(content, inputContentRecord{
				Kind: model.InputContentText, Text: new(text), MediaType: nil, Data: nil,
			})
		case model.InputContentImage:
			mediaType, hasMediaType := item.MediaType.Get()
			data, hasData := item.Data.Get()
			if item.Text.IsSome() || !hasMediaType || mediaType == "" || !hasData {
				return messageRecord{}, errors.New("invalid user image content")
			}
			encodedData, err := encodeBytes(data)
			if err != nil {
				return messageRecord{}, errors.New("invalid user image content")
			}
			content = append(content, inputContentRecord{
				Kind: model.InputContentImage, Text: nil, MediaType: new(mediaType), Data: encodedData,
			})
		default:
			return messageRecord{}, errors.New("invalid user content")
		}
	}
	return messageRecord{Content: content}, nil
}

func encodeModelEntry(entry session.Entry, response model.Response) ([]byte, error) {
	record, err := encodeModelResponse(response)
	if err != nil {
		return nil, err
	}
	var estimatedCost *estimatedCostRecord
	if cost, present := entry.EstimatedCost.Get(); present {
		estimatedCost = &estimatedCostRecord{
			Input: new(cost.Input), Output: new(cost.Output), CacheRead: new(cost.CacheRead),
			CacheWrite: new(cost.CacheWrite), Total: new(cost.Total),
		}
	}
	return encodeLine(modelRecord{
		Type: recordTypeModel, ID: entry.ID, ParentID: entry.ParentID,
		CreatedAt: entry.CreatedAt.Format(time.RFC3339Nano), Response: record, EstimatedCost: estimatedCost,
	})
}

func encodeToolResultEntry(entry session.Entry, result agent.ToolResult) ([]byte, error) {
	if result.CallID == "" || result.ToolName == "" || entry.Information.IsSome() ||
		entry.User.IsSome() || entry.Model.IsSome() {
		return nil, errors.New("invalid tool result entry")
	}
	var contents []toolResultContentRecord
	if result.Contents != nil {
		contents = make([]toolResultContentRecord, 0, len(result.Contents))
	}
	for index := range result.Contents {
		content, err := encodeToolResultContent(result.Contents[index])
		if err != nil {
			return nil, err
		}
		contents = append(contents, content)
	}
	return encodeLine(toolResultRecord{
		Type: recordTypeToolResult, ID: entry.ID, ParentID: entry.ParentID,
		CreatedAt: entry.CreatedAt.Format(time.RFC3339Nano),
		Result: toolResultValue{
			CallID: result.CallID, ToolName: result.ToolName, Contents: contents, IsError: result.IsError,
		},
	})
}

func encodeToolResultContent(content tool.ResultContent) (toolResultContentRecord, error) {
	switch content.Kind {
	case tool.ResultContentText:
		text, present := content.Text.Get()
		if !present || content.Image.IsSome() {
			return toolResultContentRecord{}, errors.New("invalid tool result content")
		}
		return toolResultContentRecord{
			Kind: tool.ResultContentText, Text: new(text), MediaType: nil, Data: nil,
		}, nil
	case tool.ResultContentImage:
		image, present := content.Image.Get()
		if !present || content.Text.IsSome() || image.MediaType == "" {
			return toolResultContentRecord{}, errors.New("invalid tool result content")
		}
		encodedData, err := encodeBytes(image.Data)
		if err != nil {
			return toolResultContentRecord{}, errors.New("invalid tool result content")
		}
		return toolResultContentRecord{
			Kind: tool.ResultContentImage, Text: nil, MediaType: new(image.MediaType), Data: encodedData,
		}, nil
	default:
		return toolResultContentRecord{}, errors.New("invalid tool result content")
	}
}

// encodeBytes keeps present nil and empty byte slices distinct in repository JSON.
func encodeBytes(data []byte) (jsontext.Value, error) {
	encoded, err := json.Marshal(bytes.Clone(data), json.FormatNilSliceAsNull(true))
	if err != nil {
		return nil, err
	}
	return jsontext.Value(encoded), nil
}

// encodeModelResponse preserves terminal continuation fields and their option presence.
func encodeModelResponse(response model.Response) (modelResponseRecord, error) {
	outcome, present := response.Outcome.Get()
	if !present || !validOutcome(outcome) {
		return modelResponseRecord{}, errors.New("invalid model entry")
	}
	var content []modelContentRecord
	if response.Content != nil {
		content = make([]modelContentRecord, 0, len(response.Content))
	}
	for index := range response.Content {
		record, err := encodeModelContent(&response.Content[index])
		if err != nil {
			return modelResponseRecord{}, err
		}
		content = append(content, record)
	}
	var diagnostics []diagnosticRecord
	if response.Diagnostics != nil {
		diagnostics = make([]diagnosticRecord, len(response.Diagnostics))
		for index := range response.Diagnostics {
			diagnostics[index] = diagnosticRecord{
				Code: response.Diagnostics[index].Code, Message: response.Diagnostics[index].Message,
			}
		}
	}
	result := modelResponseRecord{
		Content: content, Outcome: outcome,
		ErrorMessage:  response.ErrorMessage.ToPointer(),
		Provider:      (*string)(response.Provider.ToPointer()),
		Model:         (*string)(response.Model.ToPointer()),
		ResponseModel: (*string)(response.ResponseModel.ToPointer()),
		ResponseID:    response.ResponseID.ToPointer(), Usage: nil, Diagnostics: diagnostics,
	}
	if usage, ok := response.Usage.Get(); ok {
		result.Usage = &usageRecord{
			InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
			CachedInputTokens: usage.CachedInputTokens, CacheWriteTokens: usage.CacheWriteTokens,
			ReasoningTokens: usage.ReasoningTokens, TotalTokens: usage.TotalTokens,
		}
	}
	return result, nil
}

// encodeModelContent stores public text, tool calls, and opaque replay context in response order.
func encodeModelContent(item *model.Content) (modelContentRecord, error) {
	if !item.Final {
		return modelContentRecord{}, errors.New("model content is not final")
	}
	if err := validateModelContentShape(
		item.Kind, item.Text.IsSome(), item.ProviderContext.IsSome(), item.ToolCall.IsSome(),
	); err != nil {
		return modelContentRecord{}, err
	}
	record := modelContentRecord{
		Kind: item.Kind, Text: item.Text.ToPointer(), ProviderContext: nil, ToolCall: nil,
	}
	if contextValue, ok := item.ProviderContext.Get(); ok {
		record.ProviderContext = &providerContextRecord{
			ProviderID: string(contextValue.Source.ProviderID), API: contextValue.Source.API,
			Model:            string(contextValue.Source.Model),
			CompatibilityKey: contextValue.Source.CompatibilityKey.ToPointer(),
			Payload:          bytes.Clone(contextValue.Payload),
		}
	}
	if call, ok := item.ToolCall.Get(); ok {
		if call.ID == "" || call.Name == "" {
			return modelContentRecord{}, errors.New("invalid model tool call content")
		}
		record.ToolCall = &toolCallRecord{ID: call.ID, Name: call.Name, Arguments: call.Arguments}
	}
	return record, nil
}

func validateModelContentShape(
	kind model.ContentKind,
	hasText bool,
	hasProviderContext bool,
	hasToolCall bool,
) error {
	var valid bool
	switch kind {
	case model.ContentText, model.ContentRefusal:
		valid = hasText && !hasProviderContext && !hasToolCall
	case model.ContentToolCall:
		valid = !hasText && !hasProviderContext && hasToolCall
	case model.ContentReasoning:
		valid = !hasToolCall && (hasText || hasProviderContext)
	default:
		return errors.New("unsupported model content")
	}
	if !valid {
		return errors.New("invalid model content shape")
	}
	return nil
}

// encodeLine adds the record delimiter included in each synchronized append.
func encodeLine(value any) ([]byte, error) {
	encoded, err := json.Marshal(
		value,
		json.Deterministic(true),
		json.FormatNilMapAsNull(true),
		json.FormatNilSliceAsNull(true),
	)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}
