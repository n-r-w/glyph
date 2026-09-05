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
	extension, present := entry.Extension.Get()
	if !present {
		return nil, errors.New("recovered entry is not extension-owned")
	}
	createdAt := timestamppb.New(entry.CreatedAt)
	if err := createdAt.CheckValid(); err != nil {
		return nil, fmt.Errorf("map extension entry timestamp: %w", err)
	}
	var parentID *string
	if value, parentPresent := entry.ParentID.Get(); parentPresent {
		parentID = new(value)
	}
	mapped := extensionpb.SessionStateEntry_builder{
		Id: new(entry.ID), ParentId: parentID, CreatedTime: createdAt,
		ExtensionId: new(extension.ExtensionID), EntryType: new(extension.EntryType),
		Data: bytes.Clone(extension.Data),
	}.Build()
	return mapped, nil
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
