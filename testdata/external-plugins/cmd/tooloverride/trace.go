package main

import (
	"encoding/json/v2"
)

const (
	// traceEnvironment names a file that receives one line per handler invocation and tool call when set.
	traceEnvironment = "GLYPH_TOOLOVERRIDE_TRACE"
	// handlerTrace names the trace lines of handler invocations.
	handlerTrace = "handler"
	// toolTrace names the trace lines of tool runs.
	toolTrace = "tool"
)

// callValue returns the value property of the JSON arguments of a call.
func callValue(arguments []byte) (string, error) {
	var input struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil {
		return "", err
	}
	return input.Value, nil
}

// trace appends the line "<executable>\t<event>\t<tool>" to the trace file when tracing is enabled.
func (s *service) trace(event, toolName string) error {
	return s.traceLog.Append(s.mode + "\t" + event + "\t" + toolName)
}
