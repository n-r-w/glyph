package ui

import (
	"fmt"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
)

// sessionTreeFrame projects the complete tree without private extension payload bytes.
func sessionTreeFrame(tree session.Tree) (controllerui.Frame, error) {
	mapped, err := mapSessionTree(tree)
	if err != nil {
		return controllerui.Frame{}, err
	}
	frame := controllerui.NewFrame(controllerui.FrameSessionTree)
	frame.SessionTree = mo.Some(mapped)
	return frame, nil
}

// navigationProgressCallback maps and enqueues committed navigation state.
func navigationProgressCallback(
	reporter operation.Reporter[controllerui.Frame],
) func(session.Tree) error {
	return func(progress session.Tree) error {
		tree, err := mapSessionTree(progress)
		if err != nil {
			return err
		}
		branch, err := mapSessionEntries(progress.ActiveBranch())
		if err != nil {
			return fmt.Errorf("map active branch: %w", err)
		}
		frame := controllerui.NewFrame(controllerui.FrameSessionTreeNavigationProgress)
		frame.TreeNavigationProgress = mo.Some(controllerui.TreeNavigationProgress{Tree: tree, ActiveBranch: branch})
		return reporter.Report(frame)
	}
}

// mapSessionTree projects every tree entry in persistence order.
func mapSessionTree(tree session.Tree) (controllerui.SessionTree, error) {
	entries := tree.Entries()
	labels := tree.Labels()
	mapped, err := lo.MapErr(entries, func(entry session.Entry, index int) (controllerui.SessionTreeEntry, error) {
		result, mapErr := controllerui.ProjectSessionTreeEntry(entry, labels[entry.ID])
		if mapErr != nil {
			return controllerui.SessionTreeEntry{}, fmt.Errorf("map session tree entry %d: %w", index, mapErr)
		}
		return result, nil
	})
	if err != nil {
		return controllerui.SessionTree{}, err
	}
	return controllerui.SessionTree{Entries: mapped, ActiveLeafID: tree.ActiveLeafID()}, nil
}
