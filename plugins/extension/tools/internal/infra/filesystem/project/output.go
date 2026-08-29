package project

import (
	"github.com/n-r-w/glyph/plugins/extension/tools/internal/core/textbudget"
)

// searchOutput retains complete output lines within the shared text budget.
type searchOutput struct {
	// lines contains complete model-visible output lines.
	lines []string
	// bytes is the current model-visible byte count.
	bytes int
	// truncated reports whether the shared text budget was reached.
	truncated bool
}

// newSearchOutput creates an empty bounded output accumulator.
func newSearchOutput() *searchOutput { return &searchOutput{lines: nil, bytes: 0, truncated: false} }

// add appends one complete line or records that the output budget was reached.
func (o *searchOutput) add(line string) {
	if o.truncated || len(o.lines) == textbudget.MaximumLines || o.bytes+len(line) > textbudget.MaximumBytes {
		o.truncated = true
		return
	}
	o.lines = append(o.lines, line)
	o.bytes += len(line)
}

// notice reserves room by removing trailing output before appending one complete notice.
func (o *searchOutput) notice(line string) {
	for len(o.lines) > 0 && (len(o.lines) == textbudget.MaximumLines || o.bytes+len(line) > textbudget.MaximumBytes) {
		last := len(o.lines) - 1
		o.bytes -= len(o.lines[last])
		o.lines = o.lines[:last]
	}
	if len(o.lines) < textbudget.MaximumLines && o.bytes+len(line) <= textbudget.MaximumBytes {
		o.lines = append(o.lines, line)
		o.bytes += len(line)
	}
}
