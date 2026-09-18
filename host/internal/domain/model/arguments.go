package model

import "encoding/json/v2"

// ToolCallArguments contains one immutable finalized JSON argument value.
type ToolCallArguments struct {
	// value retains the accepted JSON byte sequence as an immutable string.
	value string
}

// NewToolCallArguments validates and retains one finalized JSON argument value.
func NewToolCallArguments(arguments []byte) (ToolCallArguments, error) {
	if err := json.Unmarshal(arguments, new(map[string]any)); err != nil {
		return ToolCallArguments{}, err
	}
	return ToolCallArguments{value: string(arguments)}, nil
}

// Bytes returns a detached copy of the retained JSON bytes.
func (arguments ToolCallArguments) Bytes() []byte {
	return []byte(arguments.value)
}

// String returns the retained JSON without re-encoding it.
func (arguments ToolCallArguments) String() string {
	return arguments.value
}

// Len returns the retained JSON byte length.
func (arguments ToolCallArguments) Len() int {
	return len(arguments.value)
}
