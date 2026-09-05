package ui

import (
	"fmt"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// sessionListFrame copies the ordered list so later service changes cannot mutate an in-flight frame.
func sessionListFrame(listed []session.Summary) domainui.Frame {
	return domainui.Frame{
		Kind:              domainui.FrameSessionList,
		Initialization:    mo.None[domainui.Initialization](),
		Lifecycle:         mo.None[domainui.Lifecycle](),
		AuthorizationURL:  mo.None[string](),
		ErrorCode:         mo.None[string](),
		Text:              mo.None[string](),
		ModelSelection:    mo.None[domainui.ModelSelection](),
		SessionInfo:       mo.None[session.Info](),
		Sessions:          append([]session.Summary(nil), listed...),
		SessionEntries:    nil,
		SessionStatistics: mo.None[session.Statistics](),
		SessionTree:       mo.None[domainui.SessionTree](),
		TreeNavigation:    mo.None[domainui.TreeNavigationResult](),
	}
}

// sessionChangedFrame confirms replacement and carries the complete restored transcript.
func sessionChangedFrame(info session.Info, entries []session.Entry) (domainui.Frame, error) {
	mapped, err := mapSessionEntries(entries)
	if err != nil {
		return domainui.Frame{}, err
	}
	frame := sessionInfoFrame(domainui.FrameSessionChanged, info, mo.None[session.Statistics]())
	frame.SessionEntries = mapped
	return frame, nil
}

// mapSessionEntries projects public transcript entries and skips model-hidden tree entries.
func mapSessionEntries(entries []session.Entry) ([]domainui.SessionEntry, error) {
	mappedEntries := make([]domainui.SessionEntry, 0, len(entries))
	for position := range entries {
		entry := &entries[position]
		if user, present := entry.User.Get(); present {
			mappedEntries = append(mappedEntries, domainui.SessionEntry{
				ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: domainui.SessionEntryUser,
				User: mo.Some(user.Clone()), Model: mo.None[domainui.ModelResponse](),
				ToolResult: mo.None[agent.ToolResult](), BranchSummary: mo.None[domainui.BranchSummary](),
			})
			continue
		}
		if response, present := entry.Model.Get(); present {
			mapped, err := mapModelResponseProjection(response, false)
			if err != nil {
				return nil, fmt.Errorf("map restored session entry %d: %w", position, err)
			}
			mappedEntries = append(mappedEntries, domainui.SessionEntry{
				ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: domainui.SessionEntryModel,
				User: mo.None[model.Message](), Model: mo.Some(mapped), ToolResult: mo.None[agent.ToolResult](),
				BranchSummary: mo.None[domainui.BranchSummary](),
			})
			continue
		}
		if result, present := entry.ToolResult.Get(); present {
			mappedEntries = append(mappedEntries, domainui.SessionEntry{
				ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: domainui.SessionEntryToolResult,
				User: mo.None[model.Message](), Model: mo.None[domainui.ModelResponse](),
				ToolResult: mo.Some(result.Clone()), BranchSummary: mo.None[domainui.BranchSummary](),
			})
			continue
		}
		if summary, present := entry.BranchSummary.Get(); present {
			mappedEntries = append(mappedEntries, domainui.SessionEntry{
				ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: domainui.SessionEntryBranchSummary,
				User: mo.None[model.Message](), Model: mo.None[domainui.ModelResponse](),
				ToolResult: mo.None[agent.ToolResult](),
				BranchSummary: mo.Some(domainui.BranchSummary{
					Summary: summary.Summary, FirstEntryID: summary.FirstEntryID, LastEntryID: summary.LastEntryID,
					Source: summary.Source, EstimatedCost: summary.EstimatedCost,
				}),
			})
		}
	}
	return mappedEntries, nil
}

// sessionInformationFrame composes current metadata and statistics without replacing the transcript.
func sessionInformationFrame(info session.Info, statistics session.Statistics) domainui.Frame {
	return sessionInfoFrame(domainui.FrameSessionInformation, info, mo.Some(statistics))
}

// sessionInfoFrame builds the selected session information frame kind.
func sessionInfoFrame(
	kind domainui.FrameKind,
	info session.Info,
	statistics mo.Option[session.Statistics],
) domainui.Frame {
	return domainui.Frame{
		Kind:              kind,
		Initialization:    mo.None[domainui.Initialization](),
		Lifecycle:         mo.None[domainui.Lifecycle](),
		AuthorizationURL:  mo.None[string](),
		ErrorCode:         mo.None[string](),
		Text:              mo.None[string](),
		ModelSelection:    mo.None[domainui.ModelSelection](),
		SessionInfo:       mo.Some(info),
		Sessions:          nil,
		SessionEntries:    nil,
		SessionStatistics: statistics,
		SessionTree:       mo.None[domainui.SessionTree](),
		TreeNavigation:    mo.None[domainui.TreeNavigationResult](),
	}
}
