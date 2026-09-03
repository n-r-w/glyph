// Package extension defines extension runtime domain values.
package extension

import "fmt"

// RuntimeUnavailableCondition classifies why a registered extension became unavailable.
type RuntimeUnavailableCondition uint8

const (
	// RuntimeUnavailableProcessExited means the extension process ended after successful startup.
	RuntimeUnavailableProcessExited RuntimeUnavailableCondition = iota + 1
)

// RuntimeFailure identifies one post-start extension availability failure.
type RuntimeFailure struct {
	// PluginID identifies the unavailable extension.
	PluginID string
	// Condition classifies why the extension became unavailable.
	Condition RuntimeUnavailableCondition
}

// Message returns the user-visible runtime failure text.
func (failure RuntimeFailure) Message() (string, error) {
	if failure.Condition != RuntimeUnavailableProcessExited {
		return "", fmt.Errorf("unknown runtime unavailability condition %d", failure.Condition)
	}
	return fmt.Sprintf("extension %s unavailable: extension process exited", failure.PluginID), nil
}
