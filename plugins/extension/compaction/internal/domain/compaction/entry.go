package compaction

import (
	"github.com/samber/mo"
)

// Block contains one semantic message block.
type Block struct {
	// Kind identifies the semantic content category.
	Kind string
	// Text contains textual content.
	Text string
	// MediaType identifies image encoding.
	MediaType string
	// Data contains encoded image bytes.
	Data []byte
	// CallID identifies a tool invocation.
	CallID string
	// Name identifies the invoked tool.
	Name string
	// Arguments contains JSON tool arguments.
	Arguments []byte
}

// Entry contains one original session record or prepared history record.
type Entry struct {
	// ID identifies the original session record.
	ID string
	// Kind identifies the record's semantic role.
	Kind string
	// Blocks contains ordered message content.
	Blocks []Block
	// CallID identifies the originating tool call.
	CallID string
	// Tool identifies the tool that produced a result.
	Tool string
	// Failed indicates tool execution failure.
	Failed bool
	// Extension identifies an extension-owned entry.
	Extension string
	// EntryType identifies extension-owned content.
	EntryType string
	// Visibility is the extension message's client visibility.
	Visibility Visibility
	// Marker contains a preceding compaction marker.
	Marker mo.Option[Result]
	// PrivateBytes is the retained private replay size.
	PrivateBytes int64
	// Tokens is the producer's content estimate.
	Tokens int64
}

const (
	// User identifies caller content.
	User = "user"
	// Assistant identifies model content.
	Assistant = "assistant"
	// ToolResult identifies terminal tool content.
	ToolResult = "tool_result"
	// BranchSummary identifies a branch summary.
	BranchSummary = "branch_summary"
	// ExtensionMessage identifies model-visible extension content.
	ExtensionMessage = "extension_message"
	// ExtensionEntry identifies model-hidden extension content.
	ExtensionEntry = "extension"
	// Marker identifies a compaction entry.
	Marker = "compaction"
	// Text identifies public text.
	Text = "text"
	// Image identifies encoded image content.
	Image = "image"
	// Thought identifies public reasoning.
	Thought = "reasoning"
	// Refusal identifies model refusal content.
	Refusal = "refusal"
	// Call identifies a tool request.
	Call = "call"
)

// EstimateTokens estimates this record's complete model-visible contribution.
func (e Entry) EstimateTokens() int64 {
	const (
		messageFraming       = 4
		imageTokens          = 1024
		privateBytesPerToken = 4
	)
	if e.Kind == ExtensionEntry || e.Kind == Marker {
		return 0
	}
	total := int64(messageFraming) + (e.PrivateBytes+privateBytesPerToken-1)/privateBytesPerToken
	total += estimateText(e.CallID) + estimateText(e.Tool)
	for index := range e.Blocks {
		block := &e.Blocks[index]
		if block.Kind == Image {
			total += imageTokens
			continue
		}
		total += estimateText(
			block.Text,
		) + estimateText(
			block.CallID,
		) + estimateText(
			block.Name,
		) + estimateText(
			string(block.Arguments),
		)
	}
	return total
}

// findGroupStarts identifies complete message-group boundaries.
func findGroupStarts(entries []Entry) []int {
	producers := make(map[string]int)
	ends := make([]int, len(entries))
	for index := range entries {
		entry := &entries[index]
		ends[index] = max(ends[index], index)
		for position := range entry.Blocks {
			block := &entry.Blocks[position]
			if block.Kind == Call {
				producers[block.CallID] = index
			}
		}
		if entry.Kind == ToolResult {
			if producer, found := producers[entry.CallID]; found {
				ends[producer] = index
			}
		}
	}
	groups := make([]int, 0, len(entries))
	for start := 0; start < len(entries); {
		groups = append(groups, start)
		end := ends[start]
		for index := start + 1; index <= end; index++ {
			end = max(end, ends[index])
		}
		start = end + 1
	}
	return groups
}
