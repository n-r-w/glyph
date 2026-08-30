package project

import (
	"cmp"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// walkProject visits directories in bounded batches and never follows symbolic links.
func walkProject(ctx context.Context, root string, visit func(string, fs.DirEntry) error) error {
	info, statErr := os.Lstat(root)
	if statErr != nil {
		return statErr
	}
	entry := fs.FileInfoToDirEntry(info)
	if visitErr := visit(root, entry); visitErr != nil {
		return visitErr
	}
	if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		return nil
	}
	return walkDirectory(ctx, root, visit)
}

// walkDirectory reads and visits one directory at a time in bounded entry batches.
func walkDirectory(ctx context.Context, path string, visit func(string, fs.DirEntry) error) error {
	// #nosec G304 -- traversal supplies project paths.
	dir, openErr := os.Open(path)
	if openErr != nil {
		return openErr
	}
	defer func() { _ = dir.Close() }()
	for {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		entries, readErr := dir.ReadDir(directoryBatchSize)
		slices.SortFunc(entries, func(left, right fs.DirEntry) int {
			return cmp.Compare(left.Name(), right.Name())
		})
		for _, entry := range entries {
			child := filepath.Join(path, entry.Name())
			if visitErr := visit(child, entry); visitErr != nil {
				return visitErr
			}
			if entry.IsDir() && entry.Type()&fs.ModeSymlink == 0 {
				if walkErr := walkDirectory(ctx, child, visit); walkErr != nil {
					return walkErr
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

// escapeDisplayedName keeps one filesystem name on one model-visible output line.
func escapeDisplayedName(name string) string {
	name = strings.ReplaceAll(name, "\r", `\r`)
	return strings.ReplaceAll(name, "\n", `\n`)
}

// projectRelative converts a filesystem path to a slash-separated project-relative path.
func projectRelative(path string) (string, error) {
	rel, err := filepath.Rel(".", path)
	return filepath.ToSlash(rel), err
}
