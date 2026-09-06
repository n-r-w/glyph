package ui

import (
	"github.com/samber/lo"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// sessionListFrame normalizes stored previews and constructs independently owned UI list rows.
func sessionListFrame(listed []StoredSession) controllerui.Frame {
	return controllerui.Frame{
		NextInput: mo.None[string](),
		Kind:      controllerui.FrameSessionList,

		Lifecycle:        mo.None[controllerui.Lifecycle](),
		AuthorizationURL: mo.None[string](),

		ModelSelection: mo.None[model.Selection](),
		SessionInfo:    mo.None[session.Info](),
		Sessions: lo.Map(
			listed,
			func(item StoredSession, _ int) controllerui.SessionListItem { return item.publicItem() },
		),
		SessionEntries:         nil,
		SessionStatistics:      mo.None[session.Statistics](),
		SessionTree:            mo.None[controllerui.SessionTree](),
		TreeNavigationProgress: mo.None[controllerui.TreeNavigationProgress](),
		TreeNavigation:         mo.None[controllerui.TreeNavigationResult](),
	}
}

// sessionChangedFrame confirms replacement and carries the complete restored transcript.
func sessionChangedFrame(info session.Info, entries []session.Entry) (controllerui.Frame, error) {
	mapped, err := mapSessionEntries(entries)
	if err != nil {
		return controllerui.Frame{}, err
	}
	frame := sessionInfoFrame(controllerui.FrameSessionChanged, info, mo.None[session.Statistics]())
	frame.SessionEntries = mapped
	return frame, nil
}

// sessionInformationFrame composes current metadata and statistics without replacing the transcript.
func sessionInformationFrame(info session.Info, statistics session.Statistics) controllerui.Frame {
	return sessionInfoFrame(controllerui.FrameSessionInformation, info, mo.Some(statistics))
}

// sessionInfoFrame builds the selected session information frame kind.
func sessionInfoFrame(
	kind controllerui.FrameKind,
	info session.Info,
	statistics mo.Option[session.Statistics],
) controllerui.Frame {
	return controllerui.Frame{
		NextInput: mo.None[string](),
		Kind:      kind,

		Lifecycle:        mo.None[controllerui.Lifecycle](),
		AuthorizationURL: mo.None[string](),

		ModelSelection:         mo.None[model.Selection](),
		SessionInfo:            mo.Some(info),
		Sessions:               nil,
		SessionEntries:         nil,
		SessionStatistics:      statistics,
		SessionTree:            mo.None[controllerui.SessionTree](),
		TreeNavigationProgress: mo.None[controllerui.TreeNavigationProgress](),
		TreeNavigation:         mo.None[controllerui.TreeNavigationResult](),
	}
}

// mapSessionEntries projects the public conversation for a prepared session query.
func mapSessionEntries(entries []session.Entry) ([]controllerui.SessionEntry, error) {
	result := make([]controllerui.SessionEntry, 0, len(entries))
	for position := range entries {
		projected, present, err := controllerui.ProjectSessionEntry(entries[position], position)
		if err != nil {
			return nil, err
		}
		if present {
			result = append(result, projected)
		}
	}
	return result, nil
}
