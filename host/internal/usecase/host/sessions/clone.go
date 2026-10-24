package sessions

import (
	"fmt"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// Info derives name and update time from durable aggregate state.
func (loaded LoadedSession) Info() session.Info {
	name := mo.None[string]()
	if information, present := loaded.Information.Get(); present {
		name = mo.Some(information.Name)
	}
	updatedAt := loaded.Header.CreatedAt
	entries := loaded.Tree.Entries()
	for index := range entries {
		if entries[index].CreatedAt.After(updatedAt) {
			updatedAt = entries[index].CreatedAt
		}
	}
	informationUpdatedAt, hasInformationUpdate := loaded.InformationUpdatedAt.Get()
	if hasInformationUpdate && informationUpdatedAt.After(updatedAt) {
		updatedAt = informationUpdatedAt
	}
	storagePath := mo.None[string]()
	if loaded.StoragePath != "" {
		storagePath = mo.Some(loaded.StoragePath)
	}
	return session.Info{
		ID: loaded.Header.ID, Name: name, WorkingDirectory: loaded.Header.WorkingDirectory,
		StoragePath: storagePath, CreatedAt: loaded.Header.CreatedAt, UpdatedAt: updatedAt,
	}
}

// Clone returns an independently owned session snapshot or a tree validation error with no usable snapshot.
func (loaded LoadedSession) Clone() (LoadedSession, error) {
	tree, err := loaded.Tree.Clone()
	if err != nil {
		return LoadedSession{}, fmt.Errorf("clone loaded session: %w", err)
	}
	return LoadedSession{
		Header: loaded.Header, StoragePath: loaded.StoragePath, Tree: tree,
		Information: loaded.Information, InformationUpdatedAt: loaded.InformationUpdatedAt,
	}, nil
}

// cloneEntries returns independent mutable payload ownership for every entry.
func cloneEntries(entries []session.Entry) []session.Entry {
	return lo.Map(entries, func(entry session.Entry, _ int) session.Entry {
		return entry.Clone()
	})
}
