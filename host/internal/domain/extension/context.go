package extension

// Context identifies one issued runtime-to-active-session binding.
type Context struct {
	// ID identifies the issued binding independently of durable session identity.
	ID string
	// ExtensionID identifies the extension that received the binding.
	ExtensionID string
	// RuntimeInstanceID identifies the process incarnation that received the binding.
	RuntimeInstanceID string
	// SessionID identifies the bound durable session.
	SessionID string
	// WorkingDirectory is the bound session's canonical project directory.
	WorkingDirectory string
}

// ContextRef identifies the binding used by an extension-initiated operation.
type ContextRef struct {
	// ID identifies the issued binding.
	ID string
	// RuntimeInstanceID identifies the requesting process incarnation.
	RuntimeInstanceID string
	// SessionID identifies the bound durable session.
	SessionID string
}
