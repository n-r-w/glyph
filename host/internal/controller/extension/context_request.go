// Package extension maps extension-initiated requests to session-bound Host operations.
package extension

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// contextRequest contains one validated operation selector and its public reference.
type contextRequest struct {
	// reference identifies the issued binding.
	reference *extensionpb.ExtensionContextRef
	// prepare binds the one selected executable action.
	prepare prepareContextAction
	// admission identifies required gate or parent ownership.
	admission contextAdmission
}

// newContextRequest constructs the operation selector for admission.
func newContextRequest(request *extensionpb.ExtensionRequest) (contextRequest, error) {
	mapped := contextRequest{reference: nil, prepare: nil, admission: contextAdmissionRegular}
	if request == nil {
		return mapped, errors.New("extension context request is required")
	}
	switch request.WhichRequest() {
	case extensionpb.ExtensionRequest_GetModels_case:
		mapped.reference = request.GetGetModels().GetContext()
		mapped.prepare = func(o *contextOperation) (contextAction, error) {
			return contextReadAction(o.runModelCatalog), nil
		}
	case extensionpb.ExtensionRequest_GetProviders_case:
		mapped.reference = request.GetGetProviders().GetContext()
		mapped.prepare = func(o *contextOperation) (contextAction, error) {
			return contextReadAction(o.runProviderCatalog), nil
		}
	case extensionpb.ExtensionRequest_ConfiguredModel_case:
		return newConfiguredContextRequest(mapped, request.GetConfiguredModel())
	case extensionpb.ExtensionRequest_AppendExtension_case:
		return newAppendContextRequest(mapped, request.GetAppendExtension())
	case extensionpb.ExtensionRequest_GetSessionState_case:
		mapped.reference = request.GetGetSessionState().GetContext()
		mapped.prepare = func(o *contextOperation) (contextAction, error) {
			return contextReadAction(o.runSessionState), nil
		}
	case extensionpb.ExtensionRequest_AppendExtensionMessage_case:
		return newAppendMessageContextRequest(mapped, request.GetAppendExtensionMessage())
	case extensionpb.ExtensionRequest_InvokeCompactionRequest_case:
		return newPolicyRequestContextRequest(mapped, request.GetInvokeCompactionRequest())
	case extensionpb.ExtensionRequest_ProjectCompaction_case:
		return newPolicyCandidateContextRequest(mapped, request.GetProjectCompaction(), policyCandidateProjection)
	case extensionpb.ExtensionRequest_ReplaceCompaction_case:
		return newPolicyCandidateContextRequest(mapped, request.GetReplaceCompaction(), policyCandidateReplacement)
	case extensionpb.ExtensionRequest_Compact_case:
		compact := request.GetCompact()
		mapped.reference = compact.GetContext()
		instructions := mo.TupleToOption(compact.GetInstructions(), compact.HasInstructions())
		mapped.admission = contextAdmissionManual
		mapped.prepare = func(o *contextOperation) (contextAction, error) {
			return contextProgressAction(
				func(ctx context.Context, reporter *extensionsdk.HostProgressReporter) (*extensionpb.HostCompleted, error) {
					return o.runManualCompaction(ctx, reporter, instructions)
				},
			), nil
		}
	case extensionpb.ExtensionRequest_Request_not_set_case, extensionpb.ExtensionRequest_Cancel_case:
		return contextRequest{}, errors.New("extension context operation request is required")
	case extensionpb.ExtensionRequest_SelectModel_case:
		return newModelSelectionContextRequest(mapped, request.GetSelectModel())
	case extensionpb.ExtensionRequest_SelectReasoning_case:
		return newReasoningSelectionContextRequest(mapped, request.GetSelectReasoning())
	default:
		return contextRequest{}, errors.New("unsupported extension context request")
	}
	return mapped, nil
}

// newPolicyCandidateContextRequest constructs an operation selector for a scoped compaction candidate.
func newPolicyCandidateContextRequest(
	mapped contextRequest,
	raw *extensionpb.CompactionCandidateRequest,
	kind policyCandidateKind,
) (contextRequest, error) {
	if raw == nil || raw.GetOperationId() == "" || raw.GetResult() == nil {
		return contextRequest{}, errors.New("compaction candidate requires operation authority and a complete result")
	}
	result := raw.GetResult()
	source, err := mapPolicyCandidateSource(result.GetSource())
	if err != nil {
		return contextRequest{}, err
	}
	details := mo.None[[]byte]()
	if result.HasDetails() {
		details = mo.Some(bytes.Clone(result.GetDetails()))
	}
	mapped.reference = raw.GetContext()
	candidate := policyCandidate{kind: kind, operationID: raw.GetOperationId(), value: session.CompactionEntry{
		Summary: result.GetSummary(), FirstKeptEntryID: result.GetFirstKeptEntryId(), Source: source,
		EstimatedCost: mo.None[session.EstimatedCost](), Details: details,
	}}
	mapped.admission = contextAdmissionCandidate
	mapped.prepare = func(o *contextOperation) (contextAction, error) {
		return contextReadAction(func(ctx context.Context) (*extensionpb.HostCompleted, error) {
			return o.runPolicyCandidate(ctx, candidate)
		}), nil
	}

	return mapped, nil
}

// newConfiguredContextRequest constructs an operation selector for a configured model.
func newConfiguredContextRequest(
	mapped contextRequest,
	request *extensionpb.ConfiguredModelRequest,
) (contextRequest, error) {
	configured, err := mapConfiguredRequest(request)
	if err != nil {
		return contextRequest{}, err
	}
	mapped.reference = request.GetContext()
	mapped.prepare = func(o *contextOperation) (contextAction, error) {
		return contextProgressAction(
			func(ctx context.Context, reporter *extensionsdk.HostProgressReporter) (*extensionpb.HostCompleted, error) {
				return o.runConfiguredModel(ctx, reporter, configured)
			},
		), nil
	}
	return mapped, nil
}

// newAppendContextRequest constructs an operation selector for a hidden extension entry.
func newAppendContextRequest(
	mapped contextRequest,
	request *extensionpb.AppendExtensionRequest,
) (contextRequest, error) {
	appendValue, err := mapAppendRequest(request)
	if err != nil {
		return contextRequest{}, err
	}
	mapped.reference = request.GetContext()
	mapped.prepare = func(o *contextOperation) (contextAction, error) {
		return contextReadAction(func(ctx context.Context) (*extensionpb.HostCompleted, error) {
			return o.runAppendValue(ctx, appendValue)
		}), nil
	}
	return mapped, nil
}

// newAppendMessageContextRequest constructs an operation selector for a visible extension message.
func newAppendMessageContextRequest(
	mapped contextRequest,
	request *extensionpb.AppendExtensionMessageRequest,
) (contextRequest, error) {
	message, err := mapAppendMessageRequest(request)
	if err != nil {
		return contextRequest{}, err
	}
	mapped.reference = request.GetContext()
	mapped.prepare = func(o *contextOperation) (contextAction, error) {
		return contextReadAction(func(ctx context.Context) (*extensionpb.HostCompleted, error) {
			return o.runAppendMessage(ctx, message)
		}), nil
	}
	return mapped, nil
}

// newPolicyRequestContextRequest binds one participant call to the policy operation.
func newPolicyRequestContextRequest(
	mapped contextRequest,
	wire *extensionpb.InvokeCompactionRequestRequest,
) (contextRequest, error) {
	if wire == nil || wire.GetOperationId() == "" || wire.GetParticipant() == nil || wire.GetInvocation() == nil {
		return contextRequest{}, errors.New("complete captured request invocation is required")
	}
	original, err := decodePolicyRequest(wire.GetInvocation().GetOriginal())
	if err != nil {
		return contextRequest{}, fmt.Errorf("decode original request: %w", err)
	}
	current, err := decodePolicyRequest(wire.GetInvocation().GetCurrent())
	if err != nil {
		return contextRequest{}, fmt.Errorf("decode current request: %w", err)
	}
	result := mo.None[session.CompactionEntry]()
	if supplied := wire.GetInvocation().GetCurrentResult(); supplied != nil {
		source, sourceErr := mapPolicyCandidateSource(supplied.GetSource())
		if sourceErr != nil {
			return contextRequest{}, sourceErr
		}
		details := mo.TupleToOption(append([]byte(nil), supplied.GetDetails()...), supplied.HasDetails())
		result = mo.Some(
			session.CompactionEntry{
				Summary:          supplied.GetSummary(),
				FirstKeptEntryID: supplied.GetFirstKeptEntryId(),
				Source:           source,
				Details:          details,
				EstimatedCost:    mo.None[session.EstimatedCost](),
			},
		)
	}
	target := wire.GetParticipant()
	command := CompactionRequestCommand{
		OperationID: wire.GetOperationId(),
		Participant: CompactionRequestParticipant{
			ExtensionID:       target.GetExtensionId(),
			RuntimeID:         target.GetRuntimeId(),
			HandlerID:         target.GetHandlerId(),
			RegistrationOrder: target.GetRegistrationOrder(),
		},
		Original:      original,
		Current:       current,
		CurrentResult: result,
	}
	mapped.reference = wire.GetContext()
	mapped.admission = contextAdmissionCandidate
	mapped.prepare = func(o *contextOperation) (contextAction, error) {
		return contextReadAction(func(ctx context.Context) (*extensionpb.HostCompleted, error) {
			action, invokeErr := o.service.compactionPolicy.InvokePolicyRequest(
				ctx,
				o.service.extensionID,
				o.service.runtimeID,
				command,
			)
			if invokeErr != nil {
				return nil, extensionsdk.Fail(compactionFailureCode(invokeErr), invokeErr)
			}
			response, encodeErr := action.encodePolicyRequestAction()
			if encodeErr != nil {
				return nil, extensionsdk.Fail(compactionFailureCode(encodeErr), encodeErr)
			}
			completed := new(extensionpb.HostCompleted)
			completed.SetInvokeCompactionRequest(response)
			return completed, nil
		}), nil
	}
	return mapped, nil
}

// newModelSelectionContextRequest prepares a context request for model selection.
func newModelSelectionContextRequest(
	mapped contextRequest,
	request *extensionpb.SelectModelRequest,
) (contextRequest, error) {
	selection, err := mapModelSelectionRequest(request)
	if err != nil {
		return contextRequest{}, err
	}
	mapped.reference = request.GetContext()
	mapped.prepare = func(o *contextOperation) (contextAction, error) { return o.prepareSelectionAction(selection) }
	return mapped, nil
}

// newReasoningSelectionContextRequest prepares a context request for reasoning selection.
func newReasoningSelectionContextRequest(
	mapped contextRequest,
	request *extensionpb.SelectReasoningRequest,
) (contextRequest, error) {
	selection, err := mapReasoningSelectionRequest(request)
	if err != nil {
		return contextRequest{}, err
	}
	mapped.reference = request.GetContext()
	mapped.prepare = func(o *contextOperation) (contextAction, error) { return o.prepareSelectionAction(selection) }
	return mapped, nil
}
