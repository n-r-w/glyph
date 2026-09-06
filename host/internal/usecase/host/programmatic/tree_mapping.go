package programmatic

import (
	"fmt"

	"github.com/samber/lo"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// mapSessionTree projects every tree entry while removing private extension payload bytes.
func mapSessionTree(tree session.Tree) (controller.SessionTree, error) {
	entries := tree.Entries()
	labels := tree.Labels()
	mapped, err := lo.MapErr(entries, func(entry session.Entry, index int) (controller.SessionTreeEntry, error) {
		result, mapErr := controller.ProjectSessionTreeEntry(entry, labels[entry.ID])
		if mapErr != nil {
			return controller.SessionTreeEntry{}, fmt.Errorf("map session tree entry %d: %w", index, mapErr)
		}
		return result, nil
	})
	if err != nil {
		return controller.SessionTree{}, err
	}
	return controller.SessionTree{Entries: mapped, ActiveLeafID: tree.ActiveLeafID()}, nil
}

// mapTreeNavigationProgress projects committed navigation state for Programmatic progress.
func mapTreeNavigationProgress(progress session.Tree) (controller.TreeNavigationProgress, error) {
	tree, err := mapSessionTree(progress)
	if err != nil {
		return controller.TreeNavigationProgress{}, err
	}
	activeBranch, err := mapSessionEntries(progress.ActiveBranch())
	if err != nil {
		return controller.TreeNavigationProgress{}, fmt.Errorf("map active branch: %w", err)
	}
	return controller.TreeNavigationProgress{Tree: tree, ActiveBranch: activeBranch}, nil
}
