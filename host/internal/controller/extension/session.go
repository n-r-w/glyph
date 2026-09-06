package extension

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// appendRequest contains one validated model-hidden append payload.
type appendRequest struct {
	// entryType identifies the extension-owned state kind.
	entryType string
	// data contains an owned exact JSON value.
	data []byte
}

// appendMessageRequest contains one validated model-visible append payload.
type appendMessageRequest struct {
	// entryType identifies the extension-owned message kind.
	entryType string
	// text contains the exact model-visible text.
	text string
	// visibility controls ordinary client transcript presentation.
	visibility session.ClientVisibility
}

// mapAppendMessageRequest validates and owns one public model-visible message request.
func mapAppendMessageRequest(request *extensionpb.AppendExtensionMessageRequest) (appendMessageRequest, error) {
	if request == nil || request.GetEntryType() == "" || !request.HasText() {
		return appendMessageRequest{}, errors.New("extension message type and text are required")
	}
	visibility, err := mapClientVisibility(request.GetVisibility())
	if err != nil {
		return appendMessageRequest{}, err
	}
	return appendMessageRequest{entryType: request.GetEntryType(), text: request.GetText(), visibility: visibility}, nil
}

// mapClientVisibility maps the closed public visibility set to the session domain.
func mapClientVisibility(visibility extensionpb.ClientVisibility) (session.ClientVisibility, error) {
	switch visibility {
	case extensionpb.ClientVisibility_CLIENT_VISIBILITY_VISIBLE:
		return session.ClientVisibilityVisible, nil
	case extensionpb.ClientVisibility_CLIENT_VISIBILITY_HIDDEN:
		return session.ClientVisibilityHidden, nil
	case extensionpb.ClientVisibility_CLIENT_VISIBILITY_UNSPECIFIED:
		return "", errors.New("extension message visibility is required")
	default:
		return "", errors.New("extension message visibility is unknown")
	}
}

// mapAppendRequest validates and owns one public hidden-entry request.
func mapAppendRequest(request *extensionpb.AppendExtensionRequest) (appendRequest, error) {
	if request == nil || request.GetEntryType() == "" {
		return appendRequest{}, errors.New("extension entry type is required")
	}
	if !jsontext.Value(request.GetData()).IsValid() {
		return appendRequest{}, errors.New("extension entry data must be one valid JSON value")
	}
	return appendRequest{entryType: request.GetEntryType(), data: bytes.Clone(request.GetData())}, nil
}

// mapSessionEntry maps exact stored metadata and opaque bytes to the public recovery contract.
func mapSessionEntry(entry session.Entry) (*extensionpb.SessionStateEntry, error) {
	extension, hiddenPresent := entry.Extension.Get()
	message, messagePresent := entry.ExtensionMessage.Get()
	if hiddenPresent == messagePresent {
		return nil, errors.New("recovered entry must contain exactly one extension payload")
	}
	createdAt := timestamppb.New(entry.CreatedAt)
	if err := createdAt.CheckValid(); err != nil {
		return nil, fmt.Errorf("map extension entry timestamp: %w", err)
	}
	var parentID *string
	if value, parentPresent := entry.ParentID.Get(); parentPresent {
		parentID = new(value)
	}
	if hiddenPresent {
		return extensionpb.SessionStateEntry_builder{
			Id: new(entry.ID), ParentId: parentID, CreatedTime: createdAt,
			ExtensionId: new(extension.ExtensionID), EntryType: new(extension.EntryType),
			Data: bytes.Clone(extension.Data), Message: nil,
		}.Build(), nil
	}
	visibility := extensionpb.ClientVisibility_CLIENT_VISIBILITY_VISIBLE
	if message.Visibility == session.ClientVisibilityHidden {
		visibility = extensionpb.ClientVisibility_CLIENT_VISIBILITY_HIDDEN
	}
	return extensionpb.SessionStateEntry_builder{
		Id: new(entry.ID), ParentId: parentID, CreatedTime: createdAt,
		ExtensionId: new(message.ExtensionID), EntryType: new(message.EntryType), Data: nil,
		Message: extensionpb.ExtensionMessage_builder{Text: new(message.Text), Visibility: new(visibility)}.Build(),
	}.Build(), nil
}

// mapSessionState maps one coherent state-owner snapshot without changing order or ancestry.
func mapSessionState(snapshot SessionState) (*extensionpb.GetSessionStateResult, error) {
	entries := make([]*extensionpb.SessionStateEntry, 0, len(snapshot.Entries))
	for entryIndex := range snapshot.Entries {
		mapped, err := mapSessionEntry(snapshot.Entries[entryIndex])
		if err != nil {
			return nil, err
		}
		entries = append(entries, mapped)
	}
	result := extensionpb.GetSessionStateResult_builder{
		SessionId: new(string(snapshot.SessionID)), ActiveLeafId: nil, Entries: entries,
	}.Build()
	if activeLeafID, present := snapshot.ActiveLeafID.Get(); present {
		result.SetActiveLeafId(activeLeafID)
	}
	return result, nil
}
