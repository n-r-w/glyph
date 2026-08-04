package ui

import (
	"context"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=ui

// Session owns one selected UI lifecycle after its stream opens.
type Session interface {
	Run(ctx context.Context, initialization domainui.Initialization) error
}
