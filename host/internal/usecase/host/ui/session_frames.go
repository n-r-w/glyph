package ui

import (
	"strings"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// sessionListFrame copies the ordered list so later service changes cannot mutate an in-flight frame.
func sessionListFrame(listed []session.Summary) domainui.Frame {
	return domainui.Frame{
		Kind:                domainui.FrameSessionList,
		Initialization:      mo.None[domainui.Initialization](),
		Lifecycle:           mo.None[domainui.Lifecycle](),
		AuthorizationURL:    mo.None[string](),
		Text:                mo.None[string](),
		RetryAuthentication: mo.None[bool](),
		ModelSelection:      mo.None[domainui.ModelSelection](),
		SessionInfo:         mo.None[session.Info](),
		Sessions:            append([]session.Summary(nil), listed...),
		SessionEntries:      nil,
	}
}

// sessionChangedFrame confirms replacement and carries the complete restored transcript.
func sessionChangedFrame(info session.Info, entries []session.Entry) domainui.Frame {
	frame := sessionInfoFrame(domainui.FrameSessionChanged, info)
	frame.SessionEntries = make([]domainui.SessionEntry, 0, len(entries))
	for position := range entries {
		entry := &entries[position]
		if user, present := entry.User.Get(); present {
			var text strings.Builder
			for _, content := range user.Content {
				if value, ok := content.Text.Get(); content.Kind == model.InputContentText && ok {
					text.WriteString(value)
				}
			}
			frame.SessionEntries = append(frame.SessionEntries, domainui.SessionEntry{
				ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: domainui.SessionEntryUser,
				UserText: mo.Some(text.String()), Model: mo.None[domainui.ModelResponse](),
				ToolResult: mo.None[agent.ToolResult](),
			})
			continue
		}
		if response, present := entry.Model.Get(); present {
			mapped, ok := mapRestoredResponse(response)
			if !ok {
				continue
			}
			frame.SessionEntries = append(frame.SessionEntries, domainui.SessionEntry{
				ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: domainui.SessionEntryModel,
				UserText: mo.None[string](), Model: mo.Some(mapped), ToolResult: mo.None[agent.ToolResult](),
			})
			continue
		}
		if result, present := entry.ToolResult.Get(); present {
			frame.SessionEntries = append(frame.SessionEntries, domainui.SessionEntry{
				ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: domainui.SessionEntryToolResult,
				UserText: mo.None[string](), Model: mo.None[domainui.ModelResponse](),
				ToolResult: mo.Some(cloneRestoredToolResult(result)),
			})
		}
	}
	return frame
}

// mapRestoredResponse removes opaque context carriers before UI projection.
func mapRestoredResponse(response model.Response) (domainui.ModelResponse, bool) {
	mapped, err := mapModelResponseProjection(response, true)
	return mapped, err == nil
}

// cloneRestoredToolResult gives an in-flight UI frame independent result ownership.
func cloneRestoredToolResult(result agent.ToolResult) agent.ToolResult {
	contents := append([]tool.ResultContent(nil), result.Contents...)
	for index := range contents {
		if image, present := contents[index].Image.Get(); present {
			image.Data = append([]byte(nil), image.Data...)
			contents[index].Image = mo.Some(image)
		}
	}
	return agent.ToolResult{
		CallID: result.CallID, ToolName: result.ToolName, Contents: contents, IsError: result.IsError,
	}
}

// sessionInformationFrame reports active identity without replacing TUI transcript state.
func sessionInformationFrame(info session.Info) domainui.Frame {
	return sessionInfoFrame(domainui.FrameSessionInformation, info)
}

// sessionInfoFrame initializes exactly one information-bearing lifecycle variant.
func sessionInfoFrame(kind domainui.FrameKind, info session.Info) domainui.Frame {
	return domainui.Frame{
		Kind:                kind,
		Initialization:      mo.None[domainui.Initialization](),
		Lifecycle:           mo.None[domainui.Lifecycle](),
		AuthorizationURL:    mo.None[string](),
		Text:                mo.None[string](),
		RetryAuthentication: mo.None[bool](),
		ModelSelection:      mo.None[domainui.ModelSelection](),
		SessionInfo:         mo.Some(info),
		Sessions:            nil,
		SessionEntries:      nil,
	}
}
