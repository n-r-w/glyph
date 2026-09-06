// Package catalog discovers executable extension candidates from one directory.
package catalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/n-r-w/glyph/host/internal/domain/pluginid"
	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
)

// Service discovers filesystem extension candidates.
type Service struct{}

var _ extensionruntime.Catalog = (*Service)(nil)

// New creates an extension catalog service.
func New() *Service { return &Service{} }

// Discover returns executable observations and complete filesystem failures without acceptance policy.
func (s *Service) Discover(
	ctx context.Context,
	directory extensionruntime.Directory,
) (extensionruntime.Discovery, error) {
	if err := ctx.Err(); err != nil {
		return extensionruntime.Discovery{}, fmt.Errorf("discover extension catalog: %w", err)
	}
	entries, err := os.ReadDir(filepath.Clean(directory.Path))
	candidates := make([]extensionruntime.Executable, 0, len(entries))
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
		candidates = append(candidates, extensionruntime.Executable{
			ID:   pluginid.Normalize(entry.Name()),
			Path: filepath.Join(directory.Path, entry.Name()),
		})
	}
	return extensionruntime.Discovery{Candidates: candidates, Issues: issues, DirectoryError: err}, nil
}
