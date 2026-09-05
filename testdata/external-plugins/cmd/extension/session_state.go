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
	// Appended reports whether this invocation created the recovered entry.
	Appended bool `json:"appended"`
}

// exerciseSessionState appends once, then recovers only through public context operations.
func exerciseSessionState(ctx context.Context) (*extensionv1.ToolResult, error) {
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
	appended := false
	if len(state.GetEntries()) == 0 {
		appendOperation, startErr := binding.StartAppendExtension(ctx, extensionv1.AppendExtensionRequest_builder{
			Context: nil, EntryType: new(sessionStateEntryType), Data: payload,
		}.Build())
		if startErr != nil {
			return nil, startErr
		}
		if _, waitErr := appendOperation.Wait(ctx); waitErr != nil {
			return nil, waitErr
		}
		appended = true
		read, err = binding.StartGetSessionState(ctx)
		if err != nil {
			return nil, err
		}
		state, err = read.Wait(ctx)
		if err != nil {
			return nil, err
		}
	}
	if len(state.GetEntries()) != 1 {
		return nil, errors.New("public recovery returned an unexpected entry count")
	}
	entry := state.GetEntries()[0]
	if entry.GetCreatedTime() == nil {
		return nil, errors.New("public recovery returned no entry timestamp")
	}
	report, err := json.Marshal(sessionStateReport{
		EntryID: entry.GetId(), ParentID: entry.GetParentId(),
		ExtensionID: entry.GetExtensionId(), EntryType: entry.GetEntryType(),
		CreatedTime:  entry.GetCreatedTime().AsTime().Format(time.RFC3339Nano),
		PayloadExact: bytes.Equal(payload, entry.GetData()), EntryCount: len(state.GetEntries()),
		Appended: appended,
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
