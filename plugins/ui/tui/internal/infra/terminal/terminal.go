// Package terminal opens and closes the controlling terminal used by Bubble Tea.
package terminal

import (
	"errors"
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"

	plugincontroller "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
)

// openTTY abstracts Bubble Tea controlling-terminal acquisition for focused tests.
type openTTY func() (*os.File, *os.File, error)

// Service opens controlling-terminal sessions.
type Service struct {
	open openTTY
}

// Session owns the input and output files for one TUI program.
type Session struct {
	input  *os.File
	output *os.File
}

// New creates a controlling-terminal service backed by tea.OpenTTY.
func New() *Service {
	return newWithOpen(tea.OpenTTY)
}

// newWithOpen builds a terminal service around one controlling-terminal opener.
func newWithOpen(open openTTY) *Service {
	return &Service{open: open}
}

// Open creates one terminal session without using process standard streams.
func (service *Service) Open() (plugincontroller.TerminalSession, error) {
	input, output, err := service.open()
	if err != nil {
		return nil, fmt.Errorf("open controlling terminal: %w", err)
	}
	return &Session{input: input, output: output}, nil
}

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

var (
	_ plugincontroller.Terminal        = (*Service)(nil)
	_ plugincontroller.TerminalSession = (*Session)(nil)
)
