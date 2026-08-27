package ui

import (
	"strings"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
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
			})
			continue
		}
		if response, present := entry.Model.Get(); present {
			frame.SessionEntries = append(frame.SessionEntries, domainui.SessionEntry{
				ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: domainui.SessionEntryModel,
				UserText: mo.None[string](), Model: mo.Some(mapRestoredTextResponse(response)),
			})
		}
	}
	return frame
}

func mapRestoredTextResponse(response model.Response) domainui.ModelResponse {
	var text strings.Builder
	content := make([]domainui.ModelResponseContent, 0, len(response.Content))
	for index := range response.Content {
		item := &response.Content[index]
		value, present := item.Text.Get()
		if item.Kind != model.ContentText || !present {
			continue
		}
		text.WriteString(value)
		content = append(content, domainui.ModelResponseContent{Kind: domainui.ModelContentKindText, Text: value})
	}
	return domainui.ModelResponse{
		Text: text.String(), Outcome: mo.None[string](), ErrorMessage: mo.None[string](), Provider: mo.None[string](),
		Model: mo.None[string](), ResponseModel: mo.None[string](), ResponseID: mo.None[string](), Content: content,
		Usage: mo.None[domainui.ModelUsage](), Diagnostics: nil,
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
