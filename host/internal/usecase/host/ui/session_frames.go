package ui

import (
	"bytes"
	"fmt"
	"slices"

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
func sessionChangedFrame(info session.Info, entries []session.Entry) (domainui.Frame, error) {
	frame := sessionInfoFrame(domainui.FrameSessionChanged, info)
	frame.SessionEntries = make([]domainui.SessionEntry, 0, len(entries))
	for position := range entries {
		entry := &entries[position]
		if user, present := entry.User.Get(); present {
			frame.SessionEntries = append(frame.SessionEntries, domainui.SessionEntry{
				ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: domainui.SessionEntryUser,
				User:  mo.Some(cloneRestoredUser(user)),
				Model: mo.None[domainui.ModelResponse](), ToolResult: mo.None[agent.ToolResult](),
			})
			continue
		}
		if response, present := entry.Model.Get(); present {
			mapped, err := mapModelResponseProjection(response, false)
			if err != nil {
				return domainui.Frame{}, fmt.Errorf("map restored session entry %d: %w", position, err)
			}
			frame.SessionEntries = append(frame.SessionEntries, domainui.SessionEntry{
				ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: domainui.SessionEntryModel,
				User:  mo.None[model.Message](),
				Model: mo.Some(mapped), ToolResult: mo.None[agent.ToolResult](),
			})
			continue
		}
		if result, present := entry.ToolResult.Get(); present {
			frame.SessionEntries = append(frame.SessionEntries, domainui.SessionEntry{
				ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: domainui.SessionEntryToolResult,
				User: mo.None[model.Message](), Model: mo.None[domainui.ModelResponse](),
				ToolResult: mo.Some(cloneRestoredToolResult(result)),
			})
		}
	}
	return frame, nil
}

// cloneRestoredUser copies ordered user content and image bytes for frame ownership.
func cloneRestoredUser(message model.Message) model.Message {
	message.Content = slices.Clone(message.Content)
	for index := range message.Content {
		message.Content[index].Data = message.Content[index].Data.MapValue(bytes.Clone)
	}
	return message
}

// cloneRestoredToolResult copies ordered tool-result content and image bytes for frame ownership.
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

// sessionInfoFrame builds the selected session information frame kind.
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
