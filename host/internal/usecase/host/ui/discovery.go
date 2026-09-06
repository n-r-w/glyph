package ui

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
)

// acceptDiscovery rejects the whole UI catalog before selection when any observation is invalid.
func (s *Selector) acceptDiscovery(directory Directory, discovery Discovery) ([]Candidate, error) {
	if discovery.DirectoryError != nil {
		return nil, fmt.Errorf("read UI directory %q: %w", directory.Path, discovery.DirectoryError)
	}
	var catalogErr error
	for _, failure := range discovery.Failures {
		catalogErr = errors.Join(catalogErr, fmt.Errorf("inspect UI candidate %q: %w", failure.Name, failure.Err))
	}
	groups := make(map[string][]Candidate)
	for _, candidate := range discovery.Candidates {
		if candidate.ID == "" {
			catalogErr = errors.Join(
				catalogErr,
				fmt.Errorf("UI candidate %q has an empty normalized ID", candidate.Path),
			)
			continue
		}
		groups[candidate.ID] = append(groups[candidate.ID], candidate)
	}
	candidates := make([]Candidate, 0, len(groups))
	for id, group := range groups {
		if len(group) > 1 {
			catalogErr = errors.Join(catalogErr, fmt.Errorf("UI candidate duplicate normalized ID %q", id))
			continue
		}
		candidates = append(candidates, group[0])
	}
	if catalogErr != nil {
		return nil, fmt.Errorf("validate UI catalog: %w", catalogErr)
	}
	slices.SortFunc(candidates, func(left, right Candidate) int { return cmp.Compare(left.ID, right.ID) })
	return candidates, nil
}
