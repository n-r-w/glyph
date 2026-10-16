package generation

import (
	"errors"
	"fmt"
	"strings"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

const (
	// OutcomeStop identifies normal completion.
	OutcomeStop = "stop"
	// OutcomeLength identifies response-budget completion.
	OutcomeLength = "length"
	// OutcomeToolUse identifies returned tool calls.
	OutcomeToolUse = "tool_use"
	// OutcomeAborted identifies cancellation.
	OutcomeAborted = "aborted"
	// OutcomeFailed identifies failed execution.
	OutcomeFailed = "failed"
)

// Response contains the terminal public model output.
type Response struct {
	// Outcome identifies the terminal completion reason.
	Outcome string
	// Blocks contains ordered public response content.
	Blocks []compaction.Block
	// Error contains the complete terminal cause.
	Error string
	// Usage contains optional normalized accounting.
	Usage mo.Option[compaction.Usage]
}

// summary extracts public summary text from the model response.
func (r Response) summary() (string, error) {
	var cause error
	if r.Error != "" {
		cause = errors.New(r.Error)
	}
	if r.Outcome != OutcomeStop && r.Outcome != OutcomeLength {
		return "", errors.Join(fmt.Errorf("summary model ended with outcome %q", r.Outcome), cause)
	}
	if cause != nil {
		return "", fmt.Errorf("summary model failed: %w", cause)
	}
	var text strings.Builder
	for index := range r.Blocks {
		block := &r.Blocks[index]
		switch block.Kind {
		case compaction.Text:
			text.WriteString(block.Text)
		case compaction.Thought, compaction.Refusal:
		case compaction.Call:
			return "", errors.New("summary model returned a tool call")
		default:
			return "", fmt.Errorf("summary model returned invalid content kind %q", block.Kind)
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		return "", errors.New("summary model returned no nonempty public text")
	}
	if usage, present := r.Usage.Get(); present {
		if err := usage.Validate(); err != nil {
			return "", fmt.Errorf("summary model usage: %w", err)
		}
	}
	return text.String(), nil
}
