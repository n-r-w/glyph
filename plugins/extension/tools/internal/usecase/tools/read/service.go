// Package read implements the standard tool that reads complete project files.
package read

import (
	"context"
	"fmt"

	extensioncontroller "github.com/n-r-w/glyph/plugins/extension/tools/internal/controller/extension"
)

// Service coordinates reading one project file.
type Service struct {
	projectReader ProjectReader
}

var _ extensioncontroller.ReadTool = (*Service)(nil)

// New creates a read service backed by the working-project filesystem.
func New(projectReader ProjectReader) *Service {
	return &Service{projectReader: projectReader}
}

// Read returns the complete content of the requested project file.
func (s *Service) Read(ctx context.Context, path string) (string, error) {
	content, err := s.projectReader.ReadFile(ctx, path)
	if err != nil {
		return "", fmt.Errorf("read project file %q: %w", path, err)
	}
	return content, nil
}
