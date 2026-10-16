package compaction

import (
	"bytes"
	"slices"

	"github.com/samber/mo"
)

// Binding identifies issued access to one session in one runtime.
type Binding struct {
	// Context identifies the issued access.
	Context string
	// Runtime identifies the process incarnation.
	Runtime string
	// Session identifies the durable session.
	Session string
}

// Observation contains a completed conversation and its reported accounting.
type Observation struct {
	// Input contains the actual request.
	Input Input
	// Response contains the completed response's model-visible content.
	Response Entry
	// Usage contains provider-reported accounting.
	Usage mo.Option[Usage]
}

// Anchor holds completed context for usage-based continuation estimates.
type Anchor struct {
	// history contains the completed context.
	history []Entry
	// reasoning contains the request's reasoning choice.
	reasoning string
	// instructions contains the request's instructions.
	instructions string
	// tools contains the tool-definition identity.
	tools string
	// tokens contains reported input and completed output tokens.
	tokens int64
}

// NewAnchor creates a reported-usage baseline.
func NewAnchor(observation Observation) mo.Option[Anchor] {
	usage, present := observation.Usage.Get()
	if !present {
		return mo.None[Anchor]()
	}
	history := make([]Entry, 0, len(observation.Input.History)+1)
	history = append(history, observation.Input.History...)
	history = append(history, observation.Response)
	for index := range history {
		history[index].Blocks = slices.Clone(history[index].Blocks)
		for blockIndex := range history[index].Blocks {
			block := &history[index].Blocks[blockIndex]
			block.Data = bytes.Clone(block.Data)
			block.Arguments = bytes.Clone(block.Arguments)
		}
	}
	return mo.Some(Anchor{
		history: history, reasoning: observation.Input.Reasoning,
		instructions: observation.Input.Instructions, tools: observation.Input.ToolShape,
		tokens: usage.Counts[0] + usage.Counts[1] + usage.Counts[2] + usage.Counts[3],
	})
}

// EstimateContinuation combines reported usage with subsequent input growth.
func (a Anchor) EstimateContinuation(input Input) mo.Option[int64] {
	if a.reasoning != input.Reasoning || a.instructions != input.Instructions || a.tools != input.ToolShape ||
		len(input.History) < len(a.history) {
		return mo.None[int64]()
	}
	for index := range a.history {
		if !a.history[index].equalContent(input.History[index]) {
			return mo.None[int64]()
		}
	}
	return mo.Some(a.tokens + EstimateInput(input.History[len(a.history):], "", ""))
}
