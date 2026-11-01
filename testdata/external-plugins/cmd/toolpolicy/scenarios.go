package main

import (
	"context"
	"encoding/json/v2"
	"errors"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// Run records the received calls and applies the policy that the executable name selects.
func (o *operation) Run(context.Context) (*extensionpb.HandleResponse, error) {
	invocation := o.request.GetToolPolicy()
	original, current := invocation.GetOriginal().GetArgumentsJson(), invocation.GetCurrent().GetArgumentsJson()
	if err := o.service.trace(o.service.mode, original, current); err != nil {
		return nil, err
	}
	switch o.service.behavior {
	case allowBehavior:
		return allow(), nil
	case replaceBehavior:
		return replaceArguments(replacementArguments), nil
	case invalidBehavior:
		return replaceArguments(invalidArguments), nil
	case rejectBehavior:
		value, err := callValue(current)
		if err != nil {
			return nil, err
		}
		if value == forbiddenValue {
			return reject(rejectReason), nil
		}
		return allow(), nil
	case failBehavior:
		return nil, errors.New(failureText)
	case emptyBehavior:
		return emptyAction(), nil
	default:
		return nil, errors.New("unknown tool-policy behavior " + o.service.behavior)
	}
}

// callValue returns the value property of the JSON arguments of a call; a value of another type is empty.
func callValue(arguments []byte) (string, error) {
	var input struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil {
		return "", err
	}
	return input.Value, nil
}

// allow keeps the current call.
//
//nolint:exhaustruct_v5 // Protobuf oneof builders set only their active action.
func allow() *extensionpb.HandleResponse {
	return extensionpb.HandleResponse_builder{
		ToolPolicy: extensionpb.ToolPolicyAction_builder{Allow: new(true)}.Build(),
	}.Build()
}

// replaceArguments replaces the input of the current call.
//
//nolint:exhaustruct_v5 // Protobuf oneof builders set only their active action.
func replaceArguments(arguments string) *extensionpb.HandleResponse {
	return extensionpb.HandleResponse_builder{
		ToolPolicy: extensionpb.ToolPolicyAction_builder{ReplaceArgumentsJson: []byte(arguments)}.Build(),
	}.Build()
}

// reject rejects the call with a reason.
//
//nolint:exhaustruct_v5 // Protobuf oneof builders set only their active action.
func reject(reason string) *extensionpb.HandleResponse {
	return extensionpb.HandleResponse_builder{
		ToolPolicy: extensionpb.ToolPolicyAction_builder{RejectReason: new(reason)}.Build(),
	}.Build()
}

// emptyAction returns an action that selects nothing, which Host rejects as invalid.
//
//nolint:exhaustruct_v5 // Protobuf oneof builders set only their active action.
func emptyAction() *extensionpb.HandleResponse {
	return extensionpb.HandleResponse_builder{ToolPolicy: extensionpb.ToolPolicyAction_builder{}.Build()}.Build()
}
