package project

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"path/filepath"

	"github.com/bmatcuk/doublestar/v4"

	searchtool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/search"
)

// Find returns matching files and symbolic links without entering linked directories.
func (s *Service) Find(ctx context.Context, cmd searchtool.FindCommand) (searchtool.FindResult, error) {
	if !doublestar.ValidatePattern(filepath.ToSlash(cmd.Pattern)) {
		return searchtool.FindResult{}, errors.New("invalid find glob")
	}
	root := cmd.Path
	if root == "" {
		root = "."
	}
	limit := cmd.Limit.OrElse(findDefaultLimit)
	output := newSearchOutput()
	count, limited := uint(0), false
	walkErr := walkProject(ctx, root, func(path string, entry fs.DirEntry) error {
		if entry.IsDir() {
			return nil
		}
		relative, relativeErr := projectRelative(path)
		if relativeErr != nil {
			return relativeErr
		}
		matched, matchErr := doublestar.Match(filepath.ToSlash(cmd.Pattern), filepath.ToSlash(relative))
		if matchErr != nil {
			return fmt.Errorf("match find glob: %w", matchErr)
		}
		if !matched {
			return nil
		}
		if count == limit {
			limited = true
			return io.EOF
		}
		count++
		output.add(escapeDisplayedName(relative) + "\n")
		if output.truncated {
			return io.EOF
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, io.EOF) {
		return searchtool.FindResult{}, fmt.Errorf("find project files: %w", walkErr)
	}
	if limited {
		output.notice("[Result limit reached.]\n")
	}
	if output.truncated {
		output.notice("[Output limit reached.]\n")
	}
	return searchtool.FindResult{Text: output.text()}, nil
}
