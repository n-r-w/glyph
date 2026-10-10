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
	"sync"

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
		// disabledPolicyEnvironment selects an observer-only fixture.
		disabledPolicyEnvironment = "GLYPH_REFERENCE_POLICY_DISABLED"
		// callerSocketEnvironment selects public Extension caller coordination.
		callerSocketEnvironment = "GLYPH_REFERENCE_CALLER_SOCKET"
		// callerExecutable identifies the ordinary public Extension caller.
		callerExecutable = "reference-caller"
	)

	// service owns fixture capabilities and the retained caller binding.
	service := &policyService{
		workerStart: sync.Once{}, listener: nil,
		disabled: os.Getenv(disabledPolicyEnvironment) == fixtureEnabledValue ||
			(os.Getenv(separateObserverEnvironment) == fixtureEnabledValue && filepath.Base(os.Args[0]) != policyHandlerID),
		observe: os.Getenv(callerSocketEnvironment) != "" &&
			(os.Getenv(separateObserverEnvironment) != fixtureEnabledValue || filepath.Base(os.Args[0]) == callerExecutable),
	}
	if service.observe {
		// listenConfig supplies caller socket options.
		listenConfig := &net.ListenConfig{}
		// listener owns the fixture coordination socket.
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
	return &policyRegistration{disabled: s.disabled, observe: s.observe}, nil
}

// PrepareHandle admits the reference extension's handler operations.
func (s *policyService) PrepareHandle(
	ctx context.Context,
	request *extensionpb.HandleRequest,
) (extensionsdk.HandleOperation, error) {
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
	// policy contains invocation-scoped replacement authority.
	policy, present := extensionsdk.FindCompactionPolicy(ctx)
	if !present {
		return nil, extensionsdk.Reject(invalidArgumentCode, errors.New("reference policy authority is required"))
	}
	return &policyOperation{policy: policy, state: nil}, nil
}

// PrepareExecute rejects tool execution for this policy-only extension.
func (s *policyService) PrepareExecute(
	_ context.Context,
	_ *extensionpb.ExecuteRequest,
) (extensionsdk.ExecuteOperation, error) {
	return nil, extensionsdk.Reject(invalidArgumentCode, errors.New("reference policy has no tools"))
}

// Run publishes the complete reference capability catalog.
func (r *policyRegistration) Run(_ context.Context) (*extensionpb.RegisterResponse, error) {
	const (
		// settledHandlerID identifies the session-context observer.
		settledHandlerID = "reference-settled"
		// successHandlerID identifies the committed outcome observer.
		successHandlerID = "reference-compaction-success"
		// failureHandlerID identifies the failed outcome observer.
		failureHandlerID = "reference-compaction-failure"
		// observerExecutable identifies the outcome-only process.
		observerExecutable = "reference-observer"
	)

	// handlers contains the enabled fixture capabilities.
	handlers := make([]*extensionpb.HandlerDescriptor, 0)
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
	return extensionpb.RegisterResponse_builder{Tools: nil, Handlers: handlers}.Build(), nil
}

// Release completes registration admission.
func (r *policyRegistration) Release() {}

// Run executes one reference manual policy invocation.
func (o *policyOperation) Run(ctx context.Context) (*extensionpb.HandleResponse, error) {
	const (
		// referenceSummary identifies fixture replacement content.
		referenceSummary = "Reference policy replacement summary."
		// cancelGuidance selects explicit pre-mutation cancellation.
		cancelGuidance = "cancel-before-commit"
		// staleGuidance selects an active-branch change before replacement.
		staleGuidance = "change-branch-before-commit"
		// waitGuidance selects caller-owned cancellation with active nested model work.
		waitGuidance = "wait-for-caller-cancel"
	)

	// input contains captured policy invocation data.
	input := o.policy.Input()
	// admissionProbeGuidance selects a nested request after owner cancellation.
	const admissionProbeGuidance = "request-after-parent-cancellation"
	if input.GetUserInstructions() == admissionProbeGuidance {
		return requestCanceledParentAdmission(ctx)
	}
	// policyInputEnvironment identifies the optional captured-input artifact path.
	const policyInputEnvironment = "GLYPH_REFERENCE_POLICY_INPUT"
	if err := writePolicyArtifact(os.Getenv(policyInputEnvironment), input); err != nil {
		return o.attachAcquiredState(newPolicyFailure(err)), nil
	}
	if input.GetUserInstructions() == cancelGuidance {
		// response contains the acquired terminal completion.
		response := new(extensionpb.HandleResponse)
		response.SetCompactionPolicy(
			extensionpb.CompactionPolicyResult_builder{State: nil, Error: nil, Canceled: new(true)}.Build(),
		)
		return o.attachAcquiredState(response), nil
	}
	// boundary identifies the first retained original entry.
	boundary := findPolicyBoundary(input.GetEntries())
	if boundary == "" {
		return o.attachAcquiredState(
			newPolicyFailure(errors.New("reference policy requires an original user entry")),
		), nil
	}
	// contextBinding supplies ordinary session operations.
	contextBinding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return o.attachAcquiredState(newPolicyFailure(err)), nil
	}
	if input.GetUserInstructions() == waitGuidance {
		if modelErr := waitPolicyConfiguredModel(
			ctx,
			contextBinding,
			input.GetModel(),
			input.GetReasoningChoice(),
		); modelErr != nil {
			return o.attachAcquiredState(newPolicyFailure(modelErr)), nil
		}
	}
	// source identifies the extension that supplies the summary.
	source := new(extensionpb.BranchSummarySource)
	source.SetExtensionId(contextBinding.Identity().GetExtensionId())
	if input.GetUserInstructions() == staleGuidance {
		if appendErr := appendPolicyBranchChange(ctx, contextBinding); appendErr != nil {
			return o.attachAcquiredState(newPolicyFailure(appendErr)), nil
		}
	}
	// candidate contains the proposed replacement.
	candidate := extensionpb.CompactionResult_builder{
		Summary:          new(referenceSummary),
		FirstKeptEntryId: new(boundary),
		Source:           source,
		Details:          nil,
	}.Build()
	o.state = newReferencePolicyState(input, candidate)
	if projectionErr := projectPolicyReplacement(ctx, o.policy, candidate); projectionErr != nil {
		return o.attachAcquiredState(newPolicyFailure(projectionErr)), nil
	}
	// replacement owns the pending durable replacement.
	replacement, err := o.policy.StartReplaceHistory(ctx, candidate)
	if err != nil {
		return o.attachAcquiredState(newPolicyFailure(fmt.Errorf("start reference replacement: %w", err))), nil
	}
	// receipt contains the durable replacement outcome.
	receipt, err := replacement.Wait(ctx)
	if err != nil {
		return o.attachAcquiredState(newPolicyFailure(fmt.Errorf("await reference replacement: %w", err))), nil
	}
	if receipt.GetError() != "" {
		return o.attachAcquiredState(newPolicyFailure(errors.New(receipt.GetError()))), nil
	}
	return o.completeReplacement(input.GetUserInstructions()), nil
}

// completeReplacement supplies the acquired policy completion after persistence.
func (o *policyOperation) completeReplacement(guidance string) *extensionpb.HandleResponse {
	const (
		// unavailableObserverGuidance selects loss of a shared policy and observer process.
		unavailableObserverGuidance = "lose-shared-owner-after-commit"
		// invalidStateGuidance selects invalid diagnostic state with a received policy error.
		invalidStateGuidance = "invalid-state-after-commit"
		// invalidStateCause identifies the original acquired policy error.
		invalidStateCause = "reference invalid diagnostic producer: original policy failure cause"
		// lostStateGuidance selects result loss after a durable replacement.
		lostStateGuidance = "lose-state-after-commit"
		// lostStateExit identifies deliberate loss of the policy process after persistence.
		lostStateExit = 23
		// failureGuidance selects a complete-result policy failure after persistence.
		failureGuidance = "fail-after-commit"
		// failureCause supplies a complete original policy failure.
		failureCause = "reference policy failure after durable replacement: original producer cause"
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
	// response contains the acquired terminal completion.
	response := new(extensionpb.HandleResponse)
	response.SetCompactionPolicy(
		extensionpb.CompactionPolicyResult_builder{State: nil, Error: nil, Canceled: new(false)}.Build(),
	)
	return o.attachAcquiredState(response)
}

// attachAcquiredState supplies diagnostic values and policy failure in one completion.
func (o *policyOperation) attachAcquiredState(response *extensionpb.HandleResponse) *extensionpb.HandleResponse {
	if o.state == nil {
		return response
	}
	// failure contains the complete policy cause.
	failure := response.GetError().GetMessage()
	// result contains policy diagnostics and terminal status.
	result := response.GetCompactionPolicy()
	if result == nil {
		result = extensionpb.CompactionPolicyResult_builder{State: nil, Error: nil, Canceled: new(false)}.Build()
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
	// response contains the acquired terminal completion.
	response := new(extensionpb.HandleResponse)
	response.SetError(extensionpb.HandlerError_builder{Message: new(err.Error())}.Build())
	return response
}

// findPolicyBoundary selects the most recent original user entry.
func findPolicyBoundary(entries []*extensionpb.CompactionSnapshotEntry) string {
	// boundary identifies the first retained original entry.
	boundary := ""
	for _, snapshotEntry := range slices.Backward(entries) {
		// entry contains the original branch record.
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
		// binding supplies ordinary session access to the detached caller.
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
	// response acknowledges session settlement.
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
		// nestedCheckpoint identifies the pending nested provider request.
		nestedCheckpoint = "nested cancellation checkpoint"
	)
	// content contains nested model input.
	content := new(extensionpb.ConfiguredModelInputContent)
	content.SetText(nestedCheckpoint)
	// request contains the public operation input.
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
	// operation owns the pending public request.
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
		// branchChangeType identifies fixture-owned branch mutation.
		branchChangeType = "reference-branch-change"
	)

	// appendRequest contains the fixture branch mutation.
	appendRequest := extensionpb.AppendExtensionRequest_builder{
		Context:   nil,
		EntryType: new(branchChangeType),
		Data:      []byte(`{"changed":true}`),
	}.Build()
	// appendOperation owns the pending branch mutation.
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
	// projection owns the pending candidate projection.
	projection, err := policy.StartProjectReplacement(ctx, candidate)
	if err != nil {
		return fmt.Errorf("start reference projection: %w", err)
	}
	// projected contains the public projection result.
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
	// binding and bindingErr contain the caller session context and lookup failure.
	binding, bindingErr := extensionsdk.ContextFrom(ctx)
	if bindingErr != nil {
		return newPolicyFailure(bindingErr), nil
	}
	// operation and admissionErr contain the nested request handle and admission failure.
	operation, admissionErr := binding.StartGetModels(ctx)
	if admissionErr == nil {
		_, admissionErr = operation.Wait(ctx)
	}
	return newPolicyFailure(admissionErr), nil
}
