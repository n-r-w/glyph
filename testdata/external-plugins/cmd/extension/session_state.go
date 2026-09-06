package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"strings"
	"time"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// sessionStateEntryType identifies the fixture's persistent checkpoint.
	sessionStateEntryType = "restart-checkpoint"
	// sessionMessageEntryType identifies the fixture's persistent model-visible message.
	sessionMessageEntryType = "restart-message"
	// sessionMessageText is the exact text compared after restart.
	sessionMessageText = "exact\nrestart message"
	// recoveredEntryCount is the checkpoint plus its model-visible message.
	recoveredEntryCount = 2
	// recoveryPaddingSize makes the public recovery result exceed gRPC's default receive limit.
	recoveryPaddingSize = 4*1024*1024 + 1024
)

// sessionStateReport contains only public recovery evidence returned to the test provider.
type sessionStateReport struct {
	// EntryID identifies the durable Host-owned entry.
	EntryID string `json:"entry_id"`
	// ParentID preserves the stored parent without requiring it in filtered results.
	ParentID string `json:"parent_id"`
	// ExtensionID identifies the stored extension owner.
	ExtensionID string `json:"extension_id"`
	// EntryType identifies the stored extension-defined kind.
	EntryType string `json:"entry_type"`
	// CreatedTime contains the stored timestamp at nanosecond precision.
	CreatedTime string `json:"created_time"`
	// PayloadExact reports byte equality with the extension-owned JSON input.
	PayloadExact bool `json:"payload_exact"`
	// EntryCount reports the caller-filtered active-branch entry count.
	EntryCount int `json:"entry_count"`
	// Appended reports whether this invocation created the recovered entries.
	Appended bool `json:"appended"`
	// MessageID identifies the durable model-visible message.
	MessageID string `json:"message_id"`
	// MessageParentID preserves the stored message parent.
	MessageParentID string `json:"message_parent_id"`
	// MessageExtensionID identifies the stored message owner.
	MessageExtensionID string `json:"message_extension_id"`
	// MessageEntryType identifies the extension-defined message kind.
	MessageEntryType string `json:"message_entry_type"`
	// MessageCreatedTime contains the stored message timestamp.
	MessageCreatedTime string `json:"message_created_time"`
	// MessageText contains exact recovered text.
	MessageText string `json:"message_text"`
	// MessageVisibility contains the closed public visibility name.
	MessageVisibility string `json:"message_visibility"`
}

// ensureSessionState appends both entry types when the active branch has no extension state.
func ensureSessionState(
	ctx context.Context,
	binding *extensionsdk.ExtensionContext,
	state *extensionv1.GetSessionStateResult,
	payload []byte,
) (bool, *extensionv1.GetSessionStateResult, error) {
	if len(state.GetEntries()) != 0 {
		return false, state, nil
	}
	appendOperation, err := binding.StartAppendExtension(ctx, extensionv1.AppendExtensionRequest_builder{
		Context: nil, EntryType: new(sessionStateEntryType), Data: payload,
	}.Build())
	if err != nil {
		return false, nil, err
	}
	if _, err = appendOperation.Wait(ctx); err != nil {
		return false, nil, err
	}
	messageOperation, err := binding.StartAppendExtensionMessage(
		ctx,
		extensionv1.AppendExtensionMessageRequest_builder{
			Context: nil, EntryType: new(sessionMessageEntryType), Text: new(sessionMessageText),
			Visibility: new(extensionv1.ClientVisibility_CLIENT_VISIBILITY_HIDDEN),
		}.Build(),
	)
	if err != nil {
		return false, nil, err
	}
	messageResult, err := messageOperation.Wait(ctx)
	if err != nil {
		return false, nil, err
	}
	if len(messageResult.GetIssues()) != 0 {
		return false, nil, errors.New("model-visible append returned an unexpected delivery issue")
	}
	read, err := binding.StartGetSessionState(ctx)
	if err != nil {
		return false, nil, err
	}
	state, err = read.Wait(ctx)
	if err != nil {
		return false, nil, err
	}
	return true, state, nil
}

// exerciseSessionState appends once, then recovers only through public context operations.
func exerciseSessionState(ctx context.Context, service *service) (*extensionv1.ToolResult, error) {
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return nil, err
	}
	payload := []byte(`{ "escaped": "\u0061", "padding": "` + strings.Repeat("a", recoveryPaddingSize) + `" }`)
	read, err := binding.StartGetSessionState(ctx)
	if err != nil {
		return nil, err
	}
	state, err := read.Wait(ctx)
	if err != nil {
		return nil, err
	}
	appended, state, err := ensureSessionState(ctx, binding, state, payload)
	if err != nil {
		return nil, err
	}
	if len(state.GetEntries()) != recoveredEntryCount {
		return nil, errors.New("public recovery returned an unexpected entry count")
	}
	entry := state.GetEntries()[0]
	messageEntry := state.GetEntries()[1]
	if entry.GetCreatedTime() == nil || messageEntry.GetCreatedTime() == nil || messageEntry.GetMessage() == nil {
		return nil, errors.New("public recovery returned incomplete extension entries")
	}
	service.contextMutex.Lock()
	service.savedCheckpointID = entry.GetId()
	service.savedMessageID = messageEntry.GetId()
	service.contextMutex.Unlock()
	if messageEntry.GetMessage().GetText() != sessionMessageText {
		return nil, errors.New("public recovery returned unexpected extension message text")
	}
	if messageEntry.GetMessage().GetVisibility() != extensionv1.ClientVisibility_CLIENT_VISIBILITY_HIDDEN {
		return nil, errors.New("public recovery returned unexpected extension message visibility")
	}
	report, err := json.Marshal(sessionStateReport{
		EntryID:            entry.GetId(),
		ParentID:           entry.GetParentId(),
		ExtensionID:        entry.GetExtensionId(),
		EntryType:          entry.GetEntryType(),
		CreatedTime:        entry.GetCreatedTime().AsTime().Format(time.RFC3339Nano),
		PayloadExact:       bytes.Equal(payload, entry.GetData()),
		EntryCount:         len(state.GetEntries()),
		Appended:           appended,
		MessageID:          messageEntry.GetId(),
		MessageParentID:    messageEntry.GetParentId(),
		MessageExtensionID: messageEntry.GetExtensionId(),
		MessageEntryType:   messageEntry.GetEntryType(),
		MessageCreatedTime: messageEntry.GetCreatedTime().AsTime().Format(time.RFC3339Nano),
		MessageText: messageEntry.GetMessage().
			GetText(),
		MessageVisibility: messageEntry.GetMessage().GetVisibility().String(),
	})
	if err != nil {
		return nil, err
	}
	return extensionv1.ToolResult_builder{
		Contents: []*extensionv1.ToolResultContent{
			//nolint:exhaustruct_v5 // The public builder sets only the active text field.
			extensionv1.ToolResultContent_builder{Text: new(string(report))}.Build(),
		},
		IsError: new(false),
	}.Build(), nil
}
