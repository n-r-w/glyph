// Package main supplies a public-only reference compaction policy.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// policyHandlerID identifies the reference policy capability.
	policyHandlerID = "reference-policy"
	// invalidArgumentCode identifies rejected unsupported fixture operations.
	invalidArgumentCode = "INVALID_ARGUMENT"
	// separateObserverEnvironment selects isolated fixture process roles.
	separateObserverEnvironment = "GLYPH_REFERENCE_SEPARATE_OBSERVER"
)

// policyService supplies registration and policy invocation operations.
type policyService struct {
	// observations is the number of completed-conversation invocations.
	observations atomic.Int64
	// workerStart owns the one caller worker launch.
	workerStart sync.Once
	// listener owns ordinary caller coordination.
	listener net.Listener
	// disabled removes only the policy owner capability.
	disabled bool
	// observe supplies the session-context observer capability.
	observe bool
}

// The interface assertion checks the fixture capability contract.
var _ extensionsdk.Service = (*policyService)(nil)

// policyRegistration supplies the reference capability catalog.
type policyRegistration struct {
	// disabled removes the policy owner capability.
	disabled bool
	// observe adds the session-context observer capability.
	observe bool
}

// The interface assertion checks the fixture capability contract.
var _ extensionsdk.RegisterOperation = (*policyRegistration)(nil)

// policyOperation retains invocation-scoped replacement authority.
type policyOperation struct {
	// policy contains captured input and the issued mutation authority.
	policy *extensionsdk.CompactionPolicyContext
	// state contains composition values acquired by this reference policy.
	state *extensionpb.CompactionPolicyState
}

// The interface assertion checks the fixture capability contract.
var _ extensionsdk.HandleOperation = (*policyOperation)(nil)

// main starts the independently built reference extension.
func main() {
	const (
		disabledPolicyEnvironment = "GLYPH_REFERENCE_POLICY_DISABLED"
		callerSocketEnvironment   = "GLYPH_REFERENCE_CALLER_SOCKET"
		callerExecutable          = "reference-caller"
	)

	service := &policyService{
		observations: atomic.Int64{}, workerStart: sync.Once{}, listener: nil,
		disabled: os.Getenv(disabledPolicyEnvironment) == fixtureEnabledValue ||
			(os.Getenv(separateObserverEnvironment) == fixtureEnabledValue && filepath.Base(os.Args[0]) != policyHandlerID),
		observe: os.Getenv(callerSocketEnvironment) != "" &&
			(os.Getenv(separateObserverEnvironment) != fixtureEnabledValue || filepath.Base(os.Args[0]) == callerExecutable),
	}
	if service.observe {
		listenConfig := &net.ListenConfig{}
		listener, err := listenConfig.Listen(
			context.Background(),
			fixtureSocketNetwork,
			os.Getenv(callerSocketEnvironment),
		)
		if err != nil {
			panic(err)
		}
		service.listener = listener
	}
	extensionsdk.Serve(service)
}

// PrepareRegister admits capability registration.
func (s *policyService) PrepareRegister(
	_ context.Context,
	_ *extensionpb.RegisterRequest,
) (extensionsdk.RegisterOperation, error) {
	if path := os.Getenv(conversationPolicyPIDEnvironment); path != "" {
		if err := writePolicyArtifactBytes(path, []byte(strconv.Itoa(os.Getpid()))); err != nil {
			return nil, err
		}
	}
	return &policyRegistration{disabled: s.disabled, observe: s.observe}, nil
}

// PrepareHandle admits the reference extension's handler operations.
func (s *policyService) PrepareHandle(
	ctx context.Context,
	request *extensionpb.HandleRequest,
) (extensionsdk.HandleOperation, error) {
	if outgoing := request.GetCompactionOutgoingInput(); outgoing != nil {
		if _, present := extensionsdk.FindCompactionPolicy(ctx); present {
			return nil, extensionsdk.Reject(
				invalidArgumentCode,
				errors.New("outgoing input check received mutation authority"),
			)
		}
		return &outgoingInputOperation{input: outgoing}, nil
	}
	if observation := request.GetCompletedConversation(); observation != nil {
		if _, present := extensionsdk.FindCompactionPolicy(ctx); present {
			return nil, extensionsdk.Reject(
				invalidArgumentCode,
				errors.New("conversation observation received mutation authority"),
			)
		}
		return &conversationObservationOperation{input: observation, sequence: s.observations.Add(1)}, nil
	}
	if request.GetCompactionResult() != nil {
		return &referenceResultOperation{
			handlerID:  request.GetHandlerId(),
			invocation: request.GetCompactionResult(),
		}, nil
	}
	if request.GetCompactionGenerate() != nil {
		return &referenceGeneratorOperation{invocation: request.GetCompactionGenerate()}, nil
	}
	if request.GetCompactionRequest() != nil {
		return &referenceRequestOperation{
			handlerID:  request.GetHandlerId(),
			invocation: request.GetCompactionRequest(),
		}, nil
	}
	if request.GetLifecycle().GetAgentSettled() != nil {
		return &settledOperation{service: s}, nil
	}
	if outcome := request.GetCompactionSuccess(); outcome != nil {
		return &outcomeOperation{outcome: outcome}, nil
	}
	if outcome := request.GetCompactionFailure(); outcome != nil {
		return &outcomeOperation{outcome: outcome}, nil
	}
	if request.GetCompactionPolicy() == nil {
		return nil, extensionsdk.Reject(invalidArgumentCode, errors.New("reference policy payload is required"))
	}
	policy, present := extensionsdk.FindCompactionPolicy(ctx)
	if !present {
		return nil, extensionsdk.Reject(invalidArgumentCode, errors.New("reference policy authority is required"))
	}
	return &policyOperation{policy: policy, state: nil}, nil
}

// PrepareExecute admits the observation fixture's model-callable tool.
func (s *policyService) PrepareExecute(
	_ context.Context,
	request *extensionpb.ExecuteRequest,
) (extensionsdk.ExecuteOperation, error) {
	if request.GetToolName() == conversationToolName && os.Getenv(conversationToolEnvironment) != "" {
		return &conversationToolOperation{}, nil
	}
	return nil, extensionsdk.Reject(invalidArgumentCode, errors.New("reference policy has no tools"))
}

// Run publishes the complete reference capability catalog.
func (r *policyRegistration) Run(_ context.Context) (*extensionpb.RegisterResponse, error) {
	const (
		// referenceRequestsExecutable identifies the request-only runtime fixture.
		referenceRequestsExecutable = "reference-requests"
		// referenceSeparateRequestsEnvironment selects request handlers in a separate runtime.
		referenceSeparateRequestsEnvironment = "GLYPH_REFERENCE_SEPARATE_REQUESTS"
		settledHandlerID                     = "reference-settled"
		successHandlerID                     = "reference-compaction-success"
		failureHandlerID                     = "reference-compaction-failure"
		observerExecutable                   = "reference-observer"
	)

	handlers := make([]*extensionpb.HandlerDescriptor, 0)
	if filepath.Base(os.Args[0]) == referenceResultsExecutable {
		return extensionpb.RegisterResponse_builder{Tools: nil, Handlers: newReferenceResultDescriptors()}.Build(), nil
	}
	if filepath.Base(os.Args[0]) == referenceGeneratorExecutable {
		return extensionpb.RegisterResponse_builder{
			Tools:    nil,
			Handlers: newReferenceGeneratorDescriptors(),
		}.Build(), nil
	}
	if filepath.Base(os.Args[0]) == referenceRequestsExecutable {
		return extensionpb.RegisterResponse_builder{Tools: nil, Handlers: newReferenceRequestDescriptors()}.Build(), nil
	}
	if os.Getenv(separateObserverEnvironment) != fixtureEnabledValue ||
		filepath.Base(os.Args[0]) == observerExecutable {
		handlers = append(handlers,
			extensionpb.HandlerDescriptor_builder{
				Id:   new(successHandlerID),
				Kind: new(extensionpb.HandlerKind_HANDLER_KIND_COMPACTION_SUCCESS),
			}.Build(),
			extensionpb.HandlerDescriptor_builder{
				Id:   new(failureHandlerID),
				Kind: new(extensionpb.HandlerKind_HANDLER_KIND_COMPACTION_FAILURE),
			}.Build(),
		)
	}
	if !r.disabled {
		handlers = append(
			handlers,
			extensionpb.HandlerDescriptor_builder{
				Id:   new(policyHandlerID),
				Kind: new(extensionpb.HandlerKind_HANDLER_KIND_COMPACTION_POLICY),
			}.Build(),
		)
	}
	if r.observe {
		handlers = append(
			handlers,
			extensionpb.HandlerDescriptor_builder{
				Id:   new(settledHandlerID),
				Kind: new(extensionpb.HandlerKind_HANDLER_KIND_AGENT_SETTLED),
			}.Build(),
		)
	}
	if os.Getenv(referenceSeparateRequestsEnvironment) != fixtureEnabledValue {
		requests := newReferenceRequestDescriptors()
		if os.Getenv(conversationOverflowEnvironment) == fixtureEnabledValue {
			requests = requests[:1]
		}
		handlers = append(handlers, requests...)
	}
	if os.Getenv(referenceSeparateGeneratorEnvironment) != fixtureEnabledValue &&
		os.Getenv(conversationOverflowEnvironment) != fixtureEnabledValue {
		handlers = append(handlers, newReferenceGeneratorDescriptors()...)
	}
	if os.Getenv(referenceSeparateResultsEnvironment) != fixtureEnabledValue &&
		os.Getenv(conversationOverflowEnvironment) != fixtureEnabledValue {
		handlers = append(handlers, newReferenceResultDescriptors()...)
	}
	return extensionpb.RegisterResponse_builder{
		Tools:    newConversationObservationTools(),
		Handlers: handlers,
	}.Build(), nil
}

// Release completes registration admission.
func (r *policyRegistration) Release() {}

// completeReplacement supplies the acquired policy completion after persistence.
func (o *policyOperation) completeReplacement(guidance string) *extensionpb.HandleResponse {
	const (
		unavailableObserverGuidance = "lose-shared-owner-after-commit"
		invalidStateGuidance        = "invalid-state-after-commit"
		invalidStateCause           = "reference invalid diagnostic producer: original policy failure cause"
		lostStateGuidance           = policyLoseAfterCommit
		// lostStateExit identifies deliberate loss of the policy process after persistence.
		lostStateExit   = 23
		failureGuidance = "fail-after-commit"
		failureCause    = "reference policy failure after durable replacement: original producer cause"
	)

	if guidance == lostStateGuidance || guidance == unavailableObserverGuidance {
		os.Exit(lostStateExit)
	}
	if guidance == invalidStateGuidance {
		o.state.ClearCurrentRequest()
		return o.attachAcquiredState(newPolicyFailure(errors.New(invalidStateCause)))
	}
	if guidance == failureGuidance {
		return o.attachAcquiredState(newPolicyFailure(errors.New(failureCause)))
	}
	const (
		// automaticPreserveAfterCommit names the scenario where the policy acknowledges preservation after mutation.
		automaticPreserveAfterCommit = "preserve-after-commit"
		// automaticCanceledAfter names the scenario where the policy ends preparation after mutation.
		automaticCanceledAfter = "canceled-after"
	)
	disposition := extensionpb.CompactionPolicyDisposition_COMPACTION_POLICY_DISPOSITION_REPLACED
	if o.policy.Input().GetPurpose() == extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_AUTOMATIC {
		switch os.Getenv(automaticModeEnvironment) {
		case automaticPreserveAfterCommit:
			disposition = extensionpb.CompactionPolicyDisposition_COMPACTION_POLICY_DISPOSITION_PRESERVE
		case automaticCanceledAfter:
			disposition = extensionpb.CompactionPolicyDisposition_COMPACTION_POLICY_DISPOSITION_CANCELED
		}
	}
	response := new(extensionpb.HandleResponse)
	response.SetCompactionPolicy(
		extensionpb.CompactionPolicyResult_builder{State: nil, Error: nil, Disposition: disposition.Enum()}.Build(),
	)
	return o.attachAcquiredState(response)
}

// attachAcquiredState supplies diagnostic values and policy failure in one completion.
func (o *policyOperation) attachAcquiredState(response *extensionpb.HandleResponse) *extensionpb.HandleResponse {
	if o.state == nil {
		return response
	}
	failure := response.GetError().GetMessage()
	result := response.GetCompactionPolicy()
	if result == nil {
		result = extensionpb.CompactionPolicyResult_builder{
			State:       nil,
			Error:       nil,
			Disposition: extensionpb.CompactionPolicyDisposition_COMPACTION_POLICY_DISPOSITION_REPLACED.Enum(),
		}.Build()
		response.SetCompactionPolicy(result)
	}
	result.SetState(o.state)
	result.SetError(failure)
	return response
}

// Release completes policy admission.
func (o *policyOperation) Release() {}

// newPolicyFailure supplies an ordinary policy failure with its complete cause.
func newPolicyFailure(err error) *extensionpb.HandleResponse {
	response := new(extensionpb.HandleResponse)
	response.SetError(extensionpb.HandlerError_builder{Message: new(err.Error())}.Build())
	return response
}

// findPolicyBoundary selects the most recent original user entry.
func findPolicyBoundary(entries []*extensionpb.CompactionSnapshotEntry) string {
	boundary := ""
	for _, snapshotEntry := range slices.Backward(entries) {
		entry := snapshotEntry.GetEntry()
		if entry.GetUser() != nil {
			boundary = entry.GetId()
			break
		}
	}
	return boundary
}

// settledOperation completes the session-context observation.
type settledOperation struct {
	// service supplies caller coordination.
	service *policyService
}

// The interface assertion checks the fixture capability contract.
var _ extensionsdk.HandleOperation = (*settledOperation)(nil)

// Run completes the observer without keeping the conversation gate active.
func (o *settledOperation) Run(ctx context.Context) (*extensionpb.HandleResponse, error) {
	if o.service.listener != nil {
		binding, err := extensionsdk.ContextFrom(ctx)
		if err != nil {
			return nil, err
		}
		o.service.workerStart.Do(func() {
			go func(callerContext context.Context, caller *extensionsdk.ExtensionContext, listener net.Listener) {
				if callerErr := runCaller(callerContext, caller, listener); callerErr != nil {
					fmt.Fprintln(os.Stderr, callerErr)
				}
			}(context.WithoutCancel(ctx), binding, o.service.listener)
		})
	}
	response := new(extensionpb.HandleResponse)
	response.SetLifecycle(new(extensionpb.LifecycleAction))
	return response, nil
}

// Release completes observer admission.
func (o *settledOperation) Release() {}

// waitPolicyConfiguredModel keeps actual nested work owned by its policy invocation.
func waitPolicyConfiguredModel(
	ctx context.Context,
	binding *extensionsdk.ExtensionContext,
	descriptor *extensionpb.ModelDescriptor, reasoningChoice string,
) error {
	const (
		nestedCheckpoint = "nested cancellation checkpoint"
	)
	content := new(extensionpb.ConfiguredModelInputContent)
	content.SetText(nestedCheckpoint)
	request := extensionpb.ConfiguredModelRequest_builder{
		Context: nil,
		Selection: extensionpb.ModelSelection_builder{
			ProviderId: new(descriptor.GetProviderId()),
			ModelId:    new(descriptor.GetModelId()), ReasoningChoice: new(reasoningChoice),
		}.Build(),
		Instructions: new(
			"",
		),
		Messages: []*extensionpb.ConfiguredModelMessage{extensionpb.ConfiguredModelMessage_builder{
			Role: new(
				extensionpb.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_USER,
			),
			Content: []*extensionpb.ConfiguredModelInputContent{content},
		}.Build()},
	}.Build()
	operation, err := binding.StartConfiguredModel(ctx, request)
	if err != nil {
		return err
	}
	_, err = operation.Wait(ctx)
	return err
}

// appendPolicyBranchChange creates an original branch change through the public SDK.
func appendPolicyBranchChange(ctx context.Context, contextBinding *extensionsdk.ExtensionContext) error {
	const (
		branchChangeType = "reference-branch-change"
	)

	appendRequest := extensionpb.AppendExtensionRequest_builder{
		Context:   nil,
		EntryType: new(branchChangeType),
		Data:      []byte(`{"changed":true}`),
	}.Build()
	appendOperation, appendErr := contextBinding.StartAppendExtension(ctx, appendRequest)
	if appendErr != nil {
		return appendErr
	}
	if _, waitErr := appendOperation.Wait(ctx); waitErr != nil {
		return waitErr
	}
	return nil
}

// projectPolicyReplacement checks the complete candidate through the public projection service.
func projectPolicyReplacement(
	ctx context.Context,
	policy *extensionsdk.CompactionPolicyContext,
	candidate *extensionpb.CompactionResult,
) error {
	projection, err := policy.StartProjectReplacement(ctx, candidate)
	if err != nil {
		return fmt.Errorf("start reference projection: %w", err)
	}
	projected, err := projection.Wait(ctx)
	if err != nil {
		return fmt.Errorf("await reference projection: %w", err)
	}
	if len(projected.GetHistory()) == 0 {
		return errors.New("reference projection has no model history")
	}
	return nil
}

// requestCanceledParentAdmission obtains the public nested admission rejection.
func requestCanceledParentAdmission(ctx context.Context) (*extensionpb.HandleResponse, error) {
	binding, bindingErr := extensionsdk.ContextFrom(ctx)
	if bindingErr != nil {
		return newPolicyFailure(bindingErr), nil
	}
	operation, admissionErr := binding.StartGetModels(ctx)
	if admissionErr == nil {
		_, admissionErr = operation.Wait(ctx)
	}
	return newPolicyFailure(admissionErr), nil
}

// handlePolicySelectionRejection handles nested rejection for the selected fixture guidance.
func handlePolicySelectionRejection(
	ctx context.Context,
	binding *extensionsdk.ExtensionContext,
	guidance, providerID string,
) error {
	const selectionRejectionGuidance = "handled-selection-rejection"
	const selectionRejectionObserverFailureGuidance = "handled-selection-rejection-observer-failure"
	switch guidance {
	case selectionRejectionGuidance, selectionRejectionObserverFailureGuidance:
	default:
		return nil
	}
	return handleMissingModelSelection(ctx, binding, providerID)
}
