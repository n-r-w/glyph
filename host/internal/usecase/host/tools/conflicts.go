package tools

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// NameConflicts returns the deterministic issues of ordinary tool names that several extensions own.
func (s *Service) NameConflicts(registrations []startup.AcceptedRegistration) []startup.Issue {
	shared := sharedOwners(registrations, false)
	issues := make([]startup.Issue, 0, len(shared))
	for name, pluginIDs := range shared {
		issues = append(
			issues,
			startup.Issue{PluginIDs: pluginIDs, Path: "", Err: fmt.Errorf("tool name %q conflicts", name)},
		)
	}
	slices.SortFunc(
		issues,
		func(left, right startup.Issue) int { return cmp.Compare(left.Err.Error(), right.Err.Error()) },
	)
	return issues
}

// ReplacementConflicts returns the deterministic conflicts of tool names that several extensions declare a
// replacement of.
func (s *Service) ReplacementConflicts(registrations []startup.AcceptedRegistration) []startup.ReplacementConflict {
	shared := sharedOwners(registrations, true)
	conflicts := make([]startup.ReplacementConflict, 0, len(shared))
	for name, pluginIDs := range shared {
		conflicts = append(conflicts, startup.ReplacementConflict{
			ToolName: name,
			Issue: startup.Issue{
				PluginIDs: pluginIDs, Path: "",
				Err: fmt.Errorf("tool name %q has conflicting replacements; Host accepts none of them", name),
			},
		})
	}
	slices.SortFunc(
		conflicts,
		func(left, right startup.ReplacementConflict) int { return cmp.Compare(left.ToolName, right.ToolName) },
	)
	return conflicts
}

// sharedOwners returns the names with at least two owners among the ordinary descriptors or, when replacements is
// true, among the declared replacements, each with its sorted owner IDs.
func sharedOwners(registrations []startup.AcceptedRegistration, replacements bool) map[string][]string {
	owners := make(map[string][]string)
	for _, registration := range registrations {
		for index := range registration.Tools {
			if descriptor := &registration.Tools[index]; descriptor.ReplacesTool == replacements {
				owners[descriptor.Name] = append(owners[descriptor.Name], registration.ID)
			}
		}
	}
	for name, pluginIDs := range owners {
		if len(pluginIDs) < 2 { //nolint:mnd // A conflict needs at least two owners.
			delete(owners, name)
			continue
		}
		slices.Sort(pluginIDs)
	}
	return owners
}

// Commit publishes tools from registrations accepted by every startup validator, where a declared replacement hides
// the ordinary tool of its name together with that tool's schema and owner.
func (s *Service) Commit(registrations []startup.AcceptedRegistration) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for _, registration := range registrations {
		for index := range registration.Tools {
			descriptor := &registration.Tools[index]
			// An ordinary tool never overwrites a published replacement.
			if published, exists := s.owners[descriptor.Name]; exists && published.descriptor.ReplacesTool &&
				!descriptor.ReplacesTool {
				continue
			}
			s.owners[descriptor.Name] = &owner{
				extensionID: registration.ID,
				descriptor:  *descriptor,
				schema:      s.prepared[registration.ID][descriptor.Name],
				active:      true,
			}
		}
	}
	s.prepared = make(map[string]map[string]*jsonschema.Schema)
}
