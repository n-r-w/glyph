// Package write implements complete project-file writes.
package write

import (
	"context"
	"fmt"

	extensioncontroller "github.com/n-r-w/glyph/plugins/extension/tools/internal/controller/extension"
)

// Service coordinates one complete file write.
type Service struct {
	// projectWriter writes complete working-project files.
	projectWriter ProjectWriter
}

var _ extensioncontroller.WriteTool = (*Service)(nil)

// New creates a write service backed by project file access.
func New(projectWriter ProjectWriter) *Service { return &Service{projectWriter: projectWriter} }

// Write replaces the requested project file.
func (s *Service) Write(ctx context.Context, path, content string) error {
	if err := s.projectWriter.WriteFile(ctx, path, content); err != nil {
		return fmt.Errorf("write project file %q: %w", path, err)
	}
	return nil
}
