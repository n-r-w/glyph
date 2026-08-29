package sessions

import (
	"bytes"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// replacementFromLoaded returns independent public state for an active-session replacement.
func replacementFromLoaded(loaded LoadedSession) session.Replacement {
	return session.Replacement{
		Info: infoFromLoaded(loaded), Entries: cloneEntries(loaded.Tree.ActiveBranch()),
	}
}

// infoFromLoaded derives name and update time from durable aggregate state.
func infoFromLoaded(loaded LoadedSession) session.Info {
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

// cloneLoaded prevents repository-owned aggregate state from becoming mutable active state.
func cloneLoaded(value LoadedSession) LoadedSession {
	tree, err := session.NewTree(value.Tree.Entries(), value.Tree.ActiveLeafID(), value.Tree.Labels())
	if err != nil {
		panic(err)
	}
	return LoadedSession{
		Header: value.Header, StoragePath: value.StoragePath, Tree: tree,
		Information: value.Information, InformationUpdatedAt: value.InformationUpdatedAt,
	}
}

// cloneEntries returns independent mutable payload ownership for every entry.
func cloneEntries(entries []session.Entry) []session.Entry {
	cloned := make([]session.Entry, len(entries))
	for index := range entries {
		cloned[index] = cloneSessionEntry(entries[index])
	}
	return cloned
}

// cloneSessionEntry owns the mutable payloads carried by one domain entry.
func cloneSessionEntry(entry session.Entry) session.Entry {
	entry.User = entry.User.MapValue(cloneMessage)
	entry.Model = entry.Model.MapValue(cloneModelResponse)
	entry.ToolResult = entry.ToolResult.MapValue(cloneToolResult)
	entry.Extension = entry.Extension.MapValue(func(value session.ExtensionEnvelope) session.ExtensionEnvelope {
		value.Data = bytes.Clone(value.Data)
		return value
	})
	return entry
}
