package main

import (
	"strings"
)

// traceEnvironment names a file that receives one line per handler invocation and tool call when set.
const traceEnvironment = "GLYPH_TOOLPOLICY_TRACE"

// trace appends one line with a name and tab-separated payload fields to the trace file when tracing is enabled.
func (s *service) trace(name string, fields ...[]byte) error {
	payload := make([]string, len(fields))
	for index, field := range fields {
		payload[index] = string(field)
	}
	return s.traceLog.Append(name + "\t" + strings.Join(payload, "\t"))
}
