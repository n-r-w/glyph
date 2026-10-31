package agent

import "fmt"

// MessageDelivery identifies the point of an agent run at which Agent Core gives a queued message to the model.
type MessageDelivery uint8

const (
	// MessageDeliverySteer gives the message to the next model request after the tools of the current turn finish.
	MessageDeliverySteer MessageDelivery = iota + 1
	// MessageDeliveryFollowUp gives the message to the model only when the run has no tool calls and no steer messages.
	MessageDeliveryFollowUp
)

// String returns the delivery name used in failure text.
func (delivery MessageDelivery) String() string {
	switch delivery {
	case MessageDeliverySteer:
		return "steer"
	case MessageDeliveryFollowUp:
		return "followUp"
	default:
		return fmt.Sprintf("unknown(%d)", uint8(delivery))
	}
}
