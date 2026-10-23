//go:build integration

package app

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"testing"

	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// preparedRestoredInputText identifies the fixture's Codex text content variant.
const preparedRestoredInputText = "input_text"

// preparedRestoredMessage holds the role and text of a plain-text fixture message.
type preparedRestoredMessage struct {
	// Role identifies the provider message author.
	Role string
	// Text contains the fixture message content.
	Text string
}

// assertPreparedRestoredSuffix compares restored input with the latest receipt's saved suffix.
func assertPreparedRestoredSuffix(
	t *testing.T,
	body []byte,
	branch []*programmaticpb.SessionEntry,
	receipt *programmaticpb.SessionEntry,
	request string,
) {
	t.Helper()
	boundary := -1
	for index, entry := range branch {
		if entry.GetId() == receipt.GetCompaction().GetFirstKeptEntryId() {
			boundary = index
			break
		}
	}
	require.NotEqual(t, -1, boundary, "second receipt boundary must exist in the saved active branch")
	require.Equal(t, receipt.GetId(), branch[len(branch)-1].GetId())
	summary := receipt.GetCompaction().GetSummary()
	expected := []preparedRestoredMessage{{Role: string(responses.EasyInputMessageRoleUser), Text: summary}}
	for _, entry := range branch[boundary : len(branch)-1] {
		switch {
		case entry.HasUser():
			content := entry.GetUser().GetContent()
			require.Len(t, content, 1, "unexpected retained user variant: %s", entry.GetId())
			require.True(t, content[0].HasText(), "unexpected retained user content: %s", entry.GetId())
			expected = append(expected, preparedRestoredMessage{
				Role: string(responses.EasyInputMessageRoleUser), Text: content[0].GetText(),
			})
		case entry.HasModel():
			response := entry.GetModel()
			require.Equal(t, programmaticpb.ModelOutcome_MODEL_OUTCOME_STOP, response.GetOutcome())
			content := response.GetContent()
			require.Len(t, content, 1, "unexpected retained model variant: %s", entry.GetId())
			require.True(t, content[0].HasText(), "unexpected retained model content: %s", entry.GetId())
			expected = append(expected, preparedRestoredMessage{
				Role: string(responses.EasyInputMessageRoleAssistant), Text: content[0].GetText().GetText(),
			})
		default:
			t.Fatalf("unexpected retained fixture entry: %s", entry.GetId())
		}
	}
	require.GreaterOrEqual(t, len(expected)-1, 2, "restored suffix needs two provider-visible items")
	require.NotEqual(t, expected[1], expected[2], "retained items must make reordering observable")
	expected = append(
		expected,
		preparedRestoredMessage{Role: string(responses.EasyInputMessageRoleUser), Text: request},
	)
	actual := decodePreparedRestoredInput(t, body)
	require.NotEmpty(t, actual)
	// The receipt supplies accepted summary content; its production prompt wrapper is mutable.
	require.Contains(t, actual[0].Text, summary, "latest summary must lead restored input")
	actual[0].Text = summary
	require.Equal(t, expected, actual, "ordered restored suffix must match the second receipt's saved branch")
}

// decodePreparedRestoredInput accepts only the plain-text Codex message variants used by this fixture.
func decodePreparedRestoredInput(t *testing.T, body []byte) []preparedRestoredMessage {
	t.Helper()
	var envelope struct {
		Input []jsontext.Value `json:"input"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope))
	messages := make([]preparedRestoredMessage, 0, len(envelope.Input))
	for _, raw := range envelope.Input {
		var item struct {
			Type    string         `json:"type"`
			Role    string         `json:"role"`
			Content jsontext.Value `json:"content"`
		}
		require.NoError(t, json.Unmarshal(raw, &item, json.RejectUnknownMembers(true)))
		require.Equal(t, string(responses.EasyInputMessageTypeMessage), item.Type)
		var text string
		switch item.Role {
		case string(responses.EasyInputMessageRoleUser):
			var blocks []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			require.NoError(t, json.Unmarshal(item.Content, &blocks, json.RejectUnknownMembers(true)))
			require.Len(t, blocks, 1, "unexpected restored user variant")
			require.Equal(t, preparedRestoredInputText, blocks[0].Type)
			text = blocks[0].Text
		case string(responses.EasyInputMessageRoleAssistant):
			require.NoError(t, json.Unmarshal(item.Content, &text))
		default:
			t.Fatalf("unexpected restored fixture role: %s", item.Role)
		}
		messages = append(messages, preparedRestoredMessage{Role: item.Role, Text: text})
	}
	return messages
}
