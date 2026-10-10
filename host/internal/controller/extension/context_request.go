// Package extension maps extension-initiated requests to session-bound Host operations.
package extension

import (
	"bytes"
	"context"
	"errors"

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

// mapContextRequest validates the selected request payload before admission.
func mapContextRequest(request *extensionpb.ExtensionRequest) (contextRequest, error) {
	// mapped contains the selected request payload and execution binding.
	mapped := contextRequest{reference: nil, prepare: nil, admission: contextAdmissionRegular}
	if request == nil {
		return mapped, errors.New("extension context request is required")
	}
	// selection and reference contain the selection command and binding.
	// selected indicates selection work; err is the decoding failure.
	selection, reference, selected, err := mapSelectionContextRequest(request)
	if err != nil {
		return contextRequest{}, err
	}
	if selected {
		mapped.reference = reference
		mapped.prepare = func(o *contextOperation) (contextAction, error) { return o.prepareSelectionAction(selection) }
		return mapped, nil
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
		return mapConfiguredContextRequest(mapped, request.GetConfiguredModel())
	case extensionpb.ExtensionRequest_AppendExtension_case:
		return mapAppendContextRequest(mapped, request.GetAppendExtension())
	case extensionpb.ExtensionRequest_GetSessionState_case:
		mapped.reference = request.GetGetSessionState().GetContext()
		mapped.prepare = func(o *contextOperation) (contextAction, error) {
			return contextReadAction(o.runSessionState), nil
		}
	case extensionpb.ExtensionRequest_AppendExtensionMessage_case:
		return mapAppendMessageContextRequest(mapped, request.GetAppendExtensionMessage())
	case extensionpb.ExtensionRequest_ProjectCompaction_case:
		return mapPolicyCandidate(mapped, request.GetProjectCompaction(), policyCandidateProjection)
	case extensionpb.ExtensionRequest_ReplaceCompaction_case:
		return mapPolicyCandidate(mapped, request.GetReplaceCompaction(), policyCandidateReplacement)
	case extensionpb.ExtensionRequest_Compact_case:
		// compact contains the manual compaction request.
		compact := request.GetCompact()
		mapped.reference = compact.GetContext()
		// instructions contains optional user guidance for manual compaction.
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
	case extensionpb.ExtensionRequest_SelectModel_case, extensionpb.ExtensionRequest_SelectReasoning_case:
		return contextRequest{}, errors.New("selection context request mapping did not complete")
	default:
		return contextRequest{}, errors.New("unsupported extension context request")
	}
	return mapped, nil
}

// mapPolicyCandidate validates and decodes one scoped replacement candidate.
func mapPolicyCandidate(
	mapped contextRequest,
	raw *extensionpb.CompactionCandidateRequest,
	kind policyCandidateKind,
) (contextRequest, error) {
	if raw == nil || raw.GetOperationId() == "" || raw.GetResult() == nil {
		return contextRequest{}, errors.New("compaction candidate requires operation authority and a complete result")
	}
	// result contains the complete wire candidate.
	result := raw.GetResult()
	// source and err contain the candidate producer and its decoding failure.
	source, err := mapPolicyCandidateSource(result.GetSource())
	if err != nil {
		return contextRequest{}, err
	}
	// details contains optional detached policy metadata.
	details := mo.None[[]byte]()
	if result.HasDetails() {
		details = mo.Some(bytes.Clone(result.GetDetails()))
	}
	mapped.reference = raw.GetContext()
	// candidate contains the replacement request bound to its policy authority.
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

// mapConfiguredContextRequest maps one configured-model request into the general context selector.
func mapConfiguredContextRequest(
	mapped contextRequest,
	request *extensionpb.ConfiguredModelRequest,
) (contextRequest, error) {
	// configured contains the model request; err is its decoding failure.
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

// mapAppendContextRequest maps one hidden extension append into the general context selector.
func mapAppendContextRequest(
	mapped contextRequest,
	request *extensionpb.AppendExtensionRequest,
) (contextRequest, error) {
	// appendValue contains the hidden entry request; err is its decoding failure.
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

// mapAppendMessageContextRequest maps one visible extension message into the general context selector.
func mapAppendMessageContextRequest(
	mapped contextRequest,
	request *extensionpb.AppendExtensionMessageRequest,
) (contextRequest, error) {
	// message contains the visible message request; err is its decoding failure.
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
