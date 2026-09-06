package extensionruntime

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// acceptDiscovery applies extension directory and candidate acceptance before any process starts.
func (s *Service) acceptDiscovery(directory startup.Directory, discovery Discovery) (Discovery, error) {
	if discovery.DirectoryError != nil {
		if directory.Explicit {
			return Discovery{}, fmt.Errorf(
				"read explicit extension directory %q: %w",
				directory.Path,
				discovery.DirectoryError,
			)
		}
		if errors.Is(discovery.DirectoryError, os.ErrNotExist) {
			return Discovery{Candidates: nil, Issues: nil, DirectoryError: nil}, nil
		}
		return Discovery{
			Candidates:     nil,
			Issues:         []Issue{{PluginIDs: nil, Path: directory.Path, Err: discovery.DirectoryError}},
			DirectoryError: nil,
		}, nil
	}
	groups := make(map[string][]Executable)
	issues := slices.Clone(discovery.Issues)
	for _, candidate := range discovery.Candidates {
		if candidate.ID == "" {
			issues = append(
				issues,
				Issue{
					PluginIDs: nil,
					Path:      candidate.Path,
					Err:       errors.New("extension candidate has an empty normalized ID"),
				},
			)
			continue
		}
		groups[candidate.ID] = append(groups[candidate.ID], candidate)
	}
	candidates := make([]Executable, 0, len(groups))
	for id, group := range groups {
		if len(group) > 1 {
			for _, candidate := range group {
				issues = append(
					issues,
					Issue{
						PluginIDs: []string{id},
						Path:      candidate.Path,
						Err:       errors.New("extension candidate ID is duplicated"),
					},
				)
			}
			continue
		}
		candidates = append(candidates, group[0])
	}
	slices.SortFunc(candidates, func(left, right Executable) int { return cmp.Compare(left.ID, right.ID) })
	slices.SortFunc(issues, func(left, right Issue) int { return cmp.Compare(left.Path, right.Path) })
	return Discovery{Candidates: candidates, Issues: issues, DirectoryError: nil}, nil
}
