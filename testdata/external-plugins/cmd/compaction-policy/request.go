// Package main provides the reference compaction policy fixture.
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"google.golang.org/protobuf/encoding/protojson"

	"google.golang.org/protobuf/proto"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// referenceRequestParticipantCount is the number of request handlers in the fixture.
	referenceRequestParticipantCount = 5
	// referenceRequestComposition selects successful independent action composition.
	referenceRequestComposition = "request-composition"
	// referenceRequestClear selects removal of the supplied result.
	referenceRequestClear = "request-clear-result"
	// referenceRequestCancel selects an exclusive cancellation action.
	referenceRequestCancel = "request-cancel"
	// referenceRequestFail selects a terminal handler error.
	referenceRequestFail = "request-fail"
	// referenceRequestWait selects parent cancellation during participant-owned model work.
	referenceRequestWait = "request-wait-for-caller-cancel"
	// referenceRequestFailAfterChild is the fixture instruction for terminal failure
	// after a request participant handles a nested request rejection.
	referenceRequestFailAfterChild = "request-fail-after-handled-child"
	// referenceRequestInvalid selects an invalid terminal action.
	referenceRequestInvalid = "request-invalid"
	// referenceRequestFirst identifies the first registered request handler.
	referenceRequestFirst = "request-z-first"
	// referenceRequestSecond identifies the request-only replacement handler.
	referenceRequestSecond = "request-y-second"
	// referenceRequestThird identifies the result-only replacement handler.
	referenceRequestThird = "request-x-third"
	// referenceRequestTerminal identifies the conditional terminal handler.
	referenceRequestTerminal = "request-w-terminal"
	// referenceRequestLast identifies the final preservation handler.
	referenceRequestLast = "request-v-last"
	// referenceRequestFirstInstruction identifies the first composed request value.
	referenceRequestFirstInstruction = "first request change"
	// referenceRequestSecondInstruction identifies the request derived from the original.
	referenceRequestSecondInstruction = "second request change from original"
	// referenceRequestInitialSummary identifies the preceding supplied result.
	referenceRequestInitialSummary = "Initial request handler result."
	// referenceRequestFinalSummary identifies the result-only replacement.
	referenceRequestFinalSummary = "Ordered request replacement summary."
)

// newReferenceRequestDescriptors builds request-handler registrations for the fixture.
func newReferenceRequestDescriptors() []*extensionpb.HandlerDescriptor {
	handlers := make([]*extensionpb.HandlerDescriptor, 0, referenceRequestParticipantCount)
	for _, id := range []string{
		referenceRequestFirst, referenceRequestSecond, referenceRequestThird, referenceRequestTerminal, referenceRequestLast,
	} {
		handlers = append(
			handlers,
			extensionpb.HandlerDescriptor_builder{
				Id:   new(id),
				Kind: new(extensionpb.HandlerKind_HANDLER_KIND_COMPACTION_REQUEST),
			}.Build(),
		)
	}
	return handlers
}

// isReferenceRequestGuidance selects the request-composition fixture scenarios.
func isReferenceRequestGuidance(guidance string) bool {
	return slices.Contains(
		[]string{
			referenceRequestComposition,
			referenceRequestClear,
			referenceRequestCancel,
			referenceRequestFail,
			referenceRequestInvalid,
			referenceRequestWait,
			referenceRequestFailAfterChild,
		},
		guidance,
	)
}

// composeRequests owns original values and independent request/result transitions.
func (o *policyOperation) composeRequests(
	ctx context.Context,
	input *extensionpb.CompactionPolicyInvocation,
	boundary string,
) (bool, error) {
	o.state = newReferencePolicyState(input, boundary)
	original := o.state.GetOriginalRequest()
	participants := input.GetRequestParticipants()
	if err := validateReferenceParticipantCount(input.GetPurpose(), len(participants)); err != nil {
		return false, err
	}
	// The extension chooses registration order; Host routes each individual selection.
	for order, participant := range participants {
		if participant.GetRegistrationOrder() != int64(order) || participant.GetExtensionId() == "" ||
			participant.GetRuntimeId() == "" {
			return false, errors.New("captured request participant identity or order is incomplete")
		}
		invocation := extensionpb.CompactionRequestInvocation_builder{
			Original:      original,
			Current:       o.state.GetCurrentRequest(),
			CurrentResult: o.state.GetResult(),
		}.Build()
		operation, err := o.policy.StartRequestHandler(ctx, participant, invocation)
		if err != nil {
			return false, err
		}
		action, err := operation.Wait(ctx)
		if err != nil {
			return false, err
		}
		if actionErr := validateReferenceRequestAction(action); actionErr != nil {
			return false, actionErr
		}
		if action.GetCancel() {
			return true, nil
		}
		if acceptanceErr := o.acceptRequestAction(ctx, action); acceptanceErr != nil {
			return false, acceptanceErr
		}
	}
	if o.state.GetResult() != nil {
		return false, nil
	}
	result, canceled, err := o.generateResult(ctx, input)
	if err != nil || canceled {
		return canceled, err
	}
	if projectionErr := projectPolicyReplacement(ctx, o.policy, result); projectionErr != nil {
		return false, projectionErr
	}
	o.state.SetResult(proto.CloneOf(result))
	return false, nil
}

// referenceRequestOperation exercises request actions and ordinary nested session access.
type referenceRequestOperation struct {
	// handlerID identifies this registered participant.
	handlerID string
	// invocation contains the detached original and current inputs.
	invocation *extensionpb.CompactionRequestInvocation
}

// The interface assertion checks the request participant operation contract.
var _ extensionsdk.HandleOperation = (*referenceRequestOperation)(nil)

// Run supplies the action of one reference request participant.
func (o *referenceRequestOperation) Run(ctx context.Context) (*extensionpb.HandleResponse, error) {
	const referenceRequestInputsEnvironment = "GLYPH_REFERENCE_REQUEST_INPUTS"

	if directory := os.Getenv(referenceRequestInputsEnvironment); directory != "" {
		if err := writeRequestInput(directory, o.handlerID, o.invocation); err != nil {
			return nil, err
		}
	}
	if _, present := extensionsdk.FindCompactionPolicy(ctx); present {
		return nil, errors.New("request participant received policy authority")
	}
	bound, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return nil, err
	}
	if os.Getenv(conversationOverflowEnvironment) == fixtureEnabledValue {
		return o.completeOverflowReplacement(bound)
	}
	if inputErr := validateReferenceRequestInput(o.invocation); inputErr != nil {
		return nil, inputErr
	}
	if nestedErr := o.runNestedRequestWork(ctx, bound); nestedErr != nil {
		return nil, nestedErr
	}
	var action *extensionpb.CompactionRequestAction
	switch o.handlerID {
	case referenceRequestFirst:
		action, err = o.newFirstRequestAction(bound)
	case referenceRequestSecond:
		action, err = o.newSecondRequestAction()
	case referenceRequestThird:
		action, err = o.newThirdRequestAction()
	case referenceRequestTerminal:
		action, err = o.newTerminalRequestAction(ctx, bound)
	case referenceRequestLast:
		action, err = o.newLastRequestAction()
	default:
		return nil, errors.New("unknown reference request handler")
	}
	if err != nil {
		return nil, err
	}

	response := new(extensionpb.HandleResponse)
	response.SetCompactionRequest(action)
	return response, nil
}

// Release ends the prepared participant reservation.
func (*referenceRequestOperation) Release() {}

// writeRequestInput records the public input of one request participant.
func writeRequestInput(directory, handlerID string, input *extensionpb.CompactionRequestInvocation) error {
	const requestInputSuffix = ".json"
	const requestInputPermission = 0o600
	encoded, err := protojson.Marshal(input)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	file, err := root.OpenFile(
		filepath.Base(handlerID)+requestInputSuffix,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		requestInputPermission,
	)
	if err != nil {
		return errors.Join(err, root.Close())
	}
	_, writeErr := file.Write(encoded)
	return errors.Join(writeErr, file.Close(), root.Close())
}

// newReferencePreserveAction supplies independent request and result preservation.
func newReferencePreserveAction() *extensionpb.CompactionRequestAction {
	return extensionpb.CompactionRequestAction_builder{
		Cancel:        new(false),
		RequestAction: new(extensionpb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_PRESERVE),
		Request:       nil,
		ResultAction:  new(extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_PRESERVE),
		Result:        nil,
	}.Build()
}

// newFirstRequestAction creates the initial independent changes.
func (o *referenceRequestOperation) newFirstRequestAction(
	bound *extensionsdk.ExtensionContext,
) (*extensionpb.CompactionRequestAction, error) {
	const (
		initialDetails           = "first-result-details"
		localInstructionMutation = "participant-local original mutation"
		localEntryMutation       = "participant-local entry mutation"
	)
	original, current, result := o.invocation.GetOriginal(), o.invocation.GetCurrent(), o.invocation.GetCurrentResult()
	action := newReferencePreserveAction()
	if !proto.Equal(original, current) || result != nil {
		return nil, errors.New("first request handler did not receive initial state")
	}
	replacement := proto.CloneOf(current)
	replacement.SetInstructions(referenceRequestFirstInstruction)
	supplied := extensionpb.CompactionResult_builder{
		Summary:          new(referenceRequestInitialSummary),
		FirstKeptEntryId: new(original.GetSuffix()[0].GetEntry().GetId()),
		Source: extensionpb.BranchSummarySource_builder{
			ExtensionId: new(bound.Identity().GetExtensionId()),
			Model:       nil,
		}.Build(),
		Details: []byte(initialDetails),
	}.Build()
	if os.Getenv(referenceCandidateRejectionEnvironment) == referenceCandidateRequest {
		supplied.SetFirstKeptEntryId(referenceGeneratorOutsideBranch)
	}
	action.SetRequestAction(extensionpb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_REPLACE)
	action.SetRequest(replacement)
	action.SetResultAction(extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE)
	action.SetResult(supplied)
	// Mutating this invocation must not change the policy's original or the next invocation.
	original.SetInstructions(localInstructionMutation)
	original.GetPrefix()[0].GetEntry().SetId(localEntryMutation)
	return action, nil
}

// newSecondRequestAction creates a request-only replacement action.
func (o *referenceRequestOperation) newSecondRequestAction() (*extensionpb.CompactionRequestAction, error) {
	original, current, result := o.invocation.GetOriginal(), o.invocation.GetCurrent(), o.invocation.GetCurrentResult()
	action := newReferencePreserveAction()
	if current.GetInstructions() != referenceRequestFirstInstruction ||
		result.GetSummary() != referenceRequestInitialSummary {
		return nil, errors.New("second request handler did not receive first independent changes")
	}
	replacement := proto.CloneOf(original)
	replacement.SetInstructions(referenceRequestSecondInstruction)
	if original.GetTrigger() == extensionpb.CompactionTrigger_COMPACTION_TRIGGER_THRESHOLD {
		const summaryModelID = "gpt-test"
		replacement.GetModel().SetModelId(summaryModelID)
	}
	action.SetRequestAction(extensionpb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_REPLACE)
	action.SetRequest(replacement)
	return action, nil
}

// newThirdRequestAction creates the result-only replacement.
func (o *referenceRequestOperation) newThirdRequestAction() (*extensionpb.CompactionRequestAction, error) {
	const (
		replacementDetails           = "result-only-details"
		sourceEnvironment            = "GLYPH_REFERENCE_REQUEST_SOURCE"
		otherExtensionSource         = "other-extension"
		modelSource                  = "model"
		forgedProducer               = "request-forged-producer"
		resultProvider               = "request-result-provider"
		resultModel                  = "request-result-model"
		resultReasoning              = "high"
		resultInputTokens      int64 = 4
		resultOutputTokens     int64 = 4
		resultCacheReadTokens  int64 = 2
		resultCacheWriteTokens int64 = 1
		resultReasoningTokens  int64 = 3
		resultTotalTokens      int64 = 11
	)
	current, result := o.invocation.GetCurrent(), o.invocation.GetCurrentResult()
	action := newReferencePreserveAction()
	if current.GetInstructions() != referenceRequestSecondInstruction ||
		result.GetSummary() != referenceRequestInitialSummary {
		return nil, errors.New("third request handler did not receive preserved result with replaced request")
	}
	replacement := proto.CloneOf(result)
	replacement.SetSummary(referenceRequestFinalSummary)
	replacement.SetDetails([]byte(replacementDetails))
	switch os.Getenv(sourceEnvironment) {
	case otherExtensionSource:
		replacement.GetSource().SetExtensionId(forgedProducer)
	case modelSource:
		replacement.SetSource(extensionpb.BranchSummarySource_builder{
			ExtensionId: nil,
			Model: extensionpb.BranchSummaryModelSource_builder{
				Selection: extensionpb.ModelSelection_builder{
					ProviderId: new(resultProvider), ModelId: new(resultModel),
					ReasoningChoice: new(resultReasoning),
				}.Build(),
				Usage: extensionpb.TokenUsage_builder{
					InputTokens: new(resultInputTokens), OutputTokens: new(resultOutputTokens),
					CacheReadTokens: new(resultCacheReadTokens), CacheWriteTokens: new(resultCacheWriteTokens),
					ReasoningTokens: new(resultReasoningTokens), TotalTokens: new(resultTotalTokens),
				}.Build(),
			}.Build(),
		}.Build())
		replacement.SetDetails([]byte{})
	}
	action.SetResultAction(extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE)
	action.SetResult(replacement)
	return action, nil
}

// newTerminalRequestAction creates the terminal or clearing action.
func (o *referenceRequestOperation) newTerminalRequestAction(
	ctx context.Context,
	bound *extensionsdk.ExtensionContext,
) (*extensionpb.CompactionRequestAction, error) {
	const referenceRequestFailureText = "reference request handler failure: complete source cause"

	original, current, result := o.invocation.GetOriginal(), o.invocation.GetCurrent(), o.invocation.GetCurrentResult()
	action := newReferencePreserveAction()
	if current.GetInstructions() != referenceRequestSecondInstruction ||
		result.GetSummary() != referenceRequestFinalSummary {
		return nil, errors.New("terminal request handler did not receive result-only changes")
	}
	switch original.GetInstructions() {
	case referenceRequestCancel:
		action = extensionpb.CompactionRequestAction_builder{
			Cancel:        new(true),
			RequestAction: nil,
			Request:       nil,
			ResultAction:  nil,
			Result:        nil,
		}.Build()
	case referenceRequestFail, referenceRequestFailAfterChild:
		return nil, errors.New(referenceRequestFailureText)
	case referenceRequestInvalid:
		action.SetCancel(true)
	case referenceRequestClear:
		action.SetResultAction(extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_CLEAR)
	case referenceRequestWait:
		if err := handleMissingModelSelection(ctx, bound, original.GetModel().GetProviderId()); err != nil {
			return nil, err
		}
		if err := waitPolicyConfiguredModel(
			ctx,
			bound,
			original.GetModel(),
			original.GetReasoningChoice(),
		); err != nil {
			return nil, err
		}
	case referenceRequestComposition:
	}
	return action, nil
}

// newLastRequestAction supplies the final participant's preservation action.
func (o *referenceRequestOperation) newLastRequestAction() (*extensionpb.CompactionRequestAction, error) {
	original, current, result := o.invocation.GetOriginal(), o.invocation.GetCurrent(), o.invocation.GetCurrentResult()
	action := newReferencePreserveAction()
	if current.GetInstructions() != referenceRequestSecondInstruction {
		return nil, errors.New("last handler lost the independent current request")
	}
	if original.GetInstructions() == referenceRequestClear {
		if result != nil {
			return nil, errors.New("last handler did not receive cleared result")
		}
	} else if original.GetInstructions() != referenceRequestComposition ||
		result.GetSummary() != referenceRequestFinalSummary {
		return nil, errors.New("request handler ran after terminal action")
	}
	return action, nil
}

// validateReferenceRequestInput checks the detached original and current values delivered to a participant.
func validateReferenceRequestInput(input *extensionpb.CompactionRequestInvocation) error {
	original, current := input.GetOriginal(), input.GetCurrent()
	if original == nil || current == nil || !isReferenceRequestGuidance(original.GetInstructions()) {
		return errors.New("request original input changed")
	}
	if len(original.GetPrefix()) == 0 || len(original.GetSuffix()) == 0 {
		return errors.New("request original entries are missing")
	}
	return nil
}

// runNestedRequestWork exercises ordinary session access and a handled child failure.
func (o *referenceRequestOperation) runNestedRequestWork(
	ctx context.Context,
	bound *extensionsdk.ExtensionContext,
) error {
	nested, err := bound.StartGetSessionState(ctx)
	if err != nil {
		return err
	}
	if _, err = nested.Wait(ctx); err != nil {
		return err
	}
	original := o.invocation.GetOriginal()
	if o.handlerID == referenceRequestFirst && original.GetInstructions() == referenceRequestFailAfterChild {
		return handleMissingModelSelection(ctx, bound, original.GetModel().GetProviderId())
	}
	return nil
}
