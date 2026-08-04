package cli

import (
	"fmt"
	"io"
	"strings"
)

// WriteWarning writes one pre-mode terminal warning.
func WriteWarning(writer io.Writer, text string) error {
	line := "[warning] " + text
	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	written, err := io.WriteString(writer, line)
	if err != nil {
		return fmt.Errorf("write CLI warning: %w", err)
	}
	if written != len(line) {
		return fmt.Errorf("write CLI warning: %w", io.ErrShortWrite)
	}
	return nil
}
