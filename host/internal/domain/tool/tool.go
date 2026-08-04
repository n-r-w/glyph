// Package tool defines provider-neutral tools and their execution values.
package tool

import "fmt"

// Descriptor describes one model-callable tool.
type Descriptor struct {
	Name            string
	Description     string
	InputSchemaJSON []byte
}

// ProgressChannel identifies the meaning of one progress fragment.
type ProgressChannel uint8

const (
	// ProgressChannelStatus carries human-readable lifecycle status.
	ProgressChannelStatus ProgressChannel = iota + 1
	// ProgressChannelStdout carries command standard output.
	ProgressChannelStdout
	// ProgressChannelStderr carries command standard error.
	ProgressChannelStderr
)

// Progress is one ordered tool-execution progress fragment.
type Progress struct {
	Channel ProgressChannel
	Content string
}

// Result is one terminal tool-execution outcome.
type Result struct {
	Content string
	IsError bool
}

// ProgressHandler consumes progress in execution order.
type ProgressHandler func(progress Progress) error

// RuntimeUnavailableCondition classifies why a registered extension became unavailable.
type RuntimeUnavailableCondition uint8

const (
	// RuntimeUnavailableProcessExited means the extension process ended after successful startup.
	RuntimeUnavailableProcessExited RuntimeUnavailableCondition = iota + 1
)

// RuntimeFailure identifies one post-start extension availability failure.
type RuntimeFailure struct {
	PluginID  string
	Condition RuntimeUnavailableCondition
}

// Message returns the safe user-visible runtime failure text.
func (failure RuntimeFailure) Message() (string, error) {
	if failure.Condition != RuntimeUnavailableProcessExited {
		return "", fmt.Errorf("unknown runtime unavailability condition %d", failure.Condition)
	}
	return fmt.Sprintf("extension %s unavailable: extension process exited", failure.PluginID), nil
}
