package compaction

import (
	"errors"

	"github.com/samber/mo"
)

// Usage contains normalized model accounting.
type Usage struct {
	// Counts contains input, output, cache-read, cache-write, reasoning, and total tokens.
	Counts [6]int64
}

// Validate checks normalized token counts.
func (u Usage) Validate() error {
	for _, count := range u.Counts {
		if count < 0 {
			return errors.New("negative token usage")
		}
	}
	if u.Counts[4] > u.Counts[1] || u.Counts[5] != u.Counts[0]+u.Counts[1]+u.Counts[2]+u.Counts[3] {
		return errors.New("inconsistent token usage")
	}
	return nil
}

// ModelRequest contains accounting for one contributing model request.
type ModelRequest struct {
	// Provider identifies the actual configured provider.
	Provider string
	// Model identifies the actual configured model.
	Model string
	// Reasoning identifies the actual reasoning choice.
	Reasoning string
	// Usage contains reported accounting.
	Usage mo.Option[Usage]
	// Cost contains Host-calculated input, output, cache-read, cache-write, and total USD costs.
	Cost mo.Option[[5]float64]
}

// Result contains a proposed replacement and its attribution.
type Result struct {
	// Summary contains model-visible replacement text.
	Summary string
	// FirstKept identifies the first preserved original entry.
	FirstKept string
	// Extension identifies a producer without model requests.
	Extension mo.Option[string]
	// Requests contains contributing model requests.
	Requests []ModelRequest
	// Details contains opaque extension data.
	Details mo.Option[[]byte]
}
