// Package textbudget defines the model-visible text result limits for standard tools.
package textbudget

// Truncation describes a bounded model-visible text result.
type Truncation struct {
	Truncated      bool
	TotalBytes     int64
	TotalLines     int64
	FullOutputPath string
}

const (
	// MaximumBytes is the complete text-result byte budget.
	MaximumBytes = 50 * 1024
	// MaximumLines is the complete text-result line budget.
	MaximumLines = 2000
)
