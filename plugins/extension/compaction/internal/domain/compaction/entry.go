package compaction

import (
	"bytes"
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

// Entry contains one prepared record and its original source correspondence.
type Entry struct {
	// ID identifies the original source record.
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
	// SourcePosition identifies the source's original branch position.
	SourcePosition int64
	// SourceKind identifies the kind of original entry associated with the prepared content.
	SourceKind SourceKind
	// PreviousSummary identifies the rendered preceding summary.
	PreviousSummary bool
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

// equalContent compares the prepared content and private-payload sizes.
func (e Entry) equalContent(other Entry) bool {
	if e.Kind != other.Kind || e.CallID != other.CallID || e.Tool != other.Tool || e.Failed != other.Failed ||
		e.PrivateBytes != other.PrivateBytes ||
		len(e.Blocks) != len(other.Blocks) {
		return false
	}
	for index := range e.Blocks {
		left, right := &e.Blocks[index], &other.Blocks[index]
		if left.Kind != right.Kind || left.Text != right.Text || left.MediaType != right.MediaType ||
			left.CallID != right.CallID ||
			left.Name != right.Name ||
			!bytes.Equal(left.Data, right.Data) ||
			!bytes.Equal(left.Arguments, right.Arguments) {
			return false
		}
	}
	return true
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
	// Reordering and shared sources can join several content groups into one original-branch span.
	suffixMinimum := make([]int64, len(entries))
	for index := len(entries) - 1; index >= 0; index-- {
		suffixMinimum[index] = entries[index].SourcePosition
		if index+1 < len(entries) {
			suffixMinimum[index] = min(suffixMinimum[index], suffixMinimum[index+1])
		}
	}
	legal := make([]int, 0, len(groups))
	maximum := int64(-1)
	next := 0
	for _, start := range groups {
		for next < start {
			maximum = max(maximum, entries[next].SourcePosition)
			next++
		}
		if start == 0 || maximum < suffixMinimum[start] {
			legal = append(legal, start)
		}
	}
	return legal
}
