// Package project reads files relative to the extension process working directory.
package project

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	edittool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/edit"
	readtool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/read"
)

const fileWritePermissions = 0o600

// Service provides working-project file access.
type Service struct{}

var (
	_ edittool.ProjectEditor = (*Service)(nil)
	_ readtool.ProjectReader = (*Service)(nil)
)

// New creates a working-project filesystem service.
func New() *Service {
	return &Service{}
}

// ReadFile returns the complete content of one project file.
func (s *Service) ReadFile(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("read project file %q: %w", path, err)
	}

	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("read project file %q: %w", path, err)
	}
	return string(content), nil
}

// WriteFile replaces complete project-file content directly.
func (s *Service) WriteFile(ctx context.Context, path, content string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("write project file %q: %w", path, err)
	}
	if err := os.WriteFile(filepath.Clean(path), []byte(content), fileWritePermissions); err != nil {
		return fmt.Errorf("write project file %q: %w", path, err)
	}
	return nil
}
