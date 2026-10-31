//go:build !integration

package runtime

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/middleware"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// finalizedResponse wraps one finalized-message action in a handler response.
func finalizedResponse(action *extensionpb.FinalizedMessageAction) *extensionpb.HandleResponse {
	response := new(extensionpb.HandleResponse)
	response.SetFinalizedMessage(action)
	return response
}

// finalizedReplay returns private reasoning replay of one provider response.
func finalizedReplay() mo.Option[model.ProviderContext] {
	return mo.Some(model.ProviderContext{Source: model.ProviderContextSource{
		ProviderID: "provider", API: "responses", Model: "model", CompatibilityKey: mo.None[string](),
	}, Payload: []byte(privateReplay)})
}

// TestFinalizedMessageMapsEveryRole maps original/current values and preserve actions of each message role.
func TestFinalizedMessageMapsEveryRole(t *testing.T) {
	t.Parallel()
	history := contextHistory(t, mo.None[model.ProviderContext]())
	for name, entry := range map[string]agent.HistoryEntry{
		"user": history[0], "model": history[1], "tool result": history[2],
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange one invocation whose original and current hold the same role.
			input := middleware.Transformation[agent.HistoryEntry]{
				Context: middlewareBinding(), Original: entry, Current: entry,
			}
			projection := newReplayProjection()

			// Act by mapping the invocation, a replacement with the projected current value and a preserve action.
			request, err := mapFinalizedMessageRequest("finalized", input, projection)
			require.NoError(t, err)
			replaced, replaceErr := mapFinalizedMessageAction(finalizedResponse(
				extensionpb.FinalizedMessageAction_builder{
					Preserve: nil, Replace: request.GetFinalizedMessage().GetCurrent(),
				}.Build(),
			), projection)
			preserved, preserveErr := mapFinalizedMessageAction(finalizedResponse(
				extensionpb.FinalizedMessageAction_builder{Preserve: new(true), Replace: nil}.Build(),
			), projection)

			// Assert the public payload carries the role and the decoded actions are left for owner validation.
			require.Equal(t, "finalized", request.GetHandlerId())
			require.Equal(t, "binding", request.GetContext().GetContextId())
			require.Equal(
				t,
				request.GetFinalizedMessage().GetOriginal().WhichContent(),
				request.GetFinalizedMessage().GetCurrent().WhichContent(),
			)
			require.NoError(t, replaceErr)
			require.Equal(t, middleware.Replace, replaced.Kind)
			require.Equal(t, entry.Kind, replaced.Replacement.Kind)
			require.NoError(t, preserveErr)
			require.Equal(t, middleware.Preserve, preserved.Kind)
		})
	}
}

// TestFinalizedMessageReplayRoundTrip hides private replay and restores only references retained by a replacement.
func TestFinalizedMessageReplayRoundTrip(t *testing.T) {
	t.Parallel()
	// Arrange an original model response with private replay and a current value from which an earlier handler
	// removed it.
	original := contextHistory(t, finalizedReplay())[1]
	current := contextHistory(t, mo.None[model.ProviderContext]())[1]
	input := middleware.Transformation[agent.HistoryEntry]{
		Context: middlewareBinding(), Original: original, Current: current,
	}
	projection := newReplayProjection()

	// Act by projecting the invocation and replacing it with its original and its current value.
	request, err := mapFinalizedMessageRequest("finalized", input, projection)
	require.NoError(t, err)
	encoded, err := proto.Marshal(request)
	require.NoError(t, err)
	fromOriginal, originalErr := mapFinalizedMessageAction(finalizedResponse(
		extensionpb.FinalizedMessageAction_builder{
			Preserve: nil, Replace: request.GetFinalizedMessage().GetOriginal(),
		}.Build(),
	), projection)
	fromCurrent, currentErr := mapFinalizedMessageAction(finalizedResponse(
		extensionpb.FinalizedMessageAction_builder{
			Preserve: nil, Replace: request.GetFinalizedMessage().GetCurrent(),
		}.Build(),
	), projection)

	// Assert confidential projection, restoration from the original, and deliberate discard.
	reference := request.GetFinalizedMessage().GetOriginal().GetModel().GetContent()[1].GetReplayReference()
	require.NotContains(t, string(encoded), privateReplay)
	require.NotEmpty(t, reference)
	require.Empty(t, request.GetFinalizedMessage().GetCurrent().GetModel().GetContent()[1].GetReplayReference())
	require.NoError(t, originalErr)
	require.Equal(
		t,
		middleware.Transformed[agent.HistoryEntry]{Kind: middleware.Replace, Replacement: original},
		fromOriginal,
	)
	require.NoError(t, currentErr)
	require.Equal(
		t,
		middleware.Transformed[agent.HistoryEntry]{Kind: middleware.Replace, Replacement: current},
		fromCurrent,
	)
}

// TestFinalizedMessageRejectsInvalidReferences fails unknown, duplicated, and misplaced replay references.
func TestFinalizedMessageRejectsInvalidReferences(t *testing.T) {
	t.Parallel()
	for name, edit := range map[string]func(contents []*extensionpb.ReplayableModelContent, reference string){
		"unknown": func(contents []*extensionpb.ReplayableModelContent, _ string) {
			contents[1].SetReplayReference("unknown")
		},
		"duplicated": func(contents []*extensionpb.ReplayableModelContent, reference string) {
			duplicate := proto.CloneOf(contents[1])
			duplicate.SetReplayReference(reference)
			contents[2] = duplicate
		},
		"misplaced": func(contents []*extensionpb.ReplayableModelContent, reference string) {
			contents[0].SetReplayReference(reference)
			contents[1].SetReplayReference("")
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange a projected original whose replacement edits its replay reference.
			original := contextHistory(t, finalizedReplay())[1]
			projection := newReplayProjection()
			request, err := mapFinalizedMessageRequest("finalized", middleware.Transformation[agent.HistoryEntry]{
				Context: middlewareBinding(), Original: original, Current: original,
			}, projection)
			require.NoError(t, err)
			replacement := proto.CloneOf(request.GetFinalizedMessage().GetOriginal())
			contents := replacement.GetModel().GetContent()
			edit(contents, contents[1].GetReplayReference())

			// Act by decoding the handler replacement.
			_, err = mapFinalizedMessageAction(finalizedResponse(
				extensionpb.FinalizedMessageAction_builder{Preserve: nil, Replace: replacement}.Build(),
			), projection)

			// Assert the replacement fails as an invalid replay reference.
			require.ErrorContains(t, err, "replay reference")
		})
	}
}

// TestFinalizedMessageMissingAction leaves a missing or false action invalid for the owner.
func TestFinalizedMessageMissingAction(t *testing.T) {
	t.Parallel()
	// Arrange a false preserve action.
	response := finalizedResponse(
		extensionpb.FinalizedMessageAction_builder{Preserve: new(false), Replace: nil}.Build(),
	)

	// Act by decoding the action.
	action, err := mapFinalizedMessageAction(response, newReplayProjection())

	// Assert the zero action kind that the owner rejects.
	require.NoError(t, err)
	require.Equal(t, middleware.ActionKind(0), action.Kind)
}
