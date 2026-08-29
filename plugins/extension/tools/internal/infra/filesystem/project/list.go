package project

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"slices"

	searchtool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/search"
)

// List returns direct directory entries in bounded batches.
func (s *Service) List(ctx context.Context, cmd searchtool.ListCommand) (searchtool.ListResult, error) {
	path := cmd.Path
	if path == "" {
		path = "."
	}
	limit := cmd.Limit.OrElse(listDefaultLimit)
	output := newSearchOutput()
	// #nosec G304 -- the caller selects a project directory.
	dir, openErr := os.Open(path)
	if openErr != nil {
		return searchtool.ListResult{}, fmt.Errorf("list project directory: %w", openErr)
	}
	defer func() { _ = dir.Close() }()
	count := uint(0)
	for {
		if contextErr := ctx.Err(); contextErr != nil {
			return searchtool.ListResult{}, contextErr
		}
		entries, readErr := dir.ReadDir(directoryBatchSize)
		slices.SortFunc(entries, func(left, right fs.DirEntry) int {
			return cmp.Compare(left.Name(), right.Name())
		})
		for _, entry := range entries {
			if count == limit {
				output.notice("[Entry limit reached.]\n")
				return searchtool.ListResult{Text: output.text()}, nil
			}
			count++
			name := entry.Name()
			info, statErr := os.Stat(filepath.Join(path, name))
			if statErr == nil && info.IsDir() {
				name += "/"
			} else if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
				return searchtool.ListResult{}, statErr
			}
			output.add(escapeDisplayedName(name) + "\n")
			if output.truncated {
				output.notice("[Output limit reached.]\n")
				return searchtool.ListResult{Text: output.text()}, nil
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return searchtool.ListResult{}, readErr
		}
	}
	return searchtool.ListResult{Text: output.text()}, nil
}
