package agent

// QueueMode controls how many pending messages of one kind Agent Core delivers at one delivery point.
type QueueMode uint8

const (
	// QueueModeOneAtATime delivers one pending message of the kind at each delivery point.
	QueueModeOneAtATime QueueMode = iota + 1
	// QueueModeAll delivers every pending message of the kind at a delivery point, in arrival order.
	QueueModeAll
)
