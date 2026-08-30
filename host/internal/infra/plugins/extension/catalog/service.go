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
	extensionservice "github.com/n-r-w/glyph/host/internal/usecase/host/extensions"
)

// Service discovers filesystem extension candidates.
type Service struct{}

var _ extensionservice.Catalog = (*Service)(nil)

// New creates an extension catalog service.
func New() *Service { return &Service{} }

// Discover returns normalized executable candidates.
func (s *Service) Discover(
	ctx context.Context,
	directory extensionservice.Directory,
) (extensionservice.Discovery, error) {
	if err := ctx.Err(); err != nil {
		return extensionservice.Discovery{}, fmt.Errorf("discover extension catalog: %w", err)
	}
	entries, err := os.ReadDir(filepath.Clean(directory.Path))
	if err != nil {
		if !directory.Explicit && errors.Is(err, os.ErrNotExist) {
			return extensionservice.Discovery{Candidates: nil, Issues: nil}, nil
		}
		if !directory.Explicit {
			return extensionservice.Discovery{
				Candidates: nil,
				Issues:     []extensionservice.Issue{{PluginIDs: nil, Path: directory.Path, Err: err}},
			}, nil
		}
		return extensionservice.Discovery{}, fmt.Errorf("read explicit extension directory %q: %w", directory.Path, err)
	}

	groups := make(map[string][]extensionservice.Candidate)
	issues := make([]extensionservice.Issue, 0)
	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil {
			issues = append(issues, extensionservice.Issue{
				PluginIDs: nil,
				Path:      filepath.Join(directory.Path, entry.Name()),
				Err:       infoErr,
			})
			continue
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		candidate := extensionservice.Candidate{
			ID: pluginid.Normalize(entry.Name()), Path: filepath.Join(directory.Path, entry.Name()),
		}
		if candidate.ID == "" {
			issues = append(issues, extensionservice.Issue{
				PluginIDs: nil,
				Path:      candidate.Path,
				Err:       errors.New("extension candidate has an empty normalized ID"),
			})
			continue
		}
		groups[candidate.ID] = append(groups[candidate.ID], candidate)
	}

	candidates := make([]extensionservice.Candidate, 0, len(groups))
	for id, group := range groups {
		if len(group) > 1 {
			for _, candidate := range group {
				issues = append(issues, extensionservice.Issue{
					PluginIDs: []string{id},
					Path:      candidate.Path,
					Err:       errors.New("extension candidate ID is duplicated"),
				})
			}
			continue
		}
		candidates = append(candidates, group[0])
	}
	slices.SortFunc(candidates, func(left, right extensionservice.Candidate) int {
		return cmp.Compare(left.ID, right.ID)
	})
	slices.SortFunc(issues, func(left, right extensionservice.Issue) int {
		return cmp.Compare(left.Path, right.Path)
	})
	return extensionservice.Discovery{Candidates: candidates, Issues: issues}, nil
}
