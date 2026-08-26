// Package events coordinates Host run execution and client event delivery.
package events

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=events

// OperationGate reserves agent execution against active-session replacement.
type OperationGate interface {
	// TryAcquire reserves execution without waiting and returns an idempotent release function on success.
	TryAcquire() (release func(), acquired bool)
}
