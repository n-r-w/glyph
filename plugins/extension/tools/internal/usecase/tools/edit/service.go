// Package edit replaces one uniquely occurring text fragment in a project file.
package edit

import (
	"context"
	"fmt"
	"strings"

	extensioncontroller "github.com/n-r-w/glyph/plugins/extension/tools/internal/controller/extension"
)

// Service coordinates exact project-file replacement.
type Service struct {
	projectEditor ProjectEditor
}

var _ extensioncontroller.EditTool = (*Service)(nil)

// New creates an edit service backed by project file access.
func New(projectEditor ProjectEditor) *Service { return &Service{projectEditor: projectEditor} }

// Edit replaces one unique exact text fragment.
func (s *Service) Edit(ctx context.Context, path, oldText, newText string) error {
	content, err := s.projectEditor.ReadFile(ctx, path)
	if err != nil {
		return fmt.Errorf("read file for edit %q: %w", path, err)
	}
	if strings.Count(content, oldText) != 1 {
		return fmt.Errorf("source fragment must occur exactly once in %q", path)
	}
	updated := strings.Replace(content, oldText, newText, 1)
	writeErr := s.projectEditor.WriteFile(ctx, path, updated)
	if writeErr != nil {
		return fmt.Errorf("write edited file %q: %w", path, writeErr)
	}
	return nil
}
