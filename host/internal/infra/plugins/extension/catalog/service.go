// Package catalog discovers executable extension candidates from one directory.
package catalog

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/n-r-w/glyph/host/internal/domain/pluginid"
	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
)

// Service discovers filesystem extension candidates.
type Service struct{}

var _ extensionruntime.Catalog = (*Service)(nil)

// New creates an extension catalog service.
func New() *Service { return &Service{} }

// Discover returns normalized executable candidates.
func (s *Service) Discover(
	ctx context.Context,
	directory extensionruntime.Directory,
) (extensionruntime.Discovery, error) {
	if err := ctx.Err(); err != nil {
		return extensionruntime.Discovery{}, fmt.Errorf("discover extension catalog: %w", err)
	}
	entries, err := os.ReadDir(filepath.Clean(directory.Path))
	if err != nil {
		if !directory.Explicit && errors.Is(err, os.ErrNotExist) {
			return extensionruntime.Discovery{Candidates: nil, Issues: nil}, nil
		}
		if !directory.Explicit {
			return extensionruntime.Discovery{
				Candidates: nil,
				Issues:     []extensionruntime.Issue{{PluginIDs: nil, Path: directory.Path, Err: err}},
			}, nil
		}
		return extensionruntime.Discovery{}, fmt.Errorf("read explicit extension directory %q: %w", directory.Path, err)
	}

	groups := make(map[string][]extensionruntime.Candidate)
	issues := make([]extensionruntime.Issue, 0)
	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil {
			issues = append(issues, extensionruntime.Issue{
				PluginIDs: nil,
				Path:      filepath.Join(directory.Path, entry.Name()),
				Err:       infoErr,
			})
			continue
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		candidate := extensionruntime.Candidate{
			ID: pluginid.Normalize(entry.Name()), Path: filepath.Join(directory.Path, entry.Name()),
		}
		if candidate.ID == "" {
			issues = append(issues, extensionruntime.Issue{
				PluginIDs: nil,
				Path:      candidate.Path,
				Err:       errors.New("extension candidate has an empty normalized ID"),
			})
			continue
		}
		groups[candidate.ID] = append(groups[candidate.ID], candidate)
	}

	candidates := make([]extensionruntime.Candidate, 0, len(groups))
	for id, group := range groups {
		if len(group) > 1 {
			for _, candidate := range group {
				issues = append(issues, extensionruntime.Issue{
					PluginIDs: []string{id},
					Path:      candidate.Path,
					Err:       errors.New("extension candidate ID is duplicated"),
				})
			}
			continue
		}
		candidates = append(candidates, group[0])
	}
	slices.SortFunc(candidates, func(left, right extensionruntime.Candidate) int {
		return cmp.Compare(left.ID, right.ID)
	})
	slices.SortFunc(issues, func(left, right extensionruntime.Issue) int {
		return cmp.Compare(left.Path, right.Path)
	})
	return extensionruntime.Discovery{Candidates: candidates, Issues: issues}, nil
}
