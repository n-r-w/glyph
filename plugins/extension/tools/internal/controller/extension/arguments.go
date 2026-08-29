package extension

import (
	"fmt"

	"github.com/samber/mo"
)

// readArguments is the transport-local read input.
type readArguments struct {
	// Path identifies the project file to read.
	Path string `json:"path"`
	// Offset identifies the first requested line.
	Offset mo.Option[uint] `json:"offset"`
	// Limit is the maximum number of lines.
	Limit mo.Option[uint] `json:"limit"`
}

// writeArguments is the transport-local write input.
type writeArguments struct {
	// Path identifies the project file to write.
	Path string `json:"path"`
	// Content contains the complete replacement file text.
	Content string `json:"content"`
}

// editArguments is the transport-local edit input.
type editArguments struct {
	// Path identifies the project file to edit.
	Path string `json:"path"`
	// Edits contains exact non-overlapping replacements.
	Edits []Replacement `json:"edits"`
}

// bashArguments is the transport-local bash input.
type bashArguments struct {
	// Command contains the shell command text.
	Command string `json:"command"`
	// Timeout contains the execution limit in seconds.
	Timeout mo.Option[float64] `json:"timeout"`
}

// bashTimeoutError distinguishes a tool timeout from caller cancellation.
type bashTimeoutError struct {
	// seconds contains the configured execution limit.
	seconds float64
}

// Error returns the model-visible timeout outcome.
func (e bashTimeoutError) Error() string {
	return fmt.Sprintf("bash command timed out after %g seconds", e.seconds)
}

// grepArguments is the transport-local grep input.
type grepArguments struct {
	// Pattern contains the search expression.
	Pattern string `json:"pattern"`
	// Path limits search to one project path.
	Path string `json:"path"`
	// Glob limits search to matching project files.
	Glob string `json:"glob"`
	// IgnoreCase enables case-insensitive matching.
	IgnoreCase bool `json:"ignoreCase"`
	// Literal treats Pattern as literal text.
	Literal bool `json:"literal"`
	// Context is the number of surrounding lines to include.
	Context uint `json:"context"`
	// Limit is the maximum number of matches.
	Limit mo.Option[uint] `json:"limit"`
}

// findArguments is the transport-local find input.
type findArguments struct {
	// Pattern contains the file name glob.
	Pattern string `json:"pattern"`
	// Path limits search to one project path.
	Path string `json:"path"`
	// Limit is the maximum number of results.
	Limit mo.Option[uint] `json:"limit"`
}

// listArguments is the transport-local ls input.
type listArguments struct {
	// Path identifies the project directory to list.
	Path string `json:"path"`
	// Limit is the maximum number of entries.
	Limit mo.Option[uint] `json:"limit"`
}
