package generation

import (
	"encoding/json/v2"
	"fmt"

	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// source contains the prior summary and conversation data to summarize.
type source struct {
	// Previous contains the preceding summary text.
	Previous string
	// Messages contains the conversation records to summarize.
	Messages []sourceMessage
}

// sourceMessage contains a role-labeled original record.
type sourceMessage struct {
	// Role identifies the message's semantic role.
	Role string
	// CallID identifies a tool result's producing call.
	CallID string
	// Tool identifies a tool-result producer.
	Tool string
	// Failed indicates a tool failure.
	Failed bool
	// Blocks contains source content.
	Blocks []sourceBlock
}

// sourceBlock represents textual content and tool-call data.
type sourceBlock struct {
	// Kind identifies the content's semantic role.
	Kind string
	// Text contains source text.
	Text string
	// CallID identifies a tool invocation.
	CallID string
	// Tool identifies the requested tool.
	Tool string
	// Arguments contains JSON tool arguments as source data.
	Arguments string
}

// serializeSource prepares structured conversation text for summarization.
func serializeSource(request compaction.Request) (string, error) {
	const toolTextLimit = 2000
	value := source{Previous: "", Messages: make([]sourceMessage, 0, len(request.Prefix))}
	if previous, present := request.Previous.Get(); present {
		value.Previous = previous.Summary
	}
	for index := range request.Prefix {
		entry := &request.Prefix[index]
		message := sourceMessage{
			Role:   entry.Kind,
			CallID: entry.CallID,
			Tool:   entry.Tool,
			Failed: entry.Failed,
			Blocks: make([]sourceBlock, 0, len(entry.Blocks)),
		}
		remaining, removed := toolTextLimit, false
		for position := range entry.Blocks {
			block := &entry.Blocks[position]
			if block.Kind == compaction.Image {
				continue
			}
			text := block.Text
			if entry.Kind == compaction.ToolResult && block.Kind == compaction.Text {
				characters := []rune(text)
				if len(characters) > remaining {
					characters = characters[:remaining]
					removed = true
				}
				remaining -= len(characters)
				text = string(characters)
			}
			message.Blocks = append(
				message.Blocks,
				sourceBlock{
					Kind:      block.Kind,
					Text:      text,
					CallID:    block.CallID,
					Tool:      block.Name,
					Arguments: string(block.Arguments),
				},
			)
		}
		if removed {
			message.Blocks = append(
				message.Blocks,
				sourceBlock{Kind: compaction.Text, Text: truncationMarker, CallID: "", Tool: "", Arguments: ""},
			)
		}
		value.Messages = append(value.Messages, message)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("serialize compaction source: %w", err)
	}
	return string(encoded), nil
}
