// Package catalog discovers executable UI plugin candidates.
package catalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/n-r-w/glyph/host/internal/domain/pluginid"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// Service discovers filesystem UI candidates.
type Service struct{}

var _ hostui.Catalog = (*Service)(nil)

// New creates a UI catalog service.
func New() *Service { return &Service{} }

// Discover returns executable observations and filesystem failures without selecting candidates.
func (*Service) Discover(ctx context.Context, directory hostui.Directory) (hostui.Discovery, error) {
	if err := ctx.Err(); err != nil {
		return hostui.Discovery{}, fmt.Errorf("discover UI catalog: %w", err)
	}
	entries, err := os.ReadDir(filepath.Clean(directory.Path))
	candidates := make([]hostui.Candidate, 0, len(entries))
	var failures []hostui.CandidateFailure
	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil {
			failures = append(failures, hostui.CandidateFailure{Name: entry.Name(), Err: infoErr})
			continue
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		candidates = append(candidates, hostui.Candidate{
			ID:   pluginid.Normalize(entry.Name()),
			Path: filepath.Join(directory.Path, entry.Name()),
		})
	}
	return hostui.Discovery{Candidates: candidates, Failures: failures, DirectoryError: err}, nil
}
