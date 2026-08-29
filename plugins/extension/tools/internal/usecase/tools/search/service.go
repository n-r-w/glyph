package search

import (
	"context"

	extensioncontroller "github.com/n-r-w/glyph/plugins/extension/tools/internal/controller/extension"
)

// Service adapts search controller requests to project filesystem operations.
type Service struct {
	// projectFiles performs working-project search operations.
	projectFiles ProjectFiles
}

var _ extensioncontroller.SearchTool = (*Service)(nil)

// New creates a search service.
func New(projectFiles ProjectFiles) *Service { return &Service{projectFiles: projectFiles} }

// Grep searches project files.
func (s *Service) Grep(ctx context.Context, input extensioncontroller.GrepArguments) (string, error) {
	result, err := s.projectFiles.Grep(ctx, GrepCommand{
		Pattern: input.Pattern, Path: input.Path, Glob: input.Glob, IgnoreCase: input.IgnoreCase,
		Literal: input.Literal, Context: input.Context, Limit: input.Limit,
	})
	return result.Text, err
}

// Find returns project paths matching a glob.
func (s *Service) Find(ctx context.Context, input extensioncontroller.FindArguments) (string, error) {
	result, err := s.projectFiles.Find(ctx, FindCommand{Pattern: input.Pattern, Path: input.Path, Limit: input.Limit})
	return result.Text, err
}

// List returns direct directory entries.
func (s *Service) List(ctx context.Context, input extensioncontroller.ListArguments) (string, error) {
	result, err := s.projectFiles.List(ctx, ListCommand{Path: input.Path, Limit: input.Limit})
	return result.Text, err
}
