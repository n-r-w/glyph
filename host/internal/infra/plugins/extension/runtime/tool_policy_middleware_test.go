//go:build !integration

package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/middleware"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// toolPolicyResponse wraps one tool-policy action in a handler response.
func toolPolicyResponse(action *extensionpb.ToolPolicyAction) *extensionpb.HandleResponse {
	response := new(extensionpb.HandleResponse)
	response.SetToolPolicy(action)
	return response
}

// TestToolPolicyMapping maps the original and current call and every decoded action kind.
func TestToolPolicyMapping(t *testing.T) {
	t.Parallel()
	// Arrange one invocation whose current call differs from the original in its arguments.
	original, originalErr := model.NewToolCallArguments([]byte(`{"path":"a"}`))
	current, currentErr := model.NewToolCallArguments([]byte(`{"path":"b"}`))
	require.NoError(t, originalErr)
	require.NoError(t, currentErr)
	input := middleware.Transformation[model.ToolCall]{
		Context:  middlewareBinding(),
		Original: model.ToolCall{ID: "call", Name: "read", Arguments: original},
		Current:  model.ToolCall{ID: "call", Name: "read", Arguments: current},
	}
	action := func(build func(*extensionpb.ToolPolicyAction)) *extensionpb.HandleResponse {
		value := new(extensionpb.ToolPolicyAction)
		build(value)
		return toolPolicyResponse(value)
	}

	// Act by mapping the invocation and every handler response.
	request := mapToolPolicyRequest("policy", input)
	allowed := mapToolPolicyAction(action(func(value *extensionpb.ToolPolicyAction) { value.SetAllow(true) }))
	replaced := mapToolPolicyAction(action(func(value *extensionpb.ToolPolicyAction) {
		value.SetReplaceArgumentsJson([]byte(`{"path":"c"}`))
	}))
	rejected := mapToolPolicyAction(action(func(value *extensionpb.ToolPolicyAction) { value.SetRejectReason("no") }))
	notAllowed := mapToolPolicyAction(action(func(value *extensionpb.ToolPolicyAction) { value.SetAllow(false) }))
	empty := mapToolPolicyAction(action(func(*extensionpb.ToolPolicyAction) {}))
	missing := mapToolPolicyAction(new(extensionpb.HandleResponse))

	// Assert the public payload and the actions left for owner validation.
	assert.Equal(t, "policy", request.GetHandlerId())
	assert.Equal(t, "binding", request.GetContext().GetContextId())
	assert.Equal(t, "call", request.GetToolPolicy().GetOriginal().GetId())
	assert.Equal(t, "read", request.GetToolPolicy().GetOriginal().GetName())
	assert.JSONEq(t, `{"path":"a"}`, string(request.GetToolPolicy().GetOriginal().GetArgumentsJson()))
	assert.JSONEq(t, `{"path":"b"}`, string(request.GetToolPolicy().GetCurrent().GetArgumentsJson()))
	assert.Equal(t, middleware.ToolPolicyAction{Kind: middleware.Preserve, Arguments: nil, Reason: ""}, allowed)
	assert.Equal(
		t, middleware.ToolPolicyAction{Kind: middleware.Replace, Arguments: []byte(`{"path":"c"}`), Reason: ""},
		replaced,
	)
	assert.Equal(t, middleware.ToolPolicyAction{Kind: middleware.Reject, Arguments: nil, Reason: "no"}, rejected)
	assert.Equal(t, middleware.ActionKind(0), notAllowed.Kind)
	assert.Equal(t, middleware.ActionKind(0), empty.Kind)
	assert.Equal(t, middleware.ActionKind(0), missing.Kind)
}
