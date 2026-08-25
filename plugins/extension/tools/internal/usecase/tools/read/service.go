// Package read implements bounded project-file reads.
package read

import (
	"context"
	"fmt"
	"strings"

	"github.com/samber/mo"

	extensioncontroller "github.com/n-r-w/glyph/plugins/extension/tools/internal/controller/extension"
	"github.com/n-r-w/glyph/plugins/extension/tools/internal/core/textbudget"
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
func (s *Service) Read(
	ctx context.Context,
	path string,
	offset, limit mo.Option[uint],
) (extensioncontroller.ReadResult, error) {
	content, err := s.projectReader.ReadFile(ctx, path, offset, limit)
	if err != nil {
		return extensioncontroller.ReadResult{}, fmt.Errorf("read project file %q: %w", path, err)
	}
	if image, ok := content.Image.Get(); ok {
		return extensioncontroller.ReadResult{
			Text: mo.None[string](),
			Image: mo.Some(extensioncontroller.ReadImage{
				MediaType: image.MediaType,
				Data:      image.Data,
			}),
		}, nil
	}
	if oversizedSize, ok := content.OversizedSize.Get(); ok {
		start := content.Start.OrEmpty()
		command := fmt.Sprintf(
			"sed -n '%dp' %s | head -c %d",
			start, shellQuote(path), textbudget.MaximumBytes,
		)
		message := fmt.Sprintf(
			"[Line %d is %d bytes and exceeds the %d byte limit. Use `%s` to inspect that line.]",
			start, oversizedSize, textbudget.MaximumBytes, command,
		)
		if next, hasNext := content.Next.Get(); hasNext {
			message = fmt.Sprintf(
				"[Line %d is %d bytes and leaves no room for the required continuation notice. "+
					"Use `%s` to inspect that line. Use offset=%d to continue.]",
				start, oversizedSize, command, next,
			)
		}
		return extensioncontroller.ReadResult{Text: mo.Some(message), Image: mo.None[extensioncontroller.ReadImage]()}, nil
	}
	text := content.Text.OrEmpty()
	if next, ok := content.Next.Get(); ok {
		start := content.Start.OrEmpty()
		end := content.End.OrEmpty()
		total := content.Total.OrEmpty()
		notice := fmt.Sprintf(
			"[Showing lines %d-%d of %d. Use offset=%d to continue.]",
			start, end, total, next,
		)
		if strings.HasSuffix(text, "\n") {
			text += notice
		} else {
			text += "\n" + notice
		}
	}
	return extensioncontroller.ReadResult{Text: mo.Some(text), Image: mo.None[extensioncontroller.ReadImage]()}, nil
}
