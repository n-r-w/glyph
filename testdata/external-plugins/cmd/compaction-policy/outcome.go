// Package main supplies a public-only reference compaction policy.
package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/protobuf/proto"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// successObservationKind identifies a public success observer invocation.
	successObservationKind = "success"
	// failureObservationKind identifies a public failure observer invocation.
	failureObservationKind = "failure"
)

// outcomeOperation observes acquired policy state and the independent durable entry.
type outcomeOperation struct {
	// outcome contains the complete public terminal notification.
	outcome *extensionpb.CompactionOutcome
	// kind identifies the actual public observer invocation.
	kind string
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
	if path := os.Getenv(outcomeEnvironment); path != "" {
		const observationSuffix = ".calls"
		const observationMode = 0o600
		root, err := os.OpenRoot(filepath.Dir(path))
		if err != nil {
			return newPolicyFailure(err), nil
		}
		file, err := root.OpenFile(
			filepath.Base(path)+observationSuffix,
			os.O_CREATE|os.O_WRONLY|os.O_APPEND,
			observationMode,
		)
		if err != nil {
			return newPolicyFailure(errors.Join(err, root.Close())), nil
		}
		_, writeErr := file.WriteString(o.kind + "\n")
		if err = errors.Join(writeErr, file.Close(), root.Close()); err != nil {
			return newPolicyFailure(err), nil
		}
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
		if modelErr := waitPolicyConfiguredModel(
			ctx,
			binding,
			request.GetModel(),
			request.GetReasoningChoice(),
		); modelErr != nil {
			return nil, modelErr
		}
	}
	if os.Getenv(observerFailureEnvironment) == fixtureEnabledValue {
		return newPolicyFailure(errors.New(observerCause)), nil
	}
	response := new(extensionpb.HandleResponse)
	if o.outcome.GetCommitted() == nil || o.outcome.GetError() != "" {
		response.SetCompactionFailure(new(extensionpb.CompactionObserverAction))
		return response, nil
	}
	if err := appendOutcomeContext(ctx); err != nil {
		return newPolicyFailure(err), nil
	}
	response.SetCompactionSuccess(new(extensionpb.CompactionObserverAction))
	return response, nil
}

// appendOutcomeContext supplies model-visible growth after durable replacement.
func appendOutcomeContext(ctx context.Context) error {
	const (
		// automaticOutcomeAppend is context added before the outgoing capture.
		automaticOutcomeAppend = "automatic observer append"
		// automaticObserverAppendEnvironment names the environment variable for adding visible context
		// during outcome observation.
		automaticObserverAppendEnvironment = "GLYPH_REFERENCE_AUTOMATIC_APPEND"
	)
	if os.Getenv(automaticObserverAppendEnvironment) == fixtureEnabledValue {
		message := automaticOutcomeAppend
		if os.Getenv(automaticModeEnvironment) == automaticRejectFinal {
			const growthText = " observer growth "
			const growthRepeats = 5_000
			message += strings.Repeat(growthText, growthRepeats)
		}
		if err := appendAutomaticMessage(ctx, message); err != nil {
			return err
		}
	}
	if os.Getenv(conversationOverflowEnvironment) == fixtureEnabledValue {
		return appendOverflowObservationMessage(ctx)
	}
	return nil
}

// Release completes ordinary outcome observation.
func (o *outcomeOperation) Release() {}

// newReferencePolicyState supplies the reference policy's acquired composition values.
func newReferencePolicyState(
	input *extensionpb.CompactionPolicyInvocation,
	boundary string,
) *extensionpb.CompactionPolicyState {
	prefix := make([]*extensionpb.CompactionHistoryRecord, 0)
	suffix := make([]*extensionpb.CompactionHistoryRecord, 0)
	// retained marks the retained boundary and subsequent entries.
	retained := false
	for _, entry := range input.GetHistory() {
		retained = retained || entry.GetSourceEntryId() == boundary
		if entry.GetPreviousSummary() {
			continue
		}
		projected := proto.CloneOf(entry)
		projected.SetEstimatedTokens(0)
		if retained {
			suffix = append(suffix, projected)
		} else {
			prefix = append(prefix, projected)
		}
	}
	trigger := extensionpb.CompactionTrigger_COMPACTION_TRIGGER_MANUAL
	retryIntent := false
	switch input.GetPurpose() {
	case extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_UNSPECIFIED,
		extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_MANUAL:
	case extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_AUTOMATIC:
		trigger = extensionpb.CompactionTrigger_COMPACTION_TRIGGER_THRESHOLD
	case extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_OVERFLOW:
		trigger = extensionpb.CompactionTrigger_COMPACTION_TRIGGER_OVERFLOW
		retryIntent = true
	}
	request := extensionpb.CompactionRequest_builder{
		Trigger:         trigger.Enum(),
		RetryIntent:     new(retryIntent),
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
		CurrentRequest:  proto.CloneOf(request),
		Result:          nil,
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
