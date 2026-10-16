// Package generation implements standard summary generation.
package generation

import (
	"context"

	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock_test.go -package=generation -build_constraint=!integration

// Model executes an explicit configured-model request.
type Model interface {
	// Request returns the terminal public response.
	Request(context.Context, Input) (Response, error)
}

// Input contains summary task instructions and separate structured source.
type Input struct {
	// Model identifies the final composed model.
	Model compaction.Model
	// Reasoning identifies the final composed reasoning choice.
	Reasoning string
	// Instructions contains the summary task and manual guidance.
	Instructions string
	// Source contains structured conversation data.
	Source string
}
