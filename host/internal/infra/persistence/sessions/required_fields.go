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

// validateHeaderRequiredFields rejects omitted or null scalar fields before header conversion.
func validateHeaderRequiredFields(data []byte) error {
	header, err := requiredJSONObject(data, "type", "version", "id", "createdAt", "cwd")
	if err != nil {
		return err
	}
	return requireNonNullJSONFields(header, "type", "version", "id", "createdAt", "cwd")
}

// validateEntryRequiredFields checks required key presence without changing valid zero or collection states.
func validateEntryRequiredFields(data []byte, kind string) error {
	entry, err := requiredJSONObject(data, "type", "id", "createdAt")
	if err != nil {
		return err
	}
	if validationErr := requireNonNullJSONFields(entry, "type", "id", "createdAt"); validationErr != nil {
		return validationErr
	}
	switch kind {
	case recordTypeSessionInfo:
		if validationErr := requireJSONFields(entry, "name"); validationErr != nil {
			return validationErr
		}
		return requireNonNullJSONFields(entry, "name")
	case recordTypeUser:
		return validateUserRequiredFields(entry)
	case recordTypeModel:
		return validateModelRequiredFields(entry)
	case recordTypeToolResult:
		return validateToolResultRequiredFields(entry)
	case recordTypeExtension:
		if validationErr := requireJSONFields(entry, "extensionId", "entryType", "data"); validationErr != nil {
			return validationErr
		}
		return requireNonNullJSONFields(entry, "extensionId", "entryType")
	case recordTypeBranchSummary:
		fields := []string{"summary", "firstEntryId", "lastEntryId", "provider", fieldModel, "reasoningChoice"}
		if validationErr := requireJSONFields(entry, fields...); validationErr != nil {
			return validationErr
		}
		if validationErr := requireNonNullJSONFields(entry, fields...); validationErr != nil {
			return validationErr
		}
		usageFields := []string{
			fieldInputTokens, fieldOutputTokens, "cacheReadTokens",
			fieldCacheWriteTokens, fieldReasoningTokens, fieldTotalTokens,
		}
		if validationErr := validateOptionalRequiredObject(entry, "usage", usageFields, usageFields); validationErr != nil {
			return validationErr
		}
		costFields := []string{fieldInput, fieldOutput, fieldCacheRead, fieldCacheWrite, fieldTotal}
		return validateOptionalRequiredObject(entry, "estimatedCost", costFields, costFields)
	default:
		return errors.New("invalid session entry")
	}
}

func validateUserRequiredFields(entry jsonObject) error {
	message, err := requiredChildJSONObject(entry, "message", "content")
	if err != nil {
		return err
	}
	return validateJSONArray(message["content"], "user content", func(raw jsontext.Value) error {
		return validateTextOrImageRequiredFields(raw, int(model.InputContentText), int(model.InputContentImage))
	})
}

func validateModelRequiredFields(entry jsonObject) error {
	response, err := requiredChildJSONObject(entry, "response", "content", "outcome", "diagnostics")
	if err != nil {
		return err
	}
	if validationErr := requireNonNullJSONFields(response, "outcome"); validationErr != nil {
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
	if validationErr := validateOptionalRequiredObject(
		response, "usage", usageFields, usageFields,
	); validationErr != nil {
		return validationErr
	}
	costFields := []string{fieldInput, fieldOutput, fieldCacheRead, fieldCacheWrite, fieldTotal}
	return validateOptionalRequiredObject(entry, "estimatedCost", costFields, costFields)
}

func validateDiagnosticRequiredFields(raw jsontext.Value) error {
	diagnostic, err := requiredJSONObject(raw, "code", "message")
	if err != nil {
		return err
	}
	return requireNonNullJSONFields(diagnostic, "code", "message")
}

func validateModelContentRequiredFields(raw jsontext.Value) error {
	content, err := requiredJSONObject(raw, "kind")
	if err != nil {
		return err
	}
	if validationErr := requireNonNullJSONFields(content, "kind"); validationErr != nil {
		return validationErr
	}
	if _, present := content["text"]; present {
		if validationErr := requireNonNullJSONFields(content, "text"); validationErr != nil {
			return validationErr
		}
	}
	if validationErr := validateOptionalRequiredObject(
		content,
		"providerContext",
		[]string{fieldProviderID, fieldAPI, fieldModel, "payload"},
		[]string{fieldProviderID, fieldAPI, fieldModel},
	); validationErr != nil {
		return validationErr
	}
	return validateOptionalRequiredObject(
		content,
		"toolCall",
		[]string{fieldID, fieldName, "arguments"},
		[]string{fieldID, fieldName},
	)
}

func validateToolResultRequiredFields(entry jsonObject) error {
	result, err := requiredChildJSONObject(entry, "result", "callId", "toolName", "contents", "isError")
	if err != nil {
		return err
	}
	if validationErr := requireNonNullJSONFields(result, "callId", "toolName", "isError"); validationErr != nil {
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
	if validationErr := requireNonNullJSONFields(content, "kind"); validationErr != nil {
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
		if validationErr := requireJSONFields(content, "text"); validationErr != nil {
			return validationErr
		}
		return requireNonNullJSONFields(content, "text")
	case imageKind:
		if validationErr := requireJSONFields(content, "mediaType", "data"); validationErr != nil {
			return validationErr
		}
		return requireNonNullJSONFields(content, "mediaType")
	default:
		return nil
	}
}

type jsonObject map[string]jsontext.Value

func requiredJSONObject(data []byte, fields ...string) (jsonObject, error) {
	var object jsonObject
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	if object == nil {
		return nil, errors.New("required JSON object is null")
	}
	if err := requireJSONFields(object, fields...); err != nil {
		return nil, err
	}
	return object, nil
}

func requiredChildJSONObject(parent jsonObject, field string, fields ...string) (jsonObject, error) {
	raw, present := parent[field]
	if !present {
		return nil, fmt.Errorf("required field %q is missing", field)
	}
	return requiredJSONObject(raw, fields...)
}

func validateOptionalRequiredObject(
	parent jsonObject,
	field string,
	requiredFields []string,
	nonNullFields []string,
) error {
	raw, present := parent[field]
	if !present {
		return nil
	}
	object, err := requiredJSONObject(raw)
	if err != nil {
		return fmt.Errorf("decode optional object %q: %w", field, err)
	}
	if validationErr := requireJSONFields(object, requiredFields...); validationErr != nil {
		return validationErr
	}
	return requireNonNullJSONFields(object, nonNullFields...)
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

func requireJSONFields(object jsonObject, fields ...string) error {
	for _, field := range fields {
		if _, present := object[field]; !present {
			return fmt.Errorf("required field %q is missing", field)
		}
	}
	return nil
}

func requireNonNullJSONFields(object jsonObject, fields ...string) error {
	for _, field := range fields {
		if bytes.Equal(bytes.TrimSpace(object[field]), []byte("null")) {
			return fmt.Errorf("required field %q is null", field)
		}
	}
	return nil
}
