package agent

import "errors"

// ErrPersistenceUnavailable identifies agent history that could not become durable.
var ErrPersistenceUnavailable = errors.New("session persistence failed")

// ErrRunNotActive identifies a message for an agent run when no agent run is active.
var ErrRunNotActive = errors.New("agent run is not active")
