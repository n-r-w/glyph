package session

// QueuedExtensionMessage retains the accepted session and runtime identity for delayed persistence.
type QueuedExtensionMessage struct {
	// Session identifies the exact active-session incarnation at acceptance.
	Session Identity
	// RuntimeInstanceID identifies the originating extension process captured at Host acceptance.
	RuntimeInstanceID string
	// Message contains the original extension origin and model-visible payload.
	Message ExtensionMessage
}
