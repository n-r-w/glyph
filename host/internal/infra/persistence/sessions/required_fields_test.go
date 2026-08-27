package sessions_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestLoadRejectsMissingRequiredNestedCoreFields verifies completed records retain every mandatory nested key.
func TestLoadRejectsMissingRequiredNestedCoreFields(t *testing.T) {
	t.Parallel()

	// Arrange valid core records and remove one required nested field in each case.
	tests := []struct {
		name   string
		record func() map[string]any
		path   []any
	}{
		{name: "user message content", record: requiredUserRecord, path: []any{"message", "content"}},
		{name: "user text kind", record: requiredUserRecord, path: []any{"message", "content", 0, "kind"}},
		{name: "user text value", record: requiredUserRecord, path: []any{"message", "content", 0, "text"}},
		{name: "user image media type", record: requiredUserRecord, path: []any{"message", "content", 1, "mediaType"}},
		{name: "user image data", record: requiredUserRecord, path: []any{"message", "content", 1, "data"}},
		{name: "model response content", record: requiredModelRecord, path: []any{"response", "content"}},
		{name: "model response outcome", record: requiredModelRecord, path: []any{"response", "outcome"}},
		{name: "model response diagnostics", record: requiredModelRecord, path: []any{"response", "diagnostics"}},
		{name: "usage input tokens", record: requiredModelRecord, path: []any{"response", "usage", "inputTokens"}},
		{name: "usage output tokens", record: requiredModelRecord, path: []any{"response", "usage", "outputTokens"}},
		{name: "usage cached input tokens", record: requiredModelRecord, path: []any{"response", "usage", "cachedInputTokens"}},
		{name: "usage cache write tokens", record: requiredModelRecord, path: []any{"response", "usage", "cacheWriteTokens"}},
		{name: "usage reasoning tokens", record: requiredModelRecord, path: []any{"response", "usage", "reasoningTokens"}},
		{name: "usage total tokens", record: requiredModelRecord, path: []any{"response", "usage", "totalTokens"}},
		{name: "estimated cost input", record: requiredModelRecord, path: []any{"estimatedCost", "input"}},
		{name: "estimated cost output", record: requiredModelRecord, path: []any{"estimatedCost", "output"}},
		{name: "estimated cost cache read", record: requiredModelRecord, path: []any{"estimatedCost", "cacheRead"}},
		{name: "estimated cost cache write", record: requiredModelRecord, path: []any{"estimatedCost", "cacheWrite"}},
		{name: "estimated cost total", record: requiredModelRecord, path: []any{"estimatedCost", "total"}},
		{name: "diagnostic code", record: requiredModelRecord, path: []any{"response", "diagnostics", 0, "code"}},
		{name: "diagnostic message", record: requiredModelRecord, path: []any{"response", "diagnostics", 0, "message"}},
		{name: "model content kind", record: requiredModelRecord, path: []any{"response", "content", 0, "kind"}},
		{name: "model text value", record: requiredModelRecord, path: []any{"response", "content", 0, "text"}},
		{name: "provider context provider ID", record: requiredModelRecord, path: []any{"response", "content", 1, "providerContext", "providerId"}},
		{name: "provider context API", record: requiredModelRecord, path: []any{"response", "content", 1, "providerContext", "api"}},
		{name: "provider context model", record: requiredModelRecord, path: []any{"response", "content", 1, "providerContext", "model"}},
		{name: "provider context payload", record: requiredModelRecord, path: []any{"response", "content", 1, "providerContext", "payload"}},
		{name: "tool call ID", record: requiredModelRecord, path: []any{"response", "content", 2, "toolCall", "id"}},
		{name: "tool call name", record: requiredModelRecord, path: []any{"response", "content", 2, "toolCall", "name"}},
		{name: "tool call arguments", record: requiredModelRecord, path: []any{"response", "content", 2, "toolCall", "arguments"}},
		{name: "tool result call ID", record: requiredToolResultRecord, path: []any{"result", "callId"}},
		{name: "tool result name", record: requiredToolResultRecord, path: []any{"result", "toolName"}},
		{name: "tool result contents", record: requiredToolResultRecord, path: []any{"result", "contents"}},
		{name: "tool result error flag", record: requiredToolResultRecord, path: []any{"result", "isError"}},
		{name: "tool result text kind", record: requiredToolResultRecord, path: []any{"result", "contents", 0, "kind"}},
		{name: "tool result text value", record: requiredToolResultRecord, path: []any{"result", "contents", 0, "text"}},
		{name: "tool result image media type", record: requiredToolResultRecord, path: []any{"result", "contents", 1, "mediaType"}},
		{name: "tool result image data", record: requiredToolResultRecord, path: []any{"result", "contents", 1, "data"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository, projectDirectory, cwd := newValidationRepository(t)
			record := test.record()
			deleteRequiredField(t, record, test.path)
			encoded, err := json.Marshal(record)
			require.NoError(t, err)
			content := []byte(fmt.Sprintf(validHeader, cwd) + string(encoded) + "\n")
			path := filepath.Join(projectDirectory, "stored.jsonl")
			require.NoError(t, os.WriteFile(path, content, 0o640))

			// Act by explicitly loading the newline-terminated record with one omitted required key.
			_, loadErr := repository.Load(t.Context(), session.ID("stored"))

			// Assert unavailable classification and exact file bytes and mode remain unchanged.
			require.ErrorIs(t, loadErr, session.ErrUnavailable)
			after, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			assert.Equal(t, content, after)
			info, statErr := os.Stat(path)
			require.NoError(t, statErr)
			assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
		})
	}
}

// TestLoadRejectsNullOptionalCoreObjects verifies optional usage and cost are omitted or complete objects.
func TestLoadRejectsNullOptionalCoreObjects(t *testing.T) {
	t.Parallel()

	// Arrange model records with an explicit null optional object that the domain cannot represent.
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "null usage", mutate: func(record map[string]any) {
			record["response"].(map[string]any)["usage"] = nil
		}},
		{name: "null estimated cost", mutate: func(record map[string]any) {
			record["estimatedCost"] = nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository, projectDirectory, cwd := newValidationRepository(t)
			record := requiredModelRecord()
			test.mutate(record)
			encoded, err := json.Marshal(record)
			require.NoError(t, err)
			content := fmt.Appendf(nil, validHeader, cwd)
			content = append(content, encoded...)
			content = append(content, '\n')
			path := filepath.Join(projectDirectory, "stored.jsonl")
			require.NoError(t, os.WriteFile(path, content, 0o640))

			// Act by loading the newline-terminated record with an explicit null optional object.
			_, loadErr := repository.Load(t.Context(), session.ID("stored"))

			// Assert unavailable classification and immutable bytes and mode.
			require.ErrorIs(t, loadErr, session.ErrUnavailable)
			after, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			assert.Equal(t, content, after)
			info, statErr := os.Stat(path)
			require.NoError(t, statErr)
			assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
		})
	}
}

// TestRequiredFieldValidationPreservesAllowedZeroAndContainerStates verifies presence checks do not collapse values.
func TestRequiredFieldValidationPreservesAllowedZeroAndContainerStates(t *testing.T) {
	t.Parallel()

	// Arrange valid records with present zero, false, empty-string, nil-container, and empty-container values.
	for _, test := range []struct {
		name       string
		emptyState bool
	}{
		{name: "present nil containers", emptyState: false},
		{name: "present non-nil empty containers", emptyState: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository, projectDirectory, cwd := newValidationRepository(t)
			modelRecord := requiredModelRecord()
			toolRecord := requiredToolResultRecord()
			toolRecord["id"] = "entry-2"
			userRecord := requiredUserRecord()
			userRecord["id"] = "entry-3"
			response := modelRecord["response"].(map[string]any)
			modelContents := response["content"].([]any)
			providerContext := modelContents[1].(map[string]any)["providerContext"].(map[string]any)
			arguments := modelContents[2].(map[string]any)["toolCall"].(map[string]any)
			toolResult := toolRecord["result"].(map[string]any)
			message := userRecord["message"].(map[string]any)
			if test.emptyState {
				providerContext["payload"] = ""
				arguments["arguments"] = map[string]any{}
				response["diagnostics"] = []any{}
				toolResult["contents"] = []any{}
				message["content"] = []any{}
			} else {
				providerContext["payload"] = nil
				arguments["arguments"] = nil
				response["diagnostics"] = nil
				toolResult["contents"] = nil
				message["content"] = nil
			}
			content := fmt.Appendf(nil, validHeader, cwd)
			for _, record := range []map[string]any{modelRecord, toolRecord, userRecord} {
				encoded, err := json.Marshal(record)
				require.NoError(t, err)
				content = append(content, encoded...)
				content = append(content, '\n')
			}
			require.NoError(t, os.WriteFile(filepath.Join(projectDirectory, "stored.jsonl"), content, 0o600))

			// Act by loading each present state through strict required-field validation.
			loaded, err := repository.Load(t.Context(), session.ID("stored"))

			// Assert zero scalars remain present and nil versus empty container states remain distinct.
			require.NoError(t, err)
			require.Len(t, loaded.Entries, 3)
			modelValue := loaded.Entries[0].Model.MustGet()
			assert.Equal(t, int64(0), modelValue.Usage.MustGet().TotalTokens)
			assert.Zero(t, loaded.Entries[0].EstimatedCost.MustGet().Total)
			assert.Empty(t, modelValue.Content[0].Text.MustGet())
			providerPayload := modelValue.Content[1].ProviderContext.MustGet().Payload
			toolArguments := modelValue.Content[2].ToolCall.MustGet().Arguments
			toolValue := loaded.Entries[1].ToolResult.MustGet()
			userValue := loaded.Entries[2].User.MustGet()
			assert.False(t, toolValue.IsError)
			if test.emptyState {
				assert.NotNil(t, providerPayload)
				assert.NotNil(t, toolArguments)
				assert.NotNil(t, modelValue.Diagnostics)
				assert.NotNil(t, toolValue.Contents)
				assert.NotNil(t, userValue.Content)
			} else {
				assert.Nil(t, providerPayload)
				assert.Nil(t, toolArguments)
				assert.Nil(t, modelValue.Diagnostics)
				assert.Nil(t, toolValue.Contents)
				assert.Nil(t, userValue.Content)
			}
		})
	}
}

func requiredUserRecord() map[string]any {
	return map[string]any{
		"type": "user", "id": "entry-1", "createdAt": "2026-08-27T10:00:01Z",
		"message": map[string]any{"content": []any{
			map[string]any{"kind": float64(1), "text": ""},
			map[string]any{"kind": float64(2), "mediaType": "image/png", "data": nil},
		}},
	}
}

func requiredModelRecord() map[string]any {
	return map[string]any{
		"type": "model", "id": "entry-1", "createdAt": "2026-08-27T10:00:01Z",
		"response": map[string]any{
			"content": []any{
				map[string]any{"kind": float64(1), "text": ""},
				map[string]any{"kind": float64(3), "providerContext": map[string]any{
					"providerId": "", "api": "", "model": "", "payload": nil,
				}},
				map[string]any{"kind": float64(4), "toolCall": map[string]any{
					"id": "call-1", "name": "tool", "arguments": nil,
				}},
			},
			"outcome": float64(1),
			"usage": map[string]any{
				"inputTokens": float64(0), "outputTokens": float64(0),
				"cachedInputTokens": float64(0), "cacheWriteTokens": float64(0),
				"reasoningTokens": float64(0), "totalTokens": float64(0),
			},
			"diagnostics": []any{map[string]any{"code": "", "message": ""}},
		},
		"estimatedCost": map[string]any{
			"input": float64(0), "output": float64(0), "cacheRead": float64(0),
			"cacheWrite": float64(0), "total": float64(0),
		},
	}
}

func requiredToolResultRecord() map[string]any {
	return map[string]any{
		"type": "tool_result", "id": "entry-1", "createdAt": "2026-08-27T10:00:01Z",
		"result": map[string]any{
			"callId": "call-1", "toolName": "tool", "isError": false,
			"contents": []any{
				map[string]any{"kind": float64(1), "text": ""},
				map[string]any{"kind": float64(2), "mediaType": "image/png", "data": nil},
			},
		},
	}
}

func deleteRequiredField(t *testing.T, root map[string]any, path []any) {
	t.Helper()
	var current any = root
	for _, segment := range path[:len(path)-1] {
		switch value := segment.(type) {
		case string:
			current = current.(map[string]any)[value]
		case int:
			current = current.([]any)[value]
		default:
			t.Fatalf("unsupported required-field path segment %T", segment)
		}
	}
	delete(current.(map[string]any), path[len(path)-1].(string))
}
