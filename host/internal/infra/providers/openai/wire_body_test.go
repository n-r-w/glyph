//go:build !integration

package openai

import (
	"bytes"
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReplayReferencesRestoreOrDiscard restores exact private bytes only at retained valid replay slots.
func TestReplayReferencesRestoreOrDiscard(t *testing.T) {
	t.Parallel()
	// Arrange private replay after a visible item and acquire its safe reference.
	fragment := []byte(
		`{ "type" : "reasoning", "id" : "private-id", "encrypted_content" : "private-cipher", "summary" : [] }`,
	)
	original := append([]byte(`{"input":[{"type":"message","role":"user","content":[]},`), fragment...)
	original = append(original, []byte(`]}`)...)
	projection, safe, err := projectWireBody(original)
	require.NoError(t, err)
	require.NotContains(t, string(safe), "private-id")
	require.NotContains(t, string(safe), "private-cipher")
	var object map[string]any
	require.NoError(t, json.Unmarshal(safe, &object))
	reference := object["input"].([]any)[1]
	// Act by retaining the reference after dropping a preceding visible item.
	replacement, err := json.Marshal(map[string]any{"input": []any{reference}})
	require.NoError(t, err)
	restored, err := projection.restore(replacement)
	// Assert private bytes restore exactly and removal deliberately discards them.
	require.NoError(t, err)
	require.True(t, bytes.Contains(restored, fragment), "the original private JSON fragment must remain byte-exact")
	discarded, err := projection.restore([]byte(`{"input":[]}`))
	require.NoError(t, err)
	require.NotContains(t, string(discarded), "private-cipher")
	// Act and assert immutable-original references remain usable after an earlier removal.
	restoredAgain, err := projection.restore(safe)
	require.NoError(t, err)
	require.Contains(t, string(restoredAgain), "private-cipher")
}

// TestReplayReferenceValidation rejects malformed references and never treats ordinary strings as references.
func TestReplayReferenceValidation(t *testing.T) {
	t.Parallel()
	// Arrange a safe request and its invocation-local reference identifier.
	projection, safe, err := projectWireBody(
		[]byte(`{"input":[{"type":"reasoning","encrypted_content":"private-cipher"}]}`),
	)
	require.NoError(t, err)
	require.Contains(t, string(safe), replayReferenceKey)
	var object map[string]any
	require.NoError(t, json.Unmarshal(safe, &object))
	reference := object["input"].([]any)[0].(map[string]any)
	id := reference[replayReferenceKey].(string)
	// Act and assert invalid references fail before any provider dispatch.
	for name, value := range map[string]any{
		"unknown": map[string]any{"input": []any{map[string]any{replayReferenceKey: "unknown"}}},
		"altered": map[string]any{
			"input": []any{map[string]any{replayReferenceKey: id, "extra": true}},
		},
		"duplicated":                    map[string]any{"input": []any{reference, reference}},
		"misplaced":                     map[string]any{"instructions": reference},
		"object_instead_of_input_array": map[string]any{"input": map[string]any{"0": reference}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange a replacement containing one invalid reference form.
			body, marshalErr := json.Marshal(value)
			require.NoError(t, marshalErr)
			// Act through restoration validation.
			_, restoreErr := projection.restore(body)
			// Assert the invalid reference is rejected.
			require.Error(t, restoreErr)
		})
	}
	// Act with an ordinary public string matching the old restoration marker spelling.
	replacement, err := json.Marshal(
		map[string]any{"input": []any{reference}, "marker": "glyph-private-fragment-" + id},
	)
	require.NoError(t, err)
	restored, err := projection.restore(replacement)
	require.NoError(t, err)
	// Assert ordinary text is not converted into private replay at an unrelated location.
	var final map[string]any
	require.NoError(t, json.Unmarshal(restored, &final))
	require.Equal(t, "glyph-private-fragment-"+id, final["marker"])
}

// TestChatReplayReferences hides and restores the complete OpenRouter replay collection.
func TestChatReplayReferences(t *testing.T) {
	t.Parallel()
	// Arrange Chat replay containing provider-owned identifiers and opaque content.
	original := []byte(
		`{"messages":[{"role":"assistant","content":"visible","reasoning_details":[` +
			`{"type":"reasoning.encrypted","id":"private-id","data":"private-data"}]}]}`,
	)
	projection, safe, err := projectWireBody(original)
	require.NoError(t, err)
	require.NotContains(t, string(safe), "private-data")
	require.NotContains(t, string(safe), "private-id")
	// Act by retaining the safe representation unchanged as a replacement.
	restored, err := projection.restore(safe)
	// Assert replay restoration preserves the private collection while visible content stays public.
	require.NoError(t, err)
	require.Contains(t, string(restored), "private-data")
	require.Contains(t, string(safe), "visible")
}
