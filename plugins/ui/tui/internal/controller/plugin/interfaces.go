package plugin

import (
	"io"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// Terminal opens an independent controlling terminal for Bubble Tea.
type Terminal interface {
	Open() (TerminalSession, error)
}

// TerminalSession owns the input and output files opened for one UI stream.
type TerminalSession interface {
	Input() io.Reader
	Output() io.Writer
	Close() error
}

// ProgramFactory creates the one Bubble Tea program for an initialized stream.
type ProgramFactory interface {
	New(initial presentationdomain.Event, input io.Reader, output io.Writer, emit Emit) Program
}

// Program exposes only lifecycle operations needed by the plugin controller.
type Program interface {
	Send(presentationdomain.Event)
	Quit()
	Run() error
}

// Emit synchronously sends one accepted UI command to the Host.
type Emit func(presentationdomain.Command) error

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=plugin
