// Package read implements bounded project-file reads.
package read

import (
	"context"
	"fmt"
	"strings"

	extensioncontroller "github.com/n-r-w/glyph/plugins/extension/tools/internal/controller/extension"
)

// Service coordinates bounded project-file reads.
type Service struct{ projectReader ProjectReader }

var _ extensioncontroller.ReadTool = (*Service)(nil)

// shellQuote returns one POSIX shell literal.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// New creates a bounded read service.
func New(projectReader ProjectReader) *Service { return &Service{projectReader: projectReader} }

// Read returns text or typed image content.
func (s *Service) Read(ctx context.Context, path string, offset, limit uint) (extensioncontroller.ReadResult, error) {
	content, err := s.projectReader.ReadFile(ctx, path, offset, limit)
	if err != nil {
		return extensioncontroller.ReadResult{Text: "", Image: nil}, fmt.Errorf("read project file %q: %w", path, err)
	}
	if content.Image != nil {
		return extensioncontroller.ReadResult{
			Text: "",
			Image: &extensioncontroller.ReadImage{
				MediaType: content.Image.MediaType,
				Data:      content.Image.Data,
			},
		}, nil
	}
	if content.OversizedSize > 0 {
		command := fmt.Sprintf(
			"sed -n '%dp' %s | head -c %d",
			content.Start, shellQuote(path), MaximumTextBytes,
		)
		message := fmt.Sprintf(
			"[Line %d is %d bytes and exceeds the %d byte limit. Use `%s` to inspect that line.]",
			content.Start, content.OversizedSize, MaximumTextBytes, command,
		)
		if content.Next > 0 {
			message = fmt.Sprintf(
				"[Line %d is %d bytes and leaves no room for the required continuation notice. "+
					"Use `%s` to inspect that line. Use offset=%d to continue.]",
				content.Start, content.OversizedSize, command, content.Next,
			)
		}
		return extensioncontroller.ReadResult{Text: message, Image: nil}, nil
	}
	text := content.Text
	if content.Next > 0 {
		notice := fmt.Sprintf(
			"[Showing lines %d-%d of %d. Use offset=%d to continue.]",
			content.Start, content.End, content.Total, content.Next,
		)
		if strings.HasSuffix(text, "\n") {
			text += notice
		} else {
			text += "\n" + notice
		}
	}
	return extensioncontroller.ReadResult{Text: text, Image: nil}, nil
}
