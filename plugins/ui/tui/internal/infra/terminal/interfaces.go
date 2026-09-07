// Package terminal owns framework execution, terminal rendering, and immutable display output.
package terminal

import (
	"context"
	"io"

	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=terminal

// Device opens the controlling-terminal files used by the runtime.
type Device interface {
	Open() (Files, error)
}

// Files owns the controlling-terminal input, output, and cleanup.
type Files interface {
	Input() io.Reader
	Output() io.Writer
	Close() error
}

// NotificationSource supplies the existing SDK notification stream to the terminal receive pump.
type NotificationSource interface {
	ReceiveNotification(context.Context) (*uisdk.Notification, error)
}
