package sessions

import (
	"bytes"

	"encoding/json"
	"errors"
	"fmt"
	"io"

	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// decodeEntry selects one strict version 1 record without exposing repository DTOs.
func decodeEntry(data []byte) (session.Entry, error) {
	var kind recordType
	// The discriminator selects the closed record DTO. The selected DTO then performs strict decoding.
	if err := json.Unmarshal(data, &kind); err != nil {
		return session.Entry{}, fmt.Errorf("decode session entry discriminator: %w", err)
	}
	if err := validateEntryRequiredFields(data, kind.Type); err != nil {
		return session.Entry{}, fmt.Errorf("validate required session fields: %w", err)
	}
	switch kind.Type {
	case "session_info":
		var record informationRecord
		if err := decodeRecord(data, &record); err != nil {
			return session.Entry{}, err
		}
		entryTime, err := time.Parse(time.RFC3339Nano, record.CreatedAt)
		if err != nil {
			return session.Entry{}, fmt.Errorf("parse session information entry timestamp: %w", err)
		}
		if record.ID == "" || record.Name == "" {
			return session.Entry{}, errors.New("invalid session information entry")
		}
		return session.Entry{
			ID: record.ID, CreatedAt: entryTime, Information: mo.Some(session.Information{Name: record.Name}),
			User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
			ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
			EstimatedCost: mo.None[session.EstimatedCost](),
		}, nil
	case "user":
		return decodeUser(data)
	case "model":
		return decodeModel(data)
	case "tool_result":
		return decodeToolResult(data)
	case "extension":
		return decodeExtension(data)
	default:
		return session.Entry{}, errors.New("invalid session entry")
	}
}

func decodeUser(data []byte) (session.Entry, error) {
	var record userRecord
	if err := decodeRecord(data, &record); err != nil {
		return session.Entry{}, err
	}
	entryTime, err := time.Parse(time.RFC3339Nano, record.CreatedAt)
	if err != nil {
		return session.Entry{}, fmt.Errorf("parse user entry timestamp: %w", err)
	}
	if record.ID == "" {
		return session.Entry{}, errors.New("invalid user entry")
	}
	if record.Message == nil {
		return session.Entry{}, errors.New("invalid user entry")
	}
	var content []model.InputContent
	if record.Message.Content != nil {
		content = make([]model.InputContent, 0, len(record.Message.Content))
	}
	for index := range record.Message.Content {
		item, decodeErr := decodeInputContent(record.Message.Content[index])
		if decodeErr != nil {
			return session.Entry{}, decodeErr
		}
		content = append(content, item)
	}
	return session.Entry{
		ID: record.ID, CreatedAt: entryTime, Information: mo.None[session.Information](),
		User: mo.Some(model.Message{Content: content}), Model: mo.None[session.ModelResponse](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		EstimatedCost: mo.None[session.EstimatedCost](),
	}, nil
}

func decodeInputContent(record inputContentRecord) (model.InputContent, error) {
	switch record.Kind {
	case model.InputContentText:
		if record.Text == nil || record.MediaType != nil || record.Data != nil {
			return model.InputContent{}, errors.New("invalid user text content")
		}
		return model.InputContent{
			Kind: model.InputContentText, Text: mo.Some(*record.Text),
			MediaType: mo.None[string](), Data: mo.None[[]byte](),
		}, nil
	case model.InputContentImage:
		if record.Text != nil || record.MediaType == nil || *record.MediaType == "" || record.Data == nil {
			return model.InputContent{}, errors.New("invalid user image content")
		}
		image, decodeErr := decodeBytes(record.Data)
		if decodeErr != nil {
			return model.InputContent{}, fmt.Errorf("user image data: %w", decodeErr)
		}
		return model.InputContent{
			Kind: model.InputContentImage, Text: mo.None[string](),
			MediaType: mo.Some(*record.MediaType), Data: mo.Some(image),
		}, nil
	default:
		return model.InputContent{}, errors.New("invalid user content")
	}
}

func decodeExtension(data []byte) (session.Entry, error) {
	var record extensionRecord
	if err := decodeRecord(data, &record); err != nil {
		return session.Entry{}, err
	}
	entryTime, err := time.Parse(time.RFC3339Nano, record.CreatedAt)
	if err != nil {
		return session.Entry{}, fmt.Errorf("parse extension entry timestamp: %w", err)
	}
	if record.ID == "" || record.ExtensionID == "" || record.EntryType == "" ||
		len(record.Data) == 0 || !json.Valid(record.Data) {
		return session.Entry{}, errors.New("invalid extension entry")
	}
	return session.Entry{
		ID: record.ID, CreatedAt: entryTime, Information: mo.None[session.Information](),
		User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.Some(session.ExtensionEnvelope{
			ExtensionID: record.ExtensionID, EntryType: record.EntryType, Data: bytes.Clone(record.Data),
		}), EstimatedCost: mo.None[session.EstimatedCost](),
	}, nil
}

func decodeModel(data []byte) (session.Entry, error) {
	var record modelRecord
	if err := decodeRecord(data, &record); err != nil {
		return session.Entry{}, err
	}
	entryTime, err := time.Parse(time.RFC3339Nano, record.CreatedAt)
	if err != nil {
		return session.Entry{}, fmt.Errorf("parse model entry timestamp: %w", err)
	}
	if record.ID == "" || !validOutcome(record.Response.Outcome) {
		return session.Entry{}, errors.New("invalid model entry")
	}
	response, err := decodeModelResponse(record.Response)
	if err != nil {
		return session.Entry{}, err
	}
	estimatedCost, err := decodeEstimatedCost(record.EstimatedCost)
	if err != nil {
		return session.Entry{}, err
	}
	return session.Entry{
		ID: record.ID, CreatedAt: entryTime, Information: mo.None[session.Information](),
		User: mo.None[session.UserMessage](), Model: mo.Some(response), ToolResult: mo.None[session.ToolResult](),
		Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: estimatedCost,
	}, nil
}

func decodeToolResult(data []byte) (session.Entry, error) {
	var record toolResultRecord
	if err := decodeRecord(data, &record); err != nil {
		return session.Entry{}, err
	}
	entryTime, err := time.Parse(time.RFC3339Nano, record.CreatedAt)
	if err != nil {
		return session.Entry{}, fmt.Errorf("parse tool result entry timestamp: %w", err)
	}
	if record.ID == "" || record.Result.CallID == "" || record.Result.ToolName == "" {
		return session.Entry{}, errors.New("invalid tool result entry")
	}
	var contents []tool.ResultContent
	if record.Result.Contents != nil {
		contents = make([]tool.ResultContent, 0, len(record.Result.Contents))
	}
	for index := range record.Result.Contents {
		content, decodeErr := decodeToolResultContent(record.Result.Contents[index])
		if decodeErr != nil {
			return session.Entry{}, decodeErr
		}
		contents = append(contents, content)
	}
	return session.Entry{
		ID: record.ID, CreatedAt: entryTime, Information: mo.None[session.Information](),
		User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
		ToolResult: mo.Some(agent.ToolResult{
			CallID: record.Result.CallID, ToolName: record.Result.ToolName,
			Contents: contents, IsError: record.Result.IsError,
		}),
		Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost](),
	}, nil
}

// decodeEstimatedCost preserves configured zero and rejects incomplete persisted cost objects.
func decodeEstimatedCost(record *estimatedCostRecord) (mo.Option[session.EstimatedCost], error) {
	if record == nil {
		return mo.None[session.EstimatedCost](), nil
	}
	if record.Input == nil || record.Output == nil || record.CacheRead == nil ||
		record.CacheWrite == nil || record.Total == nil {
		return mo.None[session.EstimatedCost](), errors.New("invalid estimated cost")
	}
	return mo.Some(session.EstimatedCost{
		Input: *record.Input, Output: *record.Output, CacheRead: *record.CacheRead,
		CacheWrite: *record.CacheWrite, Total: *record.Total,
	}), nil
}

func decodeToolResultContent(record toolResultContentRecord) (tool.ResultContent, error) {
	switch record.Kind {
	case tool.ResultContentText:
		if record.Text == nil || record.MediaType != nil || record.Data != nil {
			return tool.ResultContent{}, errors.New("invalid tool result text content")
		}
		return tool.ResultContent{
			Kind: tool.ResultContentText, Text: mo.Some(*record.Text), Image: mo.None[tool.ResultImage](),
		}, nil
	case tool.ResultContentImage:
		if record.Text != nil || record.MediaType == nil || *record.MediaType == "" || record.Data == nil {
			return tool.ResultContent{}, errors.New("invalid tool result image content")
		}
		data, err := decodeBytes(record.Data)
		if err != nil {
			return tool.ResultContent{}, fmt.Errorf("tool result image data: %w", err)
		}
		return tool.ResultContent{
			Kind: tool.ResultContentImage, Text: mo.None[string](),
			Image: mo.Some(tool.ResultImage{MediaType: *record.MediaType, Data: data}),
		}, nil
	default:
		return tool.ResultContent{}, errors.New("invalid tool result content")
	}
}

func decodeBytes(data json.RawMessage) ([]byte, error) {
	var decoded []byte
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

// decodeModelResponse reconstructs provider history without exposing persistence DTOs.
func decodeModelResponse(record modelResponseRecord) (model.Response, error) {
	var content []model.Content
	if record.Content != nil {
		content = make([]model.Content, 0, len(record.Content))
	}
	for index := range record.Content {
		value, err := decodeModelContent(&record.Content[index])
		if err != nil {
			return model.Response{}, err
		}
		content = append(content, value)
	}
	var diagnostics []model.Diagnostic
	if record.Diagnostics != nil {
		diagnostics = make([]model.Diagnostic, len(record.Diagnostics))
		for index := range record.Diagnostics {
			diagnostics[index] = model.Diagnostic{
				Code: record.Diagnostics[index].Code, Message: record.Diagnostics[index].Message,
			}
		}
	}
	result := model.Response{
		Content: content, Outcome: mo.Some(record.Outcome), ErrorMessage: pointerStringOption(record.ErrorMessage),
		Provider:      pointerProviderIDOption(record.Provider),
		Model:         pointerModelIDOption(record.Model),
		ResponseModel: pointerModelIDOption(record.ResponseModel),
		ResponseID:    pointerStringOption(record.ResponseID), Usage: mo.None[model.Usage](), Diagnostics: diagnostics,
	}
	if record.Usage != nil {
		result.Usage = mo.Some(model.Usage{
			InputTokens: record.Usage.InputTokens, OutputTokens: record.Usage.OutputTokens,
			CachedInputTokens: record.Usage.CachedInputTokens, CacheWriteTokens: record.Usage.CacheWriteTokens,
			ReasoningTokens: record.Usage.ReasoningTokens, TotalTokens: record.Usage.TotalTokens,
		})
	}
	return result, nil
}

// decodeModelContent rebuilds one owned continuation item from its stored representation.
func decodeModelContent(item *modelContentRecord) (model.Content, error) {
	value := model.Content{
		Kind: item.Kind, Text: pointerStringOption(item.Text), Final: true,
		ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
	}
	if item.ProviderContext != nil {
		value.ProviderContext = mo.Some(model.ProviderContext{
			Source: model.ProviderContextSource{
				ProviderID: model.ProviderID(item.ProviderContext.ProviderID), API: item.ProviderContext.API,
				Model:            model.ID(item.ProviderContext.Model),
				CompatibilityKey: pointerStringOption(item.ProviderContext.CompatibilityKey),
			},
			Payload: bytes.Clone(item.ProviderContext.Payload),
		})
	}
	if err := validateModelContentShape(
		item.Kind, item.Text != nil, item.ProviderContext != nil, item.ToolCall != nil,
	); err != nil {
		return model.Content{}, err
	}
	if item.ToolCall != nil {
		if item.ToolCall.ID == "" || item.ToolCall.Name == "" {
			return model.Content{}, errors.New("invalid model tool call content")
		}
		value.ToolCall = mo.Some(model.ToolCall{
			ID: item.ToolCall.ID, Name: item.ToolCall.Name, Arguments: item.ToolCall.Arguments,
		})
	}
	return value, nil
}

func validOutcome(outcome model.Outcome) bool {
	return outcome >= model.OutcomeStop && outcome <= model.OutcomeFailed
}

func optionStringPointer(option mo.Option[string]) *string {
	value, present := option.Get()
	if !present {
		return nil
	}
	return &value
}

func pointerStringOption(value *string) mo.Option[string] {
	if value == nil {
		return mo.None[string]()
	}
	return mo.Some(*value)
}

func optionProviderIDPointer(option mo.Option[model.ProviderID]) *string {
	value, present := option.Get()
	if !present {
		return nil
	}
	result := string(value)
	return &result
}

func optionModelIDPointer(option mo.Option[model.ID]) *string {
	value, present := option.Get()
	if !present {
		return nil
	}
	result := string(value)
	return &result
}

func pointerProviderIDOption(value *string) mo.Option[model.ProviderID] {
	if value == nil {
		return mo.None[model.ProviderID]()
	}
	return mo.Some(model.ProviderID(*value))
}

func pointerModelIDOption(value *string) mo.Option[model.ID] {
	if value == nil {
		return mo.None[model.ID]()
	}
	return mo.Some(model.ID(*value))
}

// decodeRecord accepts exactly one JSON value whose core fields match the selected DTO.
func decodeRecord(data []byte, target any) error {
	// Core records use a closed schema so format changes require a new version.
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values in one session record")
		}
		return err
	}
	return nil
}
