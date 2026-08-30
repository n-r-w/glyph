package sessions

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// Replacement returns independent public state for an active-session replacement.
func (loaded LoadedSession) Replacement() session.Replacement {
	return session.Replacement{
		Info: loaded.Info(), Entries: cloneEntries(loaded.Tree.ActiveBranch()),
	}
}

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

// Clone prevents repository-owned aggregate state from becoming mutable active state.
func (loaded LoadedSession) Clone() LoadedSession {
	return LoadedSession{
		Header: loaded.Header, StoragePath: loaded.StoragePath, Tree: loaded.Tree.Clone(),
		Information: loaded.Information, InformationUpdatedAt: loaded.InformationUpdatedAt,
	}
}

// cloneEntries returns independent mutable payload ownership for every entry.
func cloneEntries(entries []session.Entry) []session.Entry {
	cloned := make([]session.Entry, len(entries))
	for index := range entries {
		cloned[index] = entries[index].Clone()
	}
	return cloned
}
