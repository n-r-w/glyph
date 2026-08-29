// Package textbudget defines the model-visible text result limits for standard tools.
package textbudget

// Truncation describes a bounded model-visible text result.
type Truncation struct {
	// Truncated reports whether the model-visible result was shortened.
	Truncated bool
	// TotalBytes is the complete output byte count.
	TotalBytes int64
	// TotalLines is the complete output line count.
	TotalLines int64
	// FullOutputPath contains the retained complete output path.
	FullOutputPath string
}

const (
	// MaximumBytes is the complete text-result byte budget.
	MaximumBytes = 50 * 1024
	// MaximumLines is the complete text-result line budget.
	MaximumLines = 2000
)
