package extension

import (
	"errors"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// encodePolicyRequestResult projects a supplied replacement result.
func encodePolicyRequestResult(result session.CompactionEntry) *extensionpb.CompactionResult {
	wire := extensionpb.CompactionResult_builder{
		Summary:          new(result.Summary),
		FirstKeptEntryId: new(result.FirstKeptEntryID),
		Source:           mapCompactionSource(result.Source),
		Details:          nil,
	}.Build()
	if details, present := result.Details.Get(); present {
		wire.SetDetails(append([]byte(nil), details...))
	}
	return wire
}

// encodePolicyRequestEntry projects one public entry union.
func encodePolicyRequestEntry(entry session.Entry) (*extensionpb.SessionTreeEntry, error) {
	wire := new(extensionpb.SessionTreeEntry)
	wire.SetId(entry.ID)
	if result, present := entry.ToolResult.Get(); present {
		mapped, err := mapCompactionToolResult(result)
		if err != nil {
			return nil, err
		}
		wire.SetToolResult(mapped)
		return wire, nil
	}
	if response, present := entry.Model.Get(); present {
		mapped, err := mapCompactionModelResponse(response)
		if err != nil {
			return nil, err
		}
		wire.SetModel(mapped)
		return wire, nil
	}
	if user, present := entry.User.Get(); present {
		wire.SetUser(mapCompactionUser(user))
		return wire, nil
	}
	if summary, present := entry.BranchSummary.Get(); present {
		wire.SetBranchSummary(extensionpb.SessionTreeBranchSummary_builder{Summary: new(summary.Summary)}.Build())
		return wire, nil
	}
	if previous, present := entry.Compaction.Get(); present {
		wire.SetCompaction(MapCompactionMarker(previous))
		return wire, nil
	}
	if envelope, present := entry.Extension.Get(); present {
		wire.SetExtension(
			extensionpb.SessionTreeExtensionEntry_builder{
				ExtensionId: new(envelope.ExtensionID),
				EntryType:   new(envelope.EntryType),
			}.Build(),
		)
		return wire, nil
	}
	if message, present := entry.ExtensionMessage.Get(); present {
		visibility := extensionpb.ClientVisibility_CLIENT_VISIBILITY_VISIBLE
		if message.Visibility == session.ClientVisibilityHidden {
			visibility = extensionpb.ClientVisibility_CLIENT_VISIBILITY_HIDDEN
		}
		wire.SetExtensionMessage(
			extensionpb.SessionTreeExtensionMessage_builder{
				ExtensionId: new(message.ExtensionID),
				EntryType:   new(message.EntryType),
				Text:        new(message.Text),
				Visibility:  new(visibility),
			}.Build(),
		)
		return wire, nil
	}
	return nil, errors.New("request entry content is missing")
}
