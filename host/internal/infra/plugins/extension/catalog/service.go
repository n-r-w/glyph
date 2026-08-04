// Package catalog discovers executable extension candidates from one directory.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/n-r-w/glyph/host/internal/domain/pluginid"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
)

// Service discovers filesystem extension candidates.
type Service struct{}

var _ toolservice.Catalog = (*Service)(nil)

// New creates an extension catalog service.
func New() *Service { return &Service{} }

// Discover returns normalized executable candidates.
func (s *Service) Discover(ctx context.Context, directory toolservice.Directory) (toolservice.Discovery, error) {
	if err := ctx.Err(); err != nil {
		return toolservice.Discovery{}, fmt.Errorf("discover extension catalog: %w", err)
	}
	entries, err := os.ReadDir(filepath.Clean(directory.Path))
	if err != nil {
		if !directory.Explicit && errors.Is(err, os.ErrNotExist) {
			return toolservice.Discovery{Candidates: nil, Issues: nil}, nil
		}
		if !directory.Explicit {
			return toolservice.Discovery{
				Candidates: nil,
				Issues:     []toolservice.Issue{{PluginIDs: nil, Path: directory.Path, Err: err}},
			}, nil
		}
		return toolservice.Discovery{}, fmt.Errorf("read explicit extension directory %q: %w", directory.Path, err)
	}

	groups := make(map[string][]toolservice.Candidate)
	issues := make([]toolservice.Issue, 0)
	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil {
			issues = append(issues, toolservice.Issue{
				PluginIDs: nil,
				Path:      filepath.Join(directory.Path, entry.Name()),
				Err:       infoErr,
			})
			continue
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		candidate := toolservice.Candidate{
			ID: pluginid.Normalize(entry.Name()), Path: filepath.Join(directory.Path, entry.Name()),
		}
		if candidate.ID == "" {
			issues = append(issues, toolservice.Issue{
				PluginIDs: nil,
				Path:      candidate.Path,
				Err:       errors.New("extension candidate has an empty normalized ID"),
			})
			continue
		}
		groups[candidate.ID] = append(groups[candidate.ID], candidate)
	}

	candidates := make([]toolservice.Candidate, 0, len(groups))
	for id, group := range groups {
		if len(group) > 1 {
			for _, candidate := range group {
				issues = append(issues, toolservice.Issue{
					PluginIDs: []string{id},
					Path:      candidate.Path,
					Err:       errors.New("extension candidate ID is duplicated"),
				})
			}
			continue
		}
		candidates = append(candidates, group[0])
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	sort.Slice(issues, func(i, j int) bool { return issues[i].Path < issues[j].Path })
	return toolservice.Discovery{Candidates: candidates, Issues: issues}, nil
}
