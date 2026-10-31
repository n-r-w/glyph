package runtime

import (
	"errors"

	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// decodeCompactionRequestResponse supplies a request participant's typed action.
func (r *Runtime) decodeCompactionRequestResponse(
	response *extensionpb.HandleResponse,
) (extensioncontext.RequestAction, error) {
	action := response.GetCompactionRequest()
	if action == nil {
		return extensioncontext.RequestAction{}, r.protocolViolation(
			errors.New("compaction request handler returned another action kind"),
		)
	}
	return mapCompactionRequestAction(action)
}

// decodeCompactionGeneratorResponse supplies a generator's complete result.
func (r *Runtime) decodeCompactionGeneratorResponse(
	response *extensionpb.HandleResponse,
) (extensioncontext.Result, error) {
	result := response.GetCompactionGenerate()
	if result == nil {
		return extensioncontext.Result{}, r.protocolViolation(
			errors.New("compaction generator returned another action kind"),
		)
	}
	return mapCompactionResultFromProto(result), nil
}

// decodeCompactionResultResponse supplies a result participant's typed action.
func (r *Runtime) decodeCompactionResultResponse(
	response *extensionpb.HandleResponse,
) (extensioncontext.ResultAction, error) {
	action := response.GetCompactionResult()
	if action == nil {
		return extensioncontext.ResultAction{}, r.protocolViolation(
			errors.New("compaction result handler returned another action kind"),
		)
	}
	return mapCompactionResultAction(action)
}

// newCompactionParticipantRequest builds a session-bound participant request with its nested-work lifetime ID.
func newCompactionParticipantRequest(
	handlerID string,
	binding extension.Context,
	parent mo.Option[string],
) *extensionpb.HandleRequest {
	return extensionpb.HandleRequest_builder{
		CompactionOutgoingInput:  nil,
		CompletedConversation:    nil,
		HandlerId:                new(handlerID),
		Context:                  mapContext(binding),
		ParentCompactionId:       parent.ToPointer(),
		SessionBeforeTreeRequest: nil,
		SessionBeforeTreeResult:  nil,
		SessionTree:              nil,
		Lifecycle:                nil,
		ModelSelection:           nil,
		ReasoningSelection:       nil,
		Retry:                    nil,
		CompactionRequest:        nil,
		CompactionGenerate:       nil,
		CompactionResult:         nil,
		CompactionSuccess:        nil,
		CompactionFailure:        nil,
		CompactionPolicy:         nil,
		ProviderHeaders:          nil,
		ProviderBody:             nil,
		ProviderResponse:         nil,
		SystemInstructions:       nil,
		ModelContext:             nil,
		UserInput:                nil,
		FinalizedMessage:         nil,
	}.Build()
}
