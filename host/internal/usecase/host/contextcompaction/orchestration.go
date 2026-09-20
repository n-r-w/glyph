package contextcompaction

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/errtree"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

const (
	// unavailableGeneratorMessage identifies an absent generation capability.
	unavailableGeneratorMessage = "compaction requires exactly one registered generation handler; found %d"
	// invalidActionMessage identifies a malformed compaction handler action.
	invalidActionMessage = "compaction handler returned an invalid action"
)

// Compact coordinates one compaction operation against a captured agent request.
//
//nolint:gocognit,gocyclo // The ordered handler state machine keeps commit and terminal identity together.
func (s *Service) Compact(
	ctx context.Context,
	trigger Trigger,
	instructions mo.Option[string],
	retryIntent bool,
	providerRequest modelexecution.ProviderRequest,
) (OperationResult, error) {
	if err := ctx.Err(); err != nil {
		return emptyOperationResult(), context.Cause(ctx)
	}
	if s.runtime == nil || s.contexts == nil {
		return emptyOperationResult(), errors.New("compaction orchestration is not bound")
	}
	snapshot := cloneSnapshot(s.sessions.CompactionSnapshot())
	handlers := cloneHandlerSet(s.runtime.SnapshotCompactionHandlers())
	original, err := s.newRequest(snapshot, providerRequest, trigger, instructions, retryIntent)
	if err != nil {
		return s.fail(ctx, handlers, original, original, mo.None[Result](), false, err)
	}
	current := cloneRequest(original)
	currentResult := mo.None[Result]()
	for _, handler := range handlers.Requests {
		binding, bindingErr := s.contexts.IssueContext(handler.ExtensionID)
		if bindingErr != nil {
			return s.fail(
				ctx,
				handlers,
				original,
				current,
				currentResult,
				false,
				extensionFailure(
					fmt.Errorf("issue context for compaction request handler %q: %w", handler.HandlerID, bindingErr),
				),
			)
		}
		action, handlerErr := s.runtime.HandleCompactionRequest(ctx, handler, binding, RequestInvocation{
			Original: cloneRequest(
				original,
			),
			Current:       cloneRequest(current),
			CurrentResult: cloneOptionalResult(currentResult),
		})
		if handlerErr != nil {
			cause := fmt.Errorf("compaction request handler %q: %w", handler.HandlerID, handlerErr)
			canceled, failure := classifyExtensionInvocation(ctx, cause)
			return s.fail(ctx, handlers, original, current, currentResult, canceled, failure)
		}
		if replacement, present := action.Result.Get(); action.ResultAction == ResultActionReplace && present {
			err = validateProducer(handler.ExtensionID, replacement)
		}
		nextRequest := cloneRequest(current)
		nextResult := cloneOptionalResult(currentResult)
		var canceled bool
		if err == nil {
			nextRequest, nextResult, canceled, err = applyRequestAction(current, currentResult, action)
		}
		if err == nil && !canceled {
			nextRequest, err = s.deriveReplacementEstimates(original, current, nextRequest)
		}
		if err == nil && !canceled {
			err = s.validateRequestState(snapshot, nextRequest)
		}
		if candidate, present := nextResult.Get(); err == nil && !canceled && present {
			err = s.validateResultState(snapshot, candidate)
		}
		if err != nil {
			return s.fail(ctx, handlers, original, current, currentResult, false,
				extensionFailure(fmt.Errorf("compaction request handler %q: %w", handler.HandlerID, err)))
		}
		if canceled {
			return s.fail(ctx, handlers, original, current, currentResult, true, context.Cause(ctx))
		}
		current, currentResult = nextRequest, nextResult
	}
	if currentResult.IsNone() {
		generated, generationErr := s.generateCompaction(ctx, snapshot, handlers, original, current)
		if generationErr != nil {
			canceled, failure := classifyExtensionInvocation(ctx, generationErr)
			return s.fail(ctx, handlers, original, current, currentResult, canceled, failure)
		}
		currentResult = mo.Some(cloneResult(generated))
	}
	originalResult, present := currentResult.Get()
	if !present {
		err = errors.New("compaction completed generation without a result")
		return s.fail(ctx, handlers, original, current, currentResult, false, err)
	}
	originalResult = cloneResult(originalResult)
	result := cloneResult(originalResult)
	for _, handler := range handlers.Results {
		binding, bindingErr := s.contexts.IssueContext(handler.ExtensionID)
		if bindingErr != nil {
			return s.fail(
				ctx,
				handlers,
				original,
				current,
				mo.Some(result),
				false,
				extensionFailure(
					fmt.Errorf("issue context for compaction result handler %q: %w", handler.HandlerID, bindingErr),
				),
			)
		}
		action, handlerErr := s.runtime.HandleCompactionResult(ctx, handler, binding, ResultInvocation{
			OriginalRequest: cloneRequest(original), CurrentRequest: cloneRequest(current),
			OriginalResult: cloneResult(originalResult), CurrentResult: cloneResult(result),
		})
		if handlerErr != nil {
			cause := fmt.Errorf("compaction result handler %q: %w", handler.HandlerID, handlerErr)
			canceled, failure := classifyExtensionInvocation(ctx, cause)
			return s.fail(ctx, handlers, original, current, mo.Some(result), canceled, failure)
		}
		if replacement, replacementPresent := action.Result.Get(); !action.Preserve && !action.Cancel &&
			replacementPresent {
			err = validateProducer(handler.ExtensionID, replacement)
		}
		var canceled bool
		if err == nil {
			result, canceled, err = applyResultAction(result, action)
		}
		if err == nil && !canceled {
			err = s.validateResultState(snapshot, result)
		}
		if err != nil {
			return s.fail(ctx, handlers, original, current, mo.Some(result), false,
				extensionFailure(fmt.Errorf("compaction result handler %q: %w", handler.HandlerID, err)))
		}
		if canceled {
			return s.fail(ctx, handlers, original, current, mo.Some(result), true, context.Cause(ctx))
		}
	}
	entry, err := s.validateFinal(snapshot, providerRequest, result)
	if err != nil {
		return s.fail(ctx, handlers, original, current, mo.Some(result), false, err)
	}
	if err = ctx.Err(); err != nil {
		return s.fail(ctx, handlers, original, current, mo.Some(result), false, context.Cause(ctx))
	}
	committed, commitErr := s.CommitCompaction(ctx, snapshot.Identity, snapshot.ActiveLeafID, entry)
	commitErr = joinContextCause(ctx, commitErr)
	if committed.ID == "" {
		if commitErr == nil {
			commitErr = errors.New("compaction commit returned no committed entry")
		}
		return s.fail(ctx, handlers, original, current, mo.Some(result), false, commitErr)
	}
	outcome := OutcomeInvocation{
		OriginalRequest: cloneRequest(
			original,
		),
		CurrentRequest: cloneRequest(current),
		Result:         mo.Some(cloneResult(result)),
		Committed:      mo.Some(committed.Clone()),
		Canceled:       false,
		Error:          mo.None[string](),
	}
	observerErr := s.observeSuccess(context.WithoutCancel(ctx), handlers.Successes, outcome)
	terminalErr := errors.Join(internalFailure(commitErr), extensionFailure(observerErr))
	return OperationResult{Committed: mo.Some(committed.Clone()), Canceled: false},
		joinContextCause(ctx, terminalErr)
}

// generateCompaction invokes and validates the unique snapshotted generation capability.
func (s *Service) generateCompaction(
	ctx context.Context,
	snapshot Snapshot,
	handlers HandlerSet,
	original Request,
	current Request,
) (Result, error) {
	if len(handlers.Generators) != 1 {
		return Result{}, extensionFailure(fmt.Errorf(unavailableGeneratorMessage, len(handlers.Generators)))
	}
	generator := handlers.Generators[0]
	binding, err := s.contexts.IssueContext(generator.ExtensionID)
	if err != nil {
		return Result{}, extensionFailure(
			fmt.Errorf("issue context for compaction generator %q: %w", generator.HandlerID, err),
		)
	}
	generated, err := s.runtime.GenerateCompaction(ctx, generator, binding, RequestInvocation{
		Original: cloneRequest(original), Current: cloneRequest(current), CurrentResult: mo.None[Result](),
	})
	if err != nil {
		return Result{}, fmt.Errorf("compaction generator %q: %w", generator.HandlerID, err)
	}
	if err = validateProducer(generator.ExtensionID, generated); err == nil {
		err = s.validateResultState(snapshot, generated)
	}
	if err != nil {
		cause := fmt.Errorf("compaction generator %q returned an invalid result: %w", generator.HandlerID, err)
		return Result{}, extensionFailure(cause)
	}
	return generated, nil
}

// newRequest creates the immutable original request and its retained-suffix proposal.
func (s *Service) newRequest(
	snapshot Snapshot,
	providerRequest modelexecution.ProviderRequest,
	trigger Trigger,
	instructions mo.Option[string],
	retryIntent bool,
) (Request, error) {
	if trigger < TriggerManual || trigger > TriggerOverflow {
		return Request{}, errors.New("compaction trigger is invalid")
	}
	if providerRequest.Model.ContextWindow <= providerRequest.Model.MaxTokens {
		return Request{}, errors.New("compaction model has no positive input budget")
	}
	contextTokens, err := s.EstimateContext(providerRequest)
	if err != nil {
		return Request{}, fmt.Errorf("estimate compaction context: %w", err)
	}
	boundary, err := s.proposedBoundary(snapshot)
	if err != nil {
		return Request{}, err
	}
	boundaryIndex := entryIndex(snapshot.Entries, boundary)
	if boundaryIndex < 0 {
		return Request{}, errors.New("compaction retained boundary is outside the active branch")
	}
	prefixStart := 0
	if previous, present := snapshot.Previous.Get(); present {
		prefixStart = entryIndex(snapshot.Entries, previous.FirstKeptEntryID)
		if prefixStart < 0 {
			return Request{}, errors.New("preceding compaction boundary is outside the active branch")
		}
	}
	prefix, err := s.newEstimatedInputEntries(snapshot.Entries[prefixStart:boundaryIndex])
	if err != nil {
		return Request{}, err
	}
	suffix, err := s.newEstimatedInputEntries(snapshot.Entries[boundaryIndex:])
	if err != nil {
		return Request{}, err
	}
	request := Request{
		Trigger:                trigger,
		RetryIntent:            retryIntent,
		Instructions:           instructions,
		Model:                  providerRequest.Model.Clone(),
		ReasoningChoice:        providerRequest.ReasoningChoice,
		Prefix:                 prefix,
		Suffix:                 suffix,
		Previous:               snapshot.Previous.MapValue(session.CompactionEntry.Clone),
		ContextTokens:          contextTokens,
		ContextTokensEstimated: true,
		ContextWindow:          providerRequest.Model.ContextWindow,
		ResponseBudget:         providerRequest.Model.MaxTokens,
		RetainedBudget:         s.retainedBudget,
	}
	return request, s.validateRequestState(snapshot, request)
}

// proposedBoundary selects the latest complete suffix that reaches the retained-context target.
func (s *Service) proposedBoundary(snapshot Snapshot) (string, error) {
	if len(snapshot.Entries) == 0 {
		return "", errors.New("active branch has no entry available for compaction")
	}
	boundary := snapshot.Entries[0].ID
	for index := range slices.Backward(snapshot.Entries) {
		if snapshot.Entries[index].Compaction.IsSome() {
			continue
		}
		candidate := snapshot.Entries[index].ID
		history, err := s.sessions.ProjectSuffix(snapshot.Entries, candidate)
		if err != nil {
			continue
		}
		estimate, err := estimateHistory(history)
		if err != nil {
			return "", fmt.Errorf("estimate retained compaction suffix: %w", err)
		}
		boundary = candidate
		if estimate >= s.retainedBudget {
			return boundary, nil
		}
	}
	return boundary, nil
}

// validateResultState checks result shape, source accounting, and retained boundary before later handlers.
func (s *Service) validateResultState(snapshot Snapshot, result Result) error {
	if strings.TrimSpace(result.Summary) == "" {
		return errors.New("compaction summary is empty")
	}
	entry := session.CompactionEntry{
		Summary: result.Summary, FirstKeptEntryID: result.FirstKeptEntryID, Source: result.Source,
		EstimatedCost: mo.None[session.EstimatedCost](), Details: result.Details.MapValue(appendBytes),
	}
	if err := entry.ValidateAccounting(); err != nil {
		return fmt.Errorf("validate compaction source accounting: %w", err)
	}
	if s.sessions.ProjectCompaction(snapshot.Entries, entry) == nil {
		return errors.New("compaction retained boundary is invalid")
	}
	return nil
}

// validateFinal checks result shape, boundary integrity, accounting, and actual post-compaction budget.
func (s *Service) validateFinal(
	snapshot Snapshot,
	providerRequest modelexecution.ProviderRequest,
	result Result,
) (session.CompactionEntry, error) {
	entry := session.CompactionEntry{
		Summary: result.Summary, FirstKeptEntryID: result.FirstKeptEntryID, Source: result.Source,
		EstimatedCost: mo.None[session.EstimatedCost](), Details: result.Details.MapValue(appendBytes),
	}
	if err := s.validateResultState(snapshot, result); err != nil {
		return session.CompactionEntry{}, err
	}
	projected := s.sessions.ProjectCompaction(snapshot.Entries, entry)
	if projected == nil {
		return session.CompactionEntry{}, errors.New("compaction retained boundary is invalid")
	}
	candidate := cloneProviderRequest(providerRequest)
	candidate.History = projected
	estimate, err := fallbackEstimate(candidate)
	if err != nil {
		return session.CompactionEntry{}, fmt.Errorf("estimate compacted context: %w", err)
	}
	inputBudget := providerRequest.Model.ContextWindow - providerRequest.Model.MaxTokens
	if estimate > inputBudget {
		return session.CompactionEntry{}, fmt.Errorf(
			"compacted context estimate %d exceeds input budget %d", estimate, inputBudget,
		)
	}
	return entry, nil
}

// fail reports one terminal pre-commit outcome and joins independent observer failures.
func (s *Service) fail(
	ctx context.Context,
	handlers HandlerSet,
	original Request,
	current Request,
	result mo.Option[Result],
	canceled bool,
	cause error,
) (OperationResult, error) {
	message := mo.None[string]()
	if cause != nil {
		message = mo.Some(cause.Error())
	}
	outcome := OutcomeInvocation{
		OriginalRequest: cloneRequest(original), CurrentRequest: cloneRequest(current),
		Result: cloneOptionalResult(result), Committed: mo.None[session.Entry](), Canceled: canceled, Error: message,
	}
	observerErr := s.observeFailure(context.WithoutCancel(ctx), handlers.Failures, outcome)
	terminalErr := errors.Join(classifyCompactionFailure(cause), extensionFailure(observerErr))
	return OperationResult{Committed: mo.None[session.Entry](), Canceled: canceled},
		joinContextCause(ctx, terminalErr)
}

// observeSuccess stops at the first success observer failure after durable commit.
func (s *Service) observeSuccess(ctx context.Context, handlers []Handler, outcome OutcomeInvocation) error {
	for _, handler := range handlers {
		binding, err := s.contexts.IssueContext(handler.ExtensionID)
		if err != nil {
			return fmt.Errorf("issue context for compaction success observer %q: %w", handler.HandlerID, err)
		}
		if err = s.runtime.ObserveCompactionSuccess(ctx, handler, binding, cloneOutcome(outcome)); err != nil {
			return fmt.Errorf("compaction success observer %q: %w", handler.HandlerID, err)
		}
	}
	return nil
}

// observeFailure stops at the first failure observer failure without replacing the initiating cause.
func (s *Service) observeFailure(ctx context.Context, handlers []Handler, outcome OutcomeInvocation) error {
	for _, handler := range handlers {
		binding, err := s.contexts.IssueContext(handler.ExtensionID)
		if err != nil {
			return fmt.Errorf("issue context for compaction failure observer %q: %w", handler.HandlerID, err)
		}
		if err = s.runtime.ObserveCompactionFailure(ctx, handler, binding, cloneOutcome(outcome)); err != nil {
			return fmt.Errorf("compaction failure observer %q: %w", handler.HandlerID, err)
		}
	}
	return nil
}

// applyRequestAction validates the complete action before exposing changed state.
//
//nolint:gocyclo // The closed request and result action product is validated explicitly.
func applyRequestAction(
	current Request,
	currentResult mo.Option[Result],
	action RequestAction,
) (Request, mo.Option[Result], bool, error) {
	if action.Cancel {
		if action.RequestAction != 0 || action.Request.IsSome() || action.ResultAction != 0 || action.Result.IsSome() {
			return current, currentResult, false, errors.New(invalidActionMessage)
		}
		return current, currentResult, true, nil
	}
	next := cloneRequest(current)
	switch action.RequestAction {
	case RequestActionPreserve:
		if action.Request.IsSome() {
			return current, currentResult, false, errors.New(invalidActionMessage)
		}
	case RequestActionReplace:
		replacement, present := action.Request.Get()
		if !present {
			return current, currentResult, false, errors.New(invalidActionMessage)
		}
		next = cloneRequest(replacement)
	default:
		return current, currentResult, false, errors.New(invalidActionMessage)
	}
	nextResult := cloneOptionalResult(currentResult)
	switch action.ResultAction {
	case ResultActionPreserve:
		if action.Result.IsSome() {
			return current, currentResult, false, errors.New(invalidActionMessage)
		}
	case ResultActionReplace:
		replacement, present := action.Result.Get()
		if !present {
			return current, currentResult, false, errors.New(invalidActionMessage)
		}
		nextResult = mo.Some(cloneResult(replacement))
	case ResultActionClear:
		if action.Result.IsSome() {
			return current, currentResult, false, errors.New(invalidActionMessage)
		}
		nextResult = mo.None[Result]()
	default:
		return current, currentResult, false, errors.New(invalidActionMessage)
	}
	return next, nextResult, false, nil
}

// applyResultAction validates one exclusive cancellation, preservation, or replacement action.
func applyResultAction(current Result, action ResultAction) (Result, bool, error) {
	if action.Cancel {
		if action.Preserve || action.Result.IsSome() {
			return current, false, errors.New(invalidActionMessage)
		}
		return current, true, nil
	}
	if action.Preserve {
		if action.Result.IsSome() {
			return current, false, errors.New(invalidActionMessage)
		}
		return current, false, nil
	}
	replacement, present := action.Result.Get()
	if !present {
		return current, false, errors.New(invalidActionMessage)
	}
	return cloneResult(replacement), false, nil
}

// validateProducer rejects an extension result that claims a different extension source.
func validateProducer(extensionID string, result Result) error {
	producer, present := result.Source.ExtensionID.Get()
	if present && producer != extensionID {
		return fmt.Errorf("compaction extension source %q does not match handler owner %q", producer, extensionID)
	}
	return nil
}

// validateRequestState checks replacement state before a later handler can observe it.
func (s *Service) validateRequestState(snapshot Snapshot, request Request) error {
	if err := validateRequestBudgets(request); err != nil {
		return err
	}
	if err := validateRequestEntries(snapshot, request); err != nil {
		return err
	}
	if err := validatePreviousCompaction(snapshot, request); err != nil {
		return err
	}
	if _, err := s.sessions.ProjectSuffix(snapshot.Entries, request.Suffix[0].ID); err != nil {
		return fmt.Errorf("validate compaction request boundary: %w", err)
	}
	return nil
}

// validateRequestBudgets checks stable trigger and token budget invariants.
func validateRequestBudgets(request Request) error {
	if request.Trigger < TriggerManual || request.Trigger > TriggerOverflow || request.ContextWindow <= 0 ||
		request.ResponseBudget <= 0 || request.ResponseBudget > request.ContextWindow || request.RetainedBudget < 0 ||
		request.ContextTokens < 0 {
		return errors.New("compaction request contains invalid budgets or trigger")
	}
	if !request.ContextTokensEstimated {
		return errors.New("compaction request context token estimate marker must be true")
	}
	if !request.Model.Valid() {
		return errors.New("compaction request contains an invalid model descriptor")
	}
	if !slices.Contains(request.Model.ReasoningCapabilities.Choices, request.ReasoningChoice) {
		return errors.New("compaction request reasoning choice is not supported by the model descriptor")
	}
	if request.ContextWindow != request.Model.ContextWindow || request.ResponseBudget > request.Model.MaxTokens {
		return errors.New("compaction request budgets conflict with the model descriptor")
	}
	return nil
}

// validatePreviousCompaction validates optional preceding state without requiring marker equality.
func validatePreviousCompaction(snapshot Snapshot, request Request) error {
	previous, present := request.Previous.Get()
	if !present {
		return nil
	}
	if strings.TrimSpace(previous.Summary) == "" {
		return errors.New("previous compaction summary is empty")
	}
	if strings.TrimSpace(previous.FirstKeptEntryID) == "" {
		return errors.New("previous compaction boundary is empty")
	}
	if err := previous.ValidateAccounting(); err != nil {
		return fmt.Errorf("validate previous compaction accounting: %w", err)
	}
	if entryIndex(snapshot.Entries, previous.FirstKeptEntryID) < 0 {
		return fmt.Errorf(
			"previous compaction boundary %q is not on the captured branch",
			previous.FirstKeptEntryID,
		)
	}
	return nil
}

// validateRequestEntries checks that a replacement only selects a new boundary in captured history.
func validateRequestEntries(snapshot Snapshot, request Request) error {
	if len(request.Suffix) == 0 || entryIndex(snapshot.Entries, request.Suffix[0].ID) < 0 {
		return errors.New("compaction request suffix has no active-branch boundary")
	}
	prefixStart := 0
	if previous, present := snapshot.Previous.Get(); present {
		prefixStart = entryIndex(snapshot.Entries, previous.FirstKeptEntryID)
		if prefixStart < 0 {
			return errors.New("preceding compaction boundary is outside the active branch")
		}
	}
	combined := append(cloneInputEntries(request.Prefix), request.Suffix...)
	expected := snapshot.Entries[prefixStart:]
	if len(combined) != len(expected) {
		return errors.New("compaction request entries do not cover the captured active branch")
	}
	for index := range expected {
		entry := combined[index].Entry
		if entry.ID != expected[index].ID {
			return errors.New("compaction request entries are reordered or replaced")
		}
		if err := validateReplacementEntryPayload(entry); err != nil {
			return fmt.Errorf("validate compaction request entry %q: %w", entry.ID, err)
		}
		if compaction, present := entry.Compaction.Get(); present &&
			entryIndex(snapshot.Entries, compaction.FirstKeptEntryID) < 0 {
			return fmt.Errorf(
				"compaction input boundary %q is not on the captured branch",
				compaction.FirstKeptEntryID,
			)
		}
	}
	return nil
}

// classifyCompactionFailure adds stable identity without replacing typed or cancellation causes.
func classifyCompactionFailure(cause error) error {
	if cause == nil {
		return nil
	}
	if _, typed := errors.AsType[*FailureError](cause); typed {
		return cause
	}
	if errors.Is(cause, session.ErrPersistenceUnavailable) {
		return &FailureError{Category: FailurePersistenceUnavailable, Cause: cause}
	}
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		return cause
	}
	return &FailureError{Category: FailureCompactionFailed, Cause: cause}
}

// classifyExtensionInvocation separates pure owner cancellation from independent extension failure.
func classifyExtensionInvocation(ctx context.Context, cause error) (bool, error) {
	if isPureOwningCancellation(ctx, cause) {
		return true, context.Cause(ctx)
	}
	return false, extensionFailure(joinContextCause(ctx, cause))
}

// isPureOwningCancellation reports whether every untyped failure leaf belongs to the canceled owner.
func isPureOwningCancellation(ctx context.Context, cause error) bool {
	if ctx.Err() == nil {
		return false
	}
	if _, typed := errors.AsType[interface {
		error
		CompactionFailureCode() string
	}](cause); typed {
		return false
	}
	owningCause := context.Cause(ctx)
	return errtree.AllLeavesMatch(cause, func(leaf error) bool {
		return errors.Is(leaf, context.Canceled) || errors.Is(leaf, context.DeadlineExceeded) ||
			errors.Is(leaf, owningCause)
	})
}

// joinContextCause retains independent caller cancellation beside an acquired operation failure.
func joinContextCause(ctx context.Context, cause error) error {
	if cause == nil {
		return nil
	}
	callerCause := context.Cause(ctx)
	if callerCause == nil || errors.Is(cause, callerCause) {
		return cause
	}
	return errors.Join(cause, callerCause)
}

// internalFailure identifies a post-commit publication failure without changing its cause.
func internalFailure(cause error) error {
	if cause == nil {
		return nil
	}
	return &FailureError{Category: FailureInternal, Cause: cause}
}

// extensionFailure adds extension identity to one handler or observer failure.
func extensionFailure(cause error) error {
	if cause == nil {
		return nil
	}
	return &FailureError{Category: FailureExtensionFailed, Cause: cause}
}

// emptyOperationResult creates one noncommitted, noncanceled result.
func emptyOperationResult() OperationResult {
	return OperationResult{Committed: mo.None[session.Entry](), Canceled: false}
}

// entryIndex finds one entry identity in a captured branch.
func entryIndex(entries []session.Entry, id string) int {
	for index := range entries {
		if entries[index].ID == id {
			return index
		}
	}
	return -1
}
