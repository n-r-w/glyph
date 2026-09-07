package agent

import "errors"

// ErrPersistenceUnavailable identifies agent history that could not become durable.
var ErrPersistenceUnavailable = errors.New("session persistence failed")
