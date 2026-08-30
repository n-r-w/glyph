// Package catalog discovers executable UI plugin candidates.
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
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// Service discovers one complete UI catalog.
type Service struct{}

var _ hostui.Catalog = (*Service)(nil)

// New creates a UI catalog service.
func New() *Service {
	return &Service{}
}

// Discover returns the valid executable candidates in the effective directory.
func (*Service) Discover(ctx context.Context, directory domainui.Directory) (domainui.Discovery, error) {
	if err := ctx.Err(); err != nil {
		return domainui.Discovery{}, fmt.Errorf("discover UI catalog: %w", err)
	}
	entries, err := os.ReadDir(filepath.Clean(directory.Path))
	if err != nil {
		return domainui.Discovery{}, fmt.Errorf("read UI directory %q: %w", directory.Path, err)
	}

	groups := make(map[string][]domainui.Candidate)
	var catalogErr error
	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil {
			catalogErr = errors.Join(catalogErr, fmt.Errorf("inspect UI candidate %q: %w", entry.Name(), infoErr))
			continue
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		candidate := domainui.Candidate{
			ID:   pluginid.Normalize(entry.Name()),
			Path: filepath.Join(directory.Path, entry.Name()),
		}
		if candidate.ID == "" {
			catalogErr = errors.Join(
				catalogErr,
				fmt.Errorf("UI candidate %q has an empty normalized ID", candidate.Path),
			)
			continue
		}
		groups[candidate.ID] = append(groups[candidate.ID], candidate)
	}

	candidates := make([]domainui.Candidate, 0, len(groups))
	for id, group := range groups {
		if len(group) > 1 {
			catalogErr = errors.Join(catalogErr, fmt.Errorf("UI candidate duplicate normalized ID %q", id))
			continue
		}
		candidates = append(candidates, group[0])
	}
	if catalogErr != nil {
		return domainui.Discovery{}, fmt.Errorf("validate UI catalog: %w", catalogErr)
	}
	slices.SortFunc(candidates, func(left, right domainui.Candidate) int {
		return cmp.Compare(left.ID, right.ID)
	})
	return domainui.Discovery{Candidates: candidates}, nil
}
