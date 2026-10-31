package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/authentication"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/errtree"
	"github.com/n-r-w/glyph/internal/operation"
)

const (
	// selectionCodeNotFound identifies an absent configured model.
	selectionCodeNotFound = "not_found"
	// selectionCodeReasoning identifies an unsupported configured reasoning choice.
	selectionCodeReasoning = "reasoning_unsupported"
	// selectionCodeProviderAuth identifies unavailable provider credentials.
	selectionCodeProviderAuth = "credential_unavailable"
	// selectionCodeBusy identifies occupied shared selection admission.
	selectionCodeBusy = "busy"
	// selectionCodeModelUnavailable identifies an invalid final target.
	selectionCodeModelUnavailable = "model_unavailable"
	// selectionCodeExtensionRejected identifies an explicit handler rejection.
	selectionCodeExtensionRejected = "extension_rejected"
	// selectionCodeExtensionFailed identifies a failed selected participant.
	selectionCodeExtensionFailed = "extension_failed"
	// selectionCodeExtensionUnavailable identifies selected runtime loss.
	selectionCodeExtensionUnavailable = "extension_unavailable"
	// compactionRunningStage identifies active manual compaction work.
	compactionRunningStage = "running"
)

// PreparationError reports a request that did not create a Host UI operation.
type PreparationError struct {
	// code is the stable public rejection category.
	code string
	// cause preserves complete rejection text and cause.
	cause error
}

var _ controllerui.PreparationFailure = (*PreparationError)(nil)

// Error returns complete rejection text.
func (e *PreparationError) Error() string { return e.cause.Error() }

// PreparationCode returns the stable rejection category.
func (e *PreparationError) PreparationCode() string { return e.code }

// Unwrap returns the original rejection cause.
func (e *PreparationError) Unwrap() error { return e.cause }

// rejectOperation constructs one classified preparation error.
func rejectOperation(code string, cause error) error {
	return &PreparationError{code: code, cause: cause}
}

// preparedUIOperation owns one admitted Host UI operation and its release action.
type preparedUIOperation struct {
	// run executes admitted work and returns its completed payload.
	run func(context.Context, operation.Reporter[controllerui.Frame]) (controllerui.Frame, error)
	// failureCode classifies accepted-operation failures.
	failureCode func(error) string
	// release frees all admission reservations once.
	release func()
	// releaseOnce limits reservation cleanup to one call.
	releaseOnce sync.Once
}

var _ operation.Prepared[controllerui.Frame, controllerui.Frame] = (*preparedUIOperation)(nil)

// Run executes admitted work and maps its terminal state.
func (prepared *preparedUIOperation) Run(
	ctx context.Context,
	reporter operation.Reporter[controllerui.Frame],
) operation.Outcome[controllerui.Frame] {
	result, err := prepared.run(ctx, reporter)
	if err == nil {
		return operation.Completed(result)
	}
	if navigation, present := result.TreeNavigation.Get(); present && navigation.Committed.IsSome() {
		if ctx.Err() != nil && isPureCancellation(err) {
			return operation.CompletedWithSource(result, err)
		}
		return operation.FailedWithResult(prepared.failureCode(err), err, result)
	}
	if committed, ok := errors.AsType[*committedSelectionError](err); ok {
		if isPureCancellation(committed.Unwrap()) {
			return operation.CompletedWithSource(result, committed.Unwrap())
		}
		return operation.FailedWithResult(prepared.failureCode(committed.Unwrap()), committed.Unwrap(), result)
	}
	if committed, ok := errors.AsType[*committedCompactionError](err); ok {
		if ctx.Err() != nil && errtree.IsCurrentCancellation(committed.Unwrap(), func(cause error) bool {
			return errors.Is(cause, ctx.Err()) || errors.Is(cause, context.Cause(ctx))
		}) {
			return operation.CompletedWithSource(result, committed.Unwrap())
		}
		return operation.FailedWithResult(prepared.failureCode(committed.Unwrap()), committed.Unwrap(), result)
	}
	if isPureCancellation(err) {
		if !errtree.IsPlainCancellation(err, func(cause error) bool {
			return errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded)
		}) {
			return operation.CanceledWithSource[controllerui.Frame](err)
		}
		return operation.Canceled[controllerui.Frame]()
	}
	return operation.Failed[controllerui.Frame](prepared.failureCode(err), err)
}

// retryPolicy projects one atomic runtime-control snapshot into the UI contract.
func retryPolicy(control RetryControl) controllerui.RetryPolicy {
	enabled, maxRetries, delays, maxProviderDelay := control.RetryPolicy()
	return controllerui.RetryPolicy{
		Enabled: enabled, MaxRetries: maxRetries, Delays: slices.Clone(delays), MaxProviderDelay: maxProviderDelay,
	}
}

// isPureCancellation checks current causes for cancellation markers recognized by Host UI.
func isPureCancellation(err error) bool {
	return errtree.IsCurrentCancellation(err, func(cause error) bool {
		return errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded)
	})
}

// Release frees operation admission exactly once.
func (prepared *preparedUIOperation) Release() { prepared.releaseOnce.Do(prepared.release) }

// Initialize sends startup state before any runtime activation or command execution.
func (s *Session) Initialize(ctx context.Context) error {
	info, _ := s.activeSessions.ActiveInformation()
	initialization := Initialization{
		Availability:   AvailabilityCheckingAuthentication,
		Models:         s.modelCatalog.Models(),
		ModelSelection: mo.Some(s.modelCatalog.ActiveSelection()),
		SessionInfo:    info,
		RetryPolicy:    retryPolicy(s.retryControl),
	}
	if err := s.output.Initialize(ctx, initialization); err != nil {
		return fmt.Errorf("send UI initialization: %w", err)
	}
	return nil
}

// Activate starts readiness work after the controller attaches output and its failure owner.
func (s *Session) Activate(ctx context.Context) func() {
	authenticationContext, cancelAuthentication := context.WithCancelCause(ctx)
	var authenticationWork sync.WaitGroup
	s.runtime.Activate(ctx)
	authenticationWork.Go(func() { s.checkOperationAuthentication(authenticationContext) })
	return func() {
		cancelAuthentication(context.Canceled)
		authenticationWork.Wait()
		s.runtime.StopReporting()
	}
}

// checkOperationAuthentication resolves startup readiness outside request receipt.
func (s *Session) checkOperationAuthentication(ctx context.Context) {
	err := s.authenticator.CheckAuthentication(ctx)
	if err != nil && isPureCancellation(err) {
		return
	}
	availability := AvailabilityIdle
	if err != nil {
		availability = AvailabilityAuthenticationFailed
		code := controllerui.FailureCodeInternal
		if s.authenticator.IsSignInRequired(err) {
			code = controllerui.FailureCodeAuthentication
		}
		// Output.ReportError routes writer failures to the connection failure owner.
		// This worker has no result channel.
		_ = s.output.ReportError(code, err)
	}
	s.setOperationAvailability(availability)
	// Output.SetAvailability routes writer failures to the connection failure owner.
	// This worker has no result channel.
	_ = s.output.SetAvailability(availability)
}

// Prepare performs bounded validation and admission for one UI operation.
func (s *Session) Prepare(
	ctx context.Context,
	command controllerui.Command,
) (operation.Prepared[controllerui.Frame, controllerui.Frame], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if command.OperationID == "" {
		return nil, rejectOperation(
			controllerui.RejectionCodeInvalidArgument,
			errors.New("UI operation identifier is required"),
		)
	}
	if command.Kind == controllerui.CommandSubmit {
		return s.prepareSubmit(command)
	}
	if command.Kind == controllerui.CommandRetryAuthentication {
		return s.prepareAuthentication(command.AuthenticationMethod)
	}
	if command.Kind == controllerui.CommandCompact {
		return s.prepareCompaction(command)
	}
	if command.Kind == controllerui.CommandSetRetryEnabled {
		enabled, present := command.RetryEnabled.Get()
		if !present {
			return nil, rejectOperation(
				controllerui.RejectionCodeInvalidArgument, errors.New("retry enablement is required"),
			)
		}
		return &preparedUIOperation{
			run: func(context.Context, operation.Reporter[controllerui.Frame]) (controllerui.Frame, error) {
				s.retryControl.SetRetryEnabled(enabled)
				frame := controllerui.NewFrame(controllerui.FrameRetryEnabled)
				frame.RetryPolicy = mo.Some(retryPolicy(s.retryControl))
				return frame, nil
			},
			failureCode: func(error) string { return controllerui.FailureCodeInternal },
			release:     func() {}, releaseOnce: sync.Once{},
		}, nil
	}
	if command.Kind == controllerui.CommandSelectModel || command.Kind == controllerui.CommandSelectReasoningChoice {
		return s.prepareSelection(command)
	}
	return s.prepareSessionOperation(command)
}

// prepareCompaction reserves the operation gate and builds one asynchronous manual compaction.
func (s *Session) prepareCompaction(
	command controllerui.Command,
) (operation.Prepared[controllerui.Frame, controllerui.Frame], error) {
	if s.compactor == nil {
		return nil, rejectOperation(controllerui.RejectionCodeNotReady, errors.New("UI compactor is unavailable"))
	}
	release, acquired := s.gate.TryAcquire()
	if !acquired {
		return nil, rejectOperation(controllerui.RejectionCodeBusy, session.ErrBusy)
	}
	return &preparedUIOperation{
		run: func(ctx context.Context, reporter operation.Reporter[controllerui.Frame]) (controllerui.Frame, error) {
			progress := controllerui.NewFrame(controllerui.FrameCompactionProgress)
			progress.CompactionStage = mo.Some(compactionRunningStage)
			if err := reporter.Report(progress); err != nil {
				return controllerui.Frame{}, fmt.Errorf("report compaction progress: %w", err)
			}
			result, compactionErr := s.compactor.CompactManual(ctx, command.CompactionInstructions)
			return compactionCompletionFrame(result, compactionErr)
		},
		failureCode: sessionOperationFailureCode,
		release:     release, releaseOnce: sync.Once{},
	}, nil
}

// compactionCompletionFrame maps one compaction terminal into the consumer-owned UI result.
func compactionCompletionFrame(
	result ManualCompactionResult,
	compactionErr error,
) (controllerui.Frame, error) {
	frame := controllerui.NewFrame(controllerui.FrameCompaction)
	if committed, present := result.Committed.Get(); present {
		entries, err := mapSessionEntries([]session.Entry{committed})
		if err != nil {
			return controllerui.Frame{}, errors.Join(compactionErr, err)
		}
		frame.SessionEntries = entries
	}
	frame.CompactionCanceled = mo.Some(result.Canceled)
	if compactionErr == nil {
		return frame, nil
	}
	frame.CompactionError = mo.Some(compactionErr.Error())
	frame.CompactionFailureCode = mo.Some(sessionOperationFailureCode(compactionErr))
	if result.Committed.IsSome() {
		return frame, &committedCompactionError{cause: compactionErr}
	}
	return controllerui.Frame{}, compactionErr
}

// prepareSubmit reserves the agent-run gate before acceptance.
func (s *Session) prepareSubmit(
	command controllerui.Command,
) (operation.Prepared[controllerui.Frame, controllerui.Frame], error) {
	message, messageErr := command.SubmittedMessage()
	if messageErr != nil {
		return nil, rejectOperation(controllerui.RejectionCodeInvalidArgument, messageErr)
	}
	if s.operationAvailabilitySnapshot() != AvailabilityIdle {
		return nil, rejectOperation(
			controllerui.RejectionCodeNotReady,
			errors.New("host is not ready for a user request"),
		)
	}
	runID, err := s.runner.PrepareRun()
	if err != nil {
		if errors.Is(err, session.ErrBusy) {
			return nil, rejectOperation(controllerui.RejectionCodeBusy, fmt.Errorf("prepare UI run: %w", err))
		}
		return nil, err
	}
	s.setOperationAvailability(AvailabilityRunning)
	var terminalRunErr error
	return &preparedUIOperation{
		run: func(ctx context.Context, reporter operation.Reporter[controllerui.Frame]) (controllerui.Frame, error) {
			releaseProgress := s.output.BindProgress(runID, reporter)
			defer releaseProgress()
			if deliveryErr := s.output.SetAvailability(AvailabilityRunning); deliveryErr != nil {
				return controllerui.Frame{}, fmt.Errorf("report running availability: %w", deliveryErr)
			}
			_, runErr := s.runner.RunPrepared(ctx, runID, message)
			terminalRunErr = runErr
			return controllerui.NewFrame(controllerui.FrameSubmitCompleted), runErr
		},
		failureCode: runFailureCode,
		release: func() {
			s.runner.CancelPrepared(runID)
			availability := AvailabilityIdle
			if terminalRunErr != nil && s.authenticator.IsSignInRequired(terminalRunErr) {
				availability = AvailabilityAuthenticationFailed
			}
			s.setOperationAvailability(availability)
			// Output.SetAvailability routes writer failures to the connection failure owner.
			// Release cannot return the delivery error.
			_ = s.output.SetAvailability(availability)
		},
		releaseOnce: sync.Once{},
	}, nil
}

// runFailureCode distinguishes history persistence, invalid final input, and other accepted-run failures.
func runFailureCode(err error) string {
	if errtree.HasCurrent(err, agent.ErrPersistenceUnavailable) {
		return controllerui.FailureCodePersistence
	}
	if code, found := modelExecutionFailureCode(err); found && code != controllerui.FailureCodeInternal {
		return code
	}
	for current := range errtree.IterateCurrentErrors(err) {
		//nolint:errorlint // Current-node inspection excludes categories retained only as diagnostic history.
		if classified, present := current.(errtree.CodedError); present {
			switch classified.Code() {
			case controllerui.FailureCodeExtensionFailed, controllerui.FailureCodeInputInvalid:
				return classified.Code()
			}
		}
	}
	return controllerui.FailureCodeInternal
}

// directCompactionFailureCode retains the compaction-owned category for manual operations.
func directCompactionFailureCode(err error) (string, bool) {
	failure, found := errors.AsType[interface {
		error
		CompactionFailureCode() string
	}](err)
	if !found {
		return "", false
	}
	switch failure.CompactionFailureCode() {
	case controllerui.FailureCodeCompactionFailed, controllerui.FailureCodeExtensionFailed,
		controllerui.FailureCodePersistence, controllerui.FailureCodeInternal:
		return failure.CompactionFailureCode(), true
	default:
		return "", false
	}
}

// modelExecutionFailureCode reads the closed provider-neutral category of the first current cause that has one,
// without replacing that cause; categories kept only as diagnostic history do not count.
func modelExecutionFailureCode(err error) (string, bool) {
	failure, found := errtree.FirstCurrent[interface{ FailureCode() string }](err)
	if !found {
		return "", false
	}
	switch failure.FailureCode() {
	case controllerui.FailureCodeModelFailed, controllerui.FailureCodeRetryExhausted,
		controllerui.FailureCodeRetryCanceled, controllerui.FailureCodeExtensionFailed,
		controllerui.FailureCodeRetryDelayExceeded, controllerui.FailureCodeContextLimit,
		controllerui.FailureCodeCompactionFailed, controllerui.FailureCodePersistence,
		controllerui.FailureCodeInternal:
		return failure.FailureCode(), true
	default:
		return "", false
	}
}

// prepareAuthentication reserves one interactive authentication attempt.
func (s *Session) prepareAuthentication(
	method authentication.Method,
) (operation.Prepared[controllerui.Frame, controllerui.Frame], error) {
	if s.operationAvailabilitySnapshot() != AvailabilityAuthenticationFailed {
		return nil, rejectOperation(
			controllerui.RejectionCodeNotReady,
			errors.New("authentication retry is not available"),
		)
	}
	s.setOperationAvailability(AvailabilityAuthenticating)
	authenticationSucceeded := false
	return &preparedUIOperation{
		run: func(ctx context.Context, reporter operation.Reporter[controllerui.Frame]) (controllerui.Frame, error) {
			releaseProgress := s.output.BindProgress("", reporter)
			defer releaseProgress()
			if deliveryErr := s.output.SetAvailability(AvailabilityAuthenticating); deliveryErr != nil {
				return controllerui.Frame{}, fmt.Errorf("report authentication availability: %w", deliveryErr)
			}
			err := s.authenticator.SignIn(ctx, method)
			authenticationSucceeded = err == nil
			return controllerui.NewFrame(controllerui.FrameAuthenticationCompleted), err
		},
		failureCode: func(error) string { return controllerui.FailureCodeAuthentication },
		release: func() {
			availability := AvailabilityAuthenticationFailed
			if authenticationSucceeded {
				availability = AvailabilityIdle
			}
			s.setOperationAvailability(availability)
			// Output.SetAvailability routes writer failures to the connection failure owner.
			// Release cannot return the delivery error.
			_ = s.output.SetAvailability(availability)
		},
		releaseOnce: sync.Once{},
	}, nil
}

// committedCompactionError marks a failure acquired after compaction state committed.
type committedCompactionError struct {
	// cause preserves the complete post-commit failure.
	cause error
}

// Error returns complete post-commit failure text.
func (e *committedCompactionError) Error() string { return e.cause.Error() }

// Unwrap returns the post-commit failure cause.
func (e *committedCompactionError) Unwrap() error { return e.cause }

// committedSelectionError marks diagnostics acquired after selection state committed.
type committedSelectionError struct {
	// cause preserves the complete post-commit diagnostic.
	cause error
}

// Error returns complete post-commit diagnostic text.
func (e *committedSelectionError) Error() string { return e.cause.Error() }

// Unwrap returns the post-commit diagnostic cause.
func (e *committedSelectionError) Unwrap() error { return e.cause }

// prepareSelection validates readiness and delegates shared admission and execution.
func (s *Session) prepareSelection(
	command controllerui.Command,
) (operation.Prepared[controllerui.Frame, controllerui.Frame], error) {
	if availability := s.operationAvailabilitySnapshot(); availability == AvailabilityCheckingAuthentication ||
		availability == AvailabilityAuthenticating {
		return nil, rejectOperation(controllerui.RejectionCodeNotReady, errors.New("model selection is not ready"))
	}
	selectionCommand := ModelSelectionCommand{
		Kind: ModelSelectionCommandReasoning, Provider: "", Model: "", ReasoningChoice: "",
	}
	if command.Kind == controllerui.CommandSelectModel {
		providerID, providerPresent := command.ProviderID.Get()
		modelID, modelPresent := command.ModelID.Get()
		if !providerPresent || providerID == "" || !modelPresent || modelID == "" {
			return nil, rejectOperation(
				controllerui.RejectionCodeInvalidArgument,
				errors.New("provider and model are required"),
			)
		}
		selectionCommand = ModelSelectionCommand{
			Kind: ModelSelectionCommandModel, Provider: model.ProviderID(providerID), Model: model.ID(modelID),
			ReasoningChoice: "",
		}
	} else {
		choice, validationErr := command.SelectedReasoningChoice()
		if validationErr != nil {
			return nil, rejectOperation(controllerui.RejectionCodeInvalidArgument, validationErr)
		}
		selectionCommand.ReasoningChoice = choice
	}
	prepared, err := s.modelSelection.PrepareUISelection(selectionCommand)
	if err != nil {
		return nil, selectionPreparationError(err)
	}
	return &preparedUIOperation{
		run: func(ctx context.Context, _ operation.Reporter[controllerui.Frame]) (controllerui.Frame, error) {
			result := prepared.Run(ctx)
			frame := modelSelectionChangedFrame(result.Selection)
			frame.SelectionIssues = projectModelSelectionIssues(result.Issues)
			if result.Committed && result.Source != nil {
				return frame, &committedSelectionError{cause: result.Source}
			}
			return frame, result.Source
		},
		failureCode: selectionFailureCode,
		release:     prepared.Release, releaseOnce: sync.Once{},
	}, nil
}

// selectionPreparationError projects shared admission and starting-target failures.
func selectionPreparationError(err error) error {
	failure, ok := errors.AsType[SelectionFailure](err)
	if !ok {
		return err
	}
	switch failure.ModelSelectionCode() {
	case selectionCodeBusy:
		return rejectOperation(controllerui.RejectionCodeBusy, err)
	case selectionCodeNotFound:
		return rejectOperation(controllerui.RejectionCodeNotFound, err)
	case selectionCodeReasoning:
		return rejectOperation(controllerui.RejectionCodeReasoningUnsupported, err)
	default:
		return err
	}
}

// selectionFailureCode classifies accepted selection failures.
func selectionFailureCode(err error) string {
	failure, ok := errors.AsType[SelectionFailure](err)
	if !ok {
		return controllerui.FailureCodeInternal
	}
	switch failure.ModelSelectionCode() {
	case selectionCodeModelUnavailable:
		return controllerui.FailureCodeModelUnavailable
	case selectionCodeProviderAuth:
		return controllerui.FailureCodeProviderAuth
	case selectionCodeExtensionRejected:
		return controllerui.FailureCodeExtensionRejected
	case selectionCodeExtensionFailed:
		return controllerui.FailureCodeExtensionFailed
	case selectionCodeExtensionUnavailable:
		return controllerui.FailureCodeExtension
	default:
		return controllerui.FailureCodeInternal
	}
}

// prepareSessionOperation validates fields and reserves the session-mutation gate when required.
func (s *Session) prepareSessionOperation(
	command controllerui.Command,
) (operation.Prepared[controllerui.Frame, controllerui.Frame], error) {
	if s.operationAvailabilitySnapshot() == AvailabilityCheckingAuthentication ||
		s.operationAvailabilitySnapshot() == AvailabilityAuthenticating {
		return nil, rejectOperation(controllerui.RejectionCodeNotReady, errors.New("host UI is not ready"))
	}
	// Readiness rejection takes precedence over session command-field errors.
	if err := command.ValidateSession(); err != nil {
		return nil, rejectOperation(controllerui.RejectionCodeInvalidArgument, err)
	}
	release := func() {}
	if isUISessionMutation(command.Kind) {
		var acquired bool
		release, acquired = s.gate.TryAcquire()
		if !acquired {
			return nil, rejectOperation(
				controllerui.RejectionCodeBusy,
				errors.New("Session replacement is unavailable: another operation is active"),
			)
		}
	}
	return &preparedUIOperation{
		run: func(ctx context.Context, reporter operation.Reporter[controllerui.Frame]) (controllerui.Frame, error) {
			return s.runSessionOperation(ctx, command, reporter)
		},
		failureCode: sessionOperationFailureCode,
		release:     release, releaseOnce: sync.Once{},
	}, nil
}

// isUISessionMutation reports operation kinds that reserve the shared mutation gate.
func isUISessionMutation(kind controllerui.CommandKind) bool {
	//nolint:exhaustive // Retry variants are handled by their owning path before this partial switch.
	switch kind {
	case controllerui.CommandCreateSession, controllerui.CommandResumeSession, controllerui.CommandSetSessionName,
		controllerui.CommandNavigateSessionTree, controllerui.CommandForkSession, controllerui.CommandCloneSession,
		controllerui.CommandSetEntryLabel:
		return true
	case controllerui.CommandListSessions, controllerui.CommandGetSessionInfo, controllerui.CommandGetSessionTree,
		controllerui.CommandSubmit, controllerui.CommandRetryAuthentication, controllerui.CommandSelectModel,
		controllerui.CommandSelectReasoningChoice:
		return false
	default:
		return false
	}
}

// runSessionOperation executes admitted session work and returns one complete result frame.
//
//nolint:gocyclo // The closed operation union maps directly to distinct domain calls.
func (s *Session) runSessionOperation(
	ctx context.Context,
	command controllerui.Command,
	reporter operation.Reporter[controllerui.Frame],
) (controllerui.Frame, error) {
	//nolint:exhaustive // Retry variants are handled by their owning path before this partial switch.
	switch command.Kind {
	case controllerui.CommandCreateSession:
		info, entries, err := s.activeSessions.CreateActive()
		if err != nil {
			return controllerui.Frame{}, err
		}
		return sessionChangedFrame(info, entries)
	case controllerui.CommandListSessions:
		listed, err := s.activeSessions.ListUISessions(ctx)
		return sessionListFrame(listed), err
	case controllerui.CommandResumeSession:
		id, present := command.SessionID.Get()
		if !present {
			return controllerui.Frame{}, errors.New("session identifier is required")
		}
		info, entries, err := s.activeSessions.ResumeActive(ctx, session.ID(id))
		if err != nil {
			return controllerui.Frame{}, err
		}
		return sessionChangedFrame(info, entries)
	case controllerui.CommandSetSessionName:
		name, present := command.SessionName.Get()
		if !present {
			return controllerui.Frame{}, errors.New("session name is required")
		}
		if _, err := s.activeSessions.SetActiveName(ctx, name); err != nil {
			return controllerui.Frame{}, err
		}
		info, statistics := s.activeSessions.ActiveInformation()
		return sessionInformationFrame(info, statistics), nil
	case controllerui.CommandGetSessionInfo:
		info, statistics := s.activeSessions.ActiveInformation()
		return sessionInformationFrame(info, statistics), nil
	case controllerui.CommandGetSessionTree:
		tree, err := s.activeSessions.Tree()
		if err != nil {
			return controllerui.Frame{}, fmt.Errorf("session tree is unavailable: %w", err)
		}
		return sessionTreeFrame(tree)
	case controllerui.CommandNavigateSessionTree:
		target, present := command.TargetEntryID.Get()
		if !present {
			return controllerui.Frame{}, errors.New("tree navigation request is invalid")
		}
		result, err := s.navigator.NavigateUI(ctx, NavigationIntent{
			TargetEntryID: target,
			SummaryMode:   command.SummaryMode,
			CustomFocus:   command.CustomFocus,
		}, navigationProgressCallback(reporter), navigationRetryProgressCallback(reporter))
		if err != nil && result.Committed.IsNone() {
			return controllerui.Frame{}, err
		}
		frame, mapErr := result.frame()
		return frame, errors.Join(err, mapErr)
	case controllerui.CommandForkSession:
		target, present := command.TargetEntryID.Get()
		if !present {
			return controllerui.Frame{}, errors.New("fork target is required")
		}
		info, entries, nextInput, err := s.activeSessions.ForkActive(ctx, target)
		if err != nil {
			return controllerui.Frame{}, err
		}
		frame, err := sessionChangedFrame(info, entries)
		frame.Kind, frame.NextInput = controllerui.FrameSessionForked, mo.Some(nextInput)
		return frame, err
	case controllerui.CommandCloneSession:
		info, entries, err := s.activeSessions.CloneActive(ctx)
		if err != nil {
			return controllerui.Frame{}, err
		}
		frame, err := sessionChangedFrame(info, entries)
		frame.Kind = controllerui.FrameSessionCloned
		return frame, err
	case controllerui.CommandSetEntryLabel:
		target, targetPresent := command.TargetEntryID.Get()
		label, labelPresent := command.EntryLabel.Get()
		if !targetPresent || !labelPresent {
			return controllerui.Frame{}, errors.New("entry label request is incomplete")
		}
		tree, err := s.activeSessions.SetLabel(ctx, target, label)
		if err != nil {
			return controllerui.Frame{}, err
		}
		frame, err := sessionTreeFrame(tree)
		frame.Kind = controllerui.FrameEntryLabelSet
		return frame, err
	case controllerui.CommandSubmit,
		controllerui.CommandRetryAuthentication,
		controllerui.CommandSelectModel,
		controllerui.CommandSelectReasoningChoice:
		return controllerui.Frame{}, errors.New("run UI session operation: invalid operation kind")
	default:
		return controllerui.Frame{}, errors.New("run UI session operation: unknown operation kind")
	}
}

// sessionOperationFailureCode classifies accepted session operation errors.
func sessionOperationFailureCode(err error) string {
	if errors.Is(err, session.ErrPersistenceUnavailable) {
		return controllerui.FailureCodePersistence
	}
	if code, found := directCompactionFailureCode(err); found {
		return code
	}
	if code := runFailureCode(err); code == controllerui.FailureCodeExtensionFailed {
		return code
	}
	if code, found := modelExecutionFailureCode(err); found {
		return code
	}
	switch {
	case navigationFailureCode(err) == controllerui.FailureCodeModelUnavailable:
		return controllerui.FailureCodeModelUnavailable
	case navigationFailureCode(err) == controllerui.FailureCodeProviderAuth:
		return controllerui.FailureCodeProviderAuth
	case navigationFailureCode(err) == controllerui.FailureCodeModelFailed:
		return controllerui.FailureCodeModelFailed
	case navigationFailureCode(err) == controllerui.FailureCodeExtensionInvalid:
		return controllerui.FailureCodeExtensionInvalid
	case navigationFailureCode(err) == controllerui.FailureCodeExtension:
		return controllerui.FailureCodeExtension
	case errors.Is(err, session.ErrEntryNotFound):
		return controllerui.FailureCodeSession
	default:
		return controllerui.FailureCodeInternal
	}
}

// operationAvailabilitySnapshot returns current operation-mode admission state.
func (s *Session) operationAvailabilitySnapshot() Availability {
	s.operationMutex.Lock()
	defer s.operationMutex.Unlock()
	return s.operationAvailability
}

// setOperationAvailability updates operation-mode admission state.
func (s *Session) setOperationAvailability(availability Availability) {
	s.operationMutex.Lock()
	s.operationAvailability = availability
	s.operationMutex.Unlock()
}

// navigationFailureCode reads a source category without depending on the navigation implementation.
func navigationFailureCode(err error) string {
	if failure, ok := errors.AsType[NavigationFailure](err); ok {
		return failure.NavigationCode()
	}
	return ""
}
