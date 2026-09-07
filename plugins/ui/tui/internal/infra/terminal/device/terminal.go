// Package device opens and closes the controlling terminal used by Bubble Tea.
package device

import (
	"errors"
	"fmt"
	"io"
	"os"

	terminalinfra "github.com/n-r-w/glyph/plugins/ui/tui/internal/infra/terminal"

	tea "charm.land/bubbletea/v2"
)

// openTTY abstracts Bubble Tea controlling-terminal acquisition for focused tests.
type openTTY func() (*os.File, *os.File, error)

// Service opens controlling-terminal sessions.
type Service struct {
	// open acquires controlling-terminal input and output files.
	open openTTY
}

var _ terminalinfra.Device = (*Service)(nil)

// Open creates one terminal session without using process standard streams.
func (service *Service) Open() (terminalinfra.Files, error) {
	input, output, err := service.open()
	if err != nil {
		return nil, fmt.Errorf("open controlling terminal: %w", err)
	}
	return &Session{input: input, output: output}, nil
}

// Session owns the input and output files for one TUI program.
type Session struct {
	// input is the controlling-terminal input file.
	input *os.File
	// output is the controlling-terminal output file.
	output *os.File
}

var _ terminalinfra.Files = (*Session)(nil)

// Input returns the controlling-terminal input file.
func (session *Session) Input() io.Reader {
	return session.input
}

// Output returns the controlling-terminal output file.
func (session *Session) Output() io.Writer {
	return session.output
}

// Close closes both controlling-terminal files.
func (session *Session) Close() error {
	if session.input == session.output {
		return session.input.Close()
	}
	return errors.Join(session.input.Close(), session.output.Close())
}

// New creates a controlling-terminal service backed by tea.OpenTTY.
func New() *Service {
	return newWithOpen(tea.OpenTTY)
}

// newWithOpen builds a terminal service around one controlling-terminal opener.
func newWithOpen(open openTTY) *Service {
	return &Service{open: open}
}
