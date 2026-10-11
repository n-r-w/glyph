// Package main supplies a public-only reference compaction policy.
package main

import (
	"context"
	"errors"
	"net"
	"os"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// outcomeOperation observes acquired policy state and the independent durable entry.
type outcomeOperation struct {
	// outcome contains the complete public terminal notification.
	outcome *extensionpb.CompactionOutcome
}

// The interface assertion checks the fixture capability contract.
var _ extensionsdk.HandleOperation = (*outcomeOperation)(nil)

// Run captures the public outcome and optionally performs ordinary observer work.
func (o *outcomeOperation) Run(ctx context.Context) (*extensionpb.HandleResponse, error) {
	const (
		outcomeEnvironment         = "GLYPH_REFERENCE_OUTCOME"
		observerFailureEnvironment = "GLYPH_REFERENCE_OBSERVER_FAILURE"
		observerModelEnvironment   = "GLYPH_REFERENCE_OBSERVER_MODEL"
		observerCause              = "reference compaction observer failure: original observer cause"
	)

	if _, authority := extensionsdk.FindCompactionPolicy(ctx); authority {
		return newPolicyFailure(errors.New("observer received replacement authority")), nil
	}
	if err := writePolicyArtifact(os.Getenv(outcomeEnvironment), o.outcome); err != nil {
		return newPolicyFailure(err), nil
	}
	if os.Getenv(observerModelEnvironment) == fixtureEnabledValue && o.outcome.GetState() != nil {
		binding, err := extensionsdk.ContextFrom(ctx)
		if err != nil {
			return newPolicyFailure(err), nil
		}
		request := o.outcome.GetState().GetCurrentRequest()
		if err = waitPolicyConfiguredModel(ctx, binding, request.GetModel(), request.GetReasoningChoice()); err != nil {
			return newPolicyFailure(err), nil
		}
	}
	if os.Getenv(observerFailureEnvironment) == fixtureEnabledValue {
		return newPolicyFailure(errors.New(observerCause)), nil
	}
	response := new(extensionpb.HandleResponse)
	if o.outcome.GetCommitted() != nil && o.outcome.GetError() == "" {
		response.SetCompactionSuccess(new(extensionpb.CompactionObserverAction))
	} else {
		response.SetCompactionFailure(new(extensionpb.CompactionObserverAction))
	}
	return response, nil
}

// Release completes ordinary outcome observation.
func (o *outcomeOperation) Release() {}

// newReferencePolicyState supplies the reference policy's acquired composition values.
func newReferencePolicyState(
	input *extensionpb.CompactionPolicyInvocation,
	result *extensionpb.CompactionResult,
) *extensionpb.CompactionPolicyState {
	prefix := make([]*extensionpb.CompactionInputEntry, 0)
	suffix := make([]*extensionpb.CompactionInputEntry, 0)
	// retained marks the retained boundary and subsequent entries.
	retained := false
	for _, entry := range input.GetEntries() {
		retained = retained || entry.GetEntry().GetId() == result.GetFirstKeptEntryId()
		projected := extensionpb.CompactionInputEntry_builder{
			Entry:           entry.GetEntry(),
			EstimatedTokens: new(int64(0)),
		}.Build()
		if retained {
			suffix = append(suffix, projected)
		} else {
			prefix = append(prefix, projected)
		}
	}
	request := extensionpb.CompactionRequest_builder{
		Trigger: new(
			extensionpb.CompactionTrigger_COMPACTION_TRIGGER_MANUAL,
		),
		RetryIntent:     new(false),
		Instructions:    new(input.GetUserInstructions()),
		Model:           input.GetModel(),
		ReasoningChoice: new(input.GetReasoningChoice()),
		Prefix:          prefix,
		Suffix:          suffix,
		Previous:        input.GetPrevious(),
		ContextTokens: new(
			int64(0),
		),
		ContextTokensEstimated: new(true),
		ContextWindow:          new(input.GetModel().GetContextWindow()),
		ResponseBudget:         new(input.GetModel().GetMaxTokens()),
		RetainedBudget:         new(int64(0)),
	}.Build()
	return extensionpb.CompactionPolicyState_builder{
		OriginalRequest: request,
		CurrentRequest:  request,
		Result:          result,
	}.Build()
}

// startCallerCancellation owns caller cancellation and the coordination worker.
func startCallerCancellation(
	ctx context.Context, listener net.Listener,
) (ownerContext context.Context, finish func() error) {
	// Propagate the initiating cancellation cause to pending caller work.
	owner, cancel := context.WithCancelCause(ctx)
	accepted := false
	joined := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			cancel(acceptErr)
			joined <- acceptErr
			return
		}
		accepted = true
		cancel(nil)
		joined <- connection.Close()
	}()
	return owner, func() error {
		cancel(nil)
		closeErr := listener.Close()
		workerErr := <-joined
		// A successful owning close caused an ordinary interrupted accept. An already
		// closed listener reports a close failure, so its unexpected accept cause remains.
		if !accepted && closeErr == nil && errors.Is(workerErr, net.ErrClosed) {
			workerErr = nil
		}
		return errors.Join(closeErr, workerErr)
	}
}
