package sessions

import (
	"bytes"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// replacementFromLoaded returns independent public state for an active-session replacement.
func replacementFromLoaded(loaded LoadedSession) session.Replacement {
	return session.Replacement{Info: infoFromLoaded(loaded), Entries: cloneEntries(loaded.Entries)}
}

// infoFromLoaded derives name and update time from ordered records rather than filesystem metadata.
func infoFromLoaded(loaded LoadedSession) session.Info {
	name := mo.None[string]()
	updatedAt := loaded.Header.CreatedAt
	for index := range loaded.Entries {
		entry := &loaded.Entries[index]
		if information, ok := entry.Information.Get(); ok {
			name = mo.Some(information.Name)
		}
		updatedAt = entry.CreatedAt
	}
	storagePath := mo.None[string]()
	if loaded.StoragePath != "" {
		storagePath = mo.Some(loaded.StoragePath)
	}
	return session.Info{
		ID:               loaded.Header.ID,
		Name:             name,
		WorkingDirectory: loaded.Header.WorkingDirectory,
		StoragePath:      storagePath,
		CreatedAt:        loaded.Header.CreatedAt,
		UpdatedAt:        updatedAt,
	}
}

// cloneLoaded prevents repository-owned entries from becoming mutable active state.
func cloneLoaded(value LoadedSession) LoadedSession {
	return LoadedSession{
		Header: value.Header, StoragePath: value.StoragePath, Entries: cloneEntries(value.Entries),
	}
}

func cloneEntries(entries []session.Entry) []session.Entry {
	cloned := make([]session.Entry, len(entries))
	for index := range entries {
		entry := &entries[index]
		cloned[index] = session.Entry{
			ID: entry.ID, CreatedAt: entry.CreatedAt, Information: entry.Information,
			User: entry.User.MapValue(cloneMessage), Model: entry.Model.MapValue(cloneModelResponse),
			ToolResult: entry.ToolResult.MapValue(cloneToolResult),
			// Each active-session snapshot owns its opaque extension bytes.
			Extension: entry.Extension.MapValue(func(value session.ExtensionEnvelope) session.ExtensionEnvelope {
				value.Data = bytes.Clone(value.Data)
				return value
			}), EstimatedCost: entry.EstimatedCost,
		}
	}
	return cloned
}
