package main

import (
	"encoding/json/v2"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	// invalidReplayBody selects an unknown invocation-local reference for the failure scenario.
	invalidReplayBody = `{"input":[{"$glyph_replay":"unknown"}]}`
)

// transformHeaders demonstrates original/current replacement without accessing private authentication.
//
//nolint:exhaustruct_v5 // Protobuf oneof builders set only their active replacement payload.
func (o *operation) transformHeaders(input *extensionpb.ProviderHeadersInvocation) *extensionpb.HandleResponse {
	value := input.GetCurrent()
	marker := markerA
	if o.request.GetHandlerId() == headersB {
		value = input.GetOriginal()
		marker = markerB
	}
	entries := value.GetEntries()
	if entries == nil {
		entries = make(map[string]*extensionpb.WireHeaderValues)
	}
	entries[publicHeader] = extensionpb.WireHeaderValues_builder{Values: []string{marker}}.Build()
	return extensionpb.HandleResponse_builder{ProviderHeaders: extensionpb.ProviderHeadersAction_builder{
		Replace: extensionpb.WireHeaders_builder{Entries: entries}.Build(),
	}.Build()}.Build()
}

// transformBody preserves references by treating the safe JSON representation as ordinary public data.
//
//nolint:exhaustruct_v5 // Protobuf oneof builders set only their active preservation or replacement payload.
func (o *operation) transformBody(input *extensionpb.ProviderBodyInvocation) (*extensionpb.HandleResponse, error) {
	if o.mode == preserveMode {
		return extensionpb.HandleResponse_builder{
			ProviderBody: extensionpb.ProviderBodyAction_builder{Preserve: new(true)}.Build(),
		}.Build(), nil
	}
	value := input.GetCurrent()
	marker := markerA
	if o.request.GetHandlerId() == bodyB {
		value = input.GetOriginal()
		marker = markerB
	}
	var object map[string]any
	if err := json.Unmarshal(value, &object); err != nil {
		return nil, err
	}
	object[bodyMarker] = marker
	replacement, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	if o.mode == badReferenceMode {
		replacement = []byte(invalidReplayBody)
	}
	return extensionpb.HandleResponse_builder{
		ProviderBody: extensionpb.ProviderBodyAction_builder{Replace: replacement}.Build(),
	}.Build(), nil
}
