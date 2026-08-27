package sessions

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
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
	case "session_info":
		if validationErr := requireJSONFields(entry, "name"); validationErr != nil {
			return validationErr
		}
		return requireNonNullJSONFields(entry, "name")
	case "user":
		return validateUserRequiredFields(entry)
	case "model":
		return validateModelRequiredFields(entry)
	case "tool_result":
		return validateToolResultRequiredFields(entry)
	case "extension":
		if validationErr := requireJSONFields(entry, "extensionId", "entryType", "data"); validationErr != nil {
			return validationErr
		}
		return requireNonNullJSONFields(entry, "extensionId", "entryType")
	default:
		return errors.New("invalid session entry")
	}
}

func validateUserRequiredFields(entry jsonObject) error {
	message, err := requiredChildJSONObject(entry, "message", "content")
	if err != nil {
		return err
	}
	return validateJSONArray(message["content"], "user content", func(raw json.RawMessage) error {
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
		"inputTokens", "outputTokens", "cachedInputTokens", "cacheWriteTokens", "reasoningTokens", "totalTokens",
	}
	if validationErr := validateOptionalRequiredObject(
		response, "usage", usageFields, usageFields,
	); validationErr != nil {
		return validationErr
	}
	costFields := []string{"input", "output", "cacheRead", "cacheWrite", "total"}
	return validateOptionalRequiredObject(entry, "estimatedCost", costFields, costFields)
}

func validateDiagnosticRequiredFields(raw json.RawMessage) error {
	diagnostic, err := requiredJSONObject(raw, "code", "message")
	if err != nil {
		return err
	}
	return requireNonNullJSONFields(diagnostic, "code", "message")
}

func validateModelContentRequiredFields(raw json.RawMessage) error {
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
		[]string{"providerId", "api", "model", "payload"},
		[]string{"providerId", "api", "model"},
	); validationErr != nil {
		return validationErr
	}
	return validateOptionalRequiredObject(
		content,
		"toolCall",
		[]string{"id", "name", "arguments"},
		[]string{"id", "name"},
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
	return validateJSONArray(result["contents"], "tool result content", func(raw json.RawMessage) error {
		return validateTextOrImageRequiredFields(raw, int(tool.ResultContentText), int(tool.ResultContentImage))
	})
}

func validateTextOrImageRequiredFields(raw json.RawMessage, textKind, imageKind int) error {
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

type jsonObject map[string]json.RawMessage

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

func validateJSONArray(raw json.RawMessage, name string, validate func(json.RawMessage) error) error {
	var values []json.RawMessage
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
