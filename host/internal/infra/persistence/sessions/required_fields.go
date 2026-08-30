package sessions

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

const (
	// fieldInputTokens identifies normalized input usage in JSON records.
	fieldInputTokens = "inputTokens"
	// fieldOutputTokens identifies normalized output usage in JSON records.
	fieldOutputTokens = "outputTokens"
	// fieldCacheWriteTokens identifies normalized cache-write usage in JSON records.
	fieldCacheWriteTokens = "cacheWriteTokens"
	// fieldReasoningTokens identifies normalized reasoning usage in JSON records.
	fieldReasoningTokens = "reasoningTokens"
	// fieldTotalTokens identifies normalized total usage in JSON records.
	fieldTotalTokens = "totalTokens"
	// fieldInput identifies input cost in JSON records.
	fieldInput = "input"
	// fieldOutput identifies output cost in JSON records.
	fieldOutput = "output"
	// fieldCacheRead identifies cache-read cost in JSON records.
	fieldCacheRead = "cacheRead"
	// fieldCacheWrite identifies cache-write cost in JSON records.
	fieldCacheWrite = "cacheWrite"
	// fieldTotal identifies total cost in JSON records.
	fieldTotal = "total"
	// fieldAPI identifies a provider API value in JSON records.
	fieldAPI = "api"
	// fieldID identifies an object ID in JSON records.
	fieldID = "id"
	// fieldName identifies an object name in JSON records.
	fieldName = "name"
	// fieldModel identifies a model ID in JSON records.
	fieldModel = "model"
	// fieldProviderID identifies a provider ID in JSON records.
	fieldProviderID = "providerId"
)

// jsonObject contains one decoded JSON object.
type jsonObject map[string]jsontext.Value

// validateHeaderRequiredFields rejects omitted or null scalar fields before header conversion.
func validateHeaderRequiredFields(data []byte) error {
	header, err := requiredJSONObject(data, "type", "version", "id", "createdAt", "cwd")
	if err != nil {
		return err
	}
	return header.requireNonNullFields("type", "version", "id", "createdAt", "cwd")
}

// validateEntryRequiredFields checks required key presence without changing valid zero or collection states.
func validateEntryRequiredFields(data []byte, kind string) error {
	entry, err := requiredJSONObject(data, "type", "id", "createdAt")
	if err != nil {
		return err
	}
	if validationErr := entry.requireNonNullFields("type", "id", "createdAt"); validationErr != nil {
		return validationErr
	}
	switch kind {
	case recordTypeSessionInfo:
		if validationErr := entry.requireFields("name"); validationErr != nil {
			return validationErr
		}
		return entry.requireNonNullFields("name")
	case recordTypeUser:
		return entry.validateUserRequiredFields()
	case recordTypeModel:
		return entry.validateModelRequiredFields()
	case recordTypeToolResult:
		return entry.validateToolResultRequiredFields()
	case recordTypeExtension:
		if validationErr := entry.requireFields("extensionId", "entryType", "data"); validationErr != nil {
			return validationErr
		}
		return entry.requireNonNullFields("extensionId", "entryType")
	case recordTypeBranchSummary:
		fields := []string{"summary", "firstEntryId", "lastEntryId", "provider", fieldModel, "reasoningChoice"}
		if validationErr := entry.requireFields(fields...); validationErr != nil {
			return validationErr
		}
		if validationErr := entry.requireNonNullFields(fields...); validationErr != nil {
			return validationErr
		}
		usageFields := []string{
			fieldInputTokens, fieldOutputTokens, "cacheReadTokens",
			fieldCacheWriteTokens, fieldReasoningTokens, fieldTotalTokens,
		}
		if validationErr := entry.validateOptionalRequiredObject("usage", usageFields, usageFields); validationErr != nil {
			return validationErr
		}
		costFields := []string{fieldInput, fieldOutput, fieldCacheRead, fieldCacheWrite, fieldTotal}
		return entry.validateOptionalRequiredObject("estimatedCost", costFields, costFields)
	default:
		return errors.New("invalid session entry")
	}
}

// validateUserRequiredFields validates one user entry object.
func (entry jsonObject) validateUserRequiredFields() error {
	message, err := entry.requiredChild("message", "content")
	if err != nil {
		return err
	}
	return validateJSONArray(message["content"], "user content", func(raw jsontext.Value) error {
		return validateTextOrImageRequiredFields(raw, int(model.InputContentText), int(model.InputContentImage))
	})
}

// validateModelRequiredFields validates one model entry object.
func (entry jsonObject) validateModelRequiredFields() error {
	response, err := entry.requiredChild("response", "content", "outcome", "diagnostics")
	if err != nil {
		return err
	}
	if validationErr := response.requireNonNullFields("outcome"); validationErr != nil {
		return validationErr
	}
	if validationErr := validateJSONArray(
		response["content"], "model content", validateModelContentRequiredFields,
	); validationErr != nil {
		return validationErr
	}
	if validationErr := validateJSONArray(
		response["diagnostics"], "diagnostics", validateDiagnosticRequiredFields,
	); validationErr != nil {
		return validationErr
	}
	usageFields := []string{
		fieldInputTokens, fieldOutputTokens, "cachedInputTokens",
		fieldCacheWriteTokens, fieldReasoningTokens, fieldTotalTokens,
	}
	if validationErr := response.validateOptionalRequiredObject(
		"usage", usageFields, usageFields,
	); validationErr != nil {
		return validationErr
	}
	costFields := []string{fieldInput, fieldOutput, fieldCacheRead, fieldCacheWrite, fieldTotal}
	return entry.validateOptionalRequiredObject("estimatedCost", costFields, costFields)
}

func validateDiagnosticRequiredFields(raw jsontext.Value) error {
	diagnostic, err := requiredJSONObject(raw, "code", "message")
	if err != nil {
		return err
	}
	return diagnostic.requireNonNullFields("code", "message")
}

func validateModelContentRequiredFields(raw jsontext.Value) error {
	content, err := requiredJSONObject(raw, "kind")
	if err != nil {
		return err
	}
	if validationErr := content.requireNonNullFields("kind"); validationErr != nil {
		return validationErr
	}
	if _, present := content["text"]; present {
		if validationErr := content.requireNonNullFields("text"); validationErr != nil {
			return validationErr
		}
	}
	if validationErr := content.validateOptionalRequiredObject(
		"providerContext",
		[]string{fieldProviderID, fieldAPI, fieldModel, "payload"},
		[]string{fieldProviderID, fieldAPI, fieldModel},
	); validationErr != nil {
		return validationErr
	}
	return content.validateOptionalRequiredObject(
		"toolCall",
		[]string{fieldID, fieldName, "arguments"},
		[]string{fieldID, fieldName},
	)
}

// validateToolResultRequiredFields validates one tool-result entry object.
func (entry jsonObject) validateToolResultRequiredFields() error {
	result, err := entry.requiredChild("result", "callId", "toolName", "contents", "isError")
	if err != nil {
		return err
	}
	if validationErr := result.requireNonNullFields("callId", "toolName", "isError"); validationErr != nil {
		return validationErr
	}
	return validateJSONArray(result["contents"], "tool result content", func(raw jsontext.Value) error {
		return validateTextOrImageRequiredFields(raw, int(tool.ResultContentText), int(tool.ResultContentImage))
	})
}

func validateTextOrImageRequiredFields(raw jsontext.Value, textKind, imageKind int) error {
	content, err := requiredJSONObject(raw, "kind")
	if err != nil {
		return err
	}
	if validationErr := content.requireNonNullFields("kind"); validationErr != nil {
		return validationErr
	}
	var discriminator struct {
		Kind int `json:"kind"`
	}
	if decodeErr := json.Unmarshal(raw, &discriminator); decodeErr != nil {
		return decodeErr
	}
	switch discriminator.Kind {
	case textKind:
		if validationErr := content.requireFields("text"); validationErr != nil {
			return validationErr
		}
		return content.requireNonNullFields("text")
	case imageKind:
		if validationErr := content.requireFields("mediaType", "data"); validationErr != nil {
			return validationErr
		}
		return content.requireNonNullFields("mediaType")
	default:
		return nil
	}
}

func requiredJSONObject(data []byte, fields ...string) (jsonObject, error) {
	var object jsonObject
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	if object == nil {
		return nil, errors.New("required JSON object is null")
	}
	if err := object.requireFields(fields...); err != nil {
		return nil, err
	}
	return object, nil
}

// requiredChild returns one required child object.
func (entry jsonObject) requiredChild(field string, fields ...string) (jsonObject, error) {
	raw, present := entry[field]
	if !present {
		return nil, fmt.Errorf("required field %q is missing", field)
	}
	return requiredJSONObject(raw, fields...)
}

// validateOptionalRequiredObject validates one present optional child object.
func (entry jsonObject) validateOptionalRequiredObject(
	field string,
	requiredFields []string,
	nonNullFields []string,
) error {
	raw, present := entry[field]
	if !present {
		return nil
	}
	object, err := requiredJSONObject(raw)
	if err != nil {
		return fmt.Errorf("decode optional object %q: %w", field, err)
	}
	if validationErr := object.requireFields(requiredFields...); validationErr != nil {
		return validationErr
	}
	return object.requireNonNullFields(nonNullFields...)
}

func validateJSONArray(raw jsontext.Value, name string, validate func(jsontext.Value) error) error {
	var values []jsontext.Value
	if err := json.Unmarshal(raw, &values); err != nil {
		return fmt.Errorf("decode required %s: %w", name, err)
	}
	for _, value := range values {
		if err := validate(value); err != nil {
			return err
		}
	}
	return nil
}

// requireFields checks required field presence.
func (entry jsonObject) requireFields(fields ...string) error {
	for _, field := range fields {
		if _, present := entry[field]; !present {
			return fmt.Errorf("required field %q is missing", field)
		}
	}
	return nil
}

// requireNonNullFields checks required fields for explicit null values.
func (entry jsonObject) requireNonNullFields(fields ...string) error {
	for _, field := range fields {
		if bytes.Equal(bytes.TrimSpace(entry[field]), []byte("null")) {
			return fmt.Errorf("required field %q is null", field)
		}
	}
	return nil
}
