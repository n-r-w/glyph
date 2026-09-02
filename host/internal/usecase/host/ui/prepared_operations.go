package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
	"github.com/n-r-w/glyph/internal/operation"
)

const (
	rejectionCodeInvalidArgument = "INVALID_ARGUMENT"
	rejectionCodeBusy            = "BUSY"
	rejectionCodeNotReady        = "NOT_READY"
	failureCodeInternal          = "INTERNAL"
	failureCodeAuthentication    = "AUTHENTICATION_FAILED"
	failureCodeProviderAuth      = "CREDENTIAL_UNAVAILABLE"
	failureCodeSession           = "SESSION_UNAVAILABLE"
	failureCodePersistence       = "PERSISTENCE_UNAVAILABLE"
	failureCodeModelUnavailable  = "MODEL_UNAVAILABLE"
	failureCodeNotFound          = "NOT_FOUND"
	failureCodeReasoning         = "REASONING_UNSUPPORTED"
	failureCodeModelFailed       = "MODEL_FAILED"
	failureCodeExtensionInvalid  = "EXTENSION_INVALID_RESULT"
	failureCodeExtension         = "EXTENSION_UNAVAILABLE"
	selectionCodeNotFound        = "not_found"
	selectionCodeReasoning       = "reasoning_unsupported"
	selectionCodeProviderAuth    = "credential_unavailable"
)

// PreparationError reports a request that did not create a Host UI operation.
type PreparationError struct {
	// code is the stable public rejection category.
	code string
	// cause preserves complete rejection text and cause.
	cause error
}

// Error returns complete rejection text.
func (e *PreparationError) Error() string { return e.cause.Error() }

// Code returns the stable rejection category.
func (e *PreparationError) Code() string { return e.code }

// Unwrap returns the original rejection cause.
func (e *PreparationError) Unwrap() error { return e.cause }

// rejectOperation constructs one classified preparation error.
func rejectOperation(code string, cause error) error {
	return &PreparationError{code: code, cause: cause}
}

// preparedUIOperation owns one admitted Host UI operation and its release action.
type preparedUIOperation struct {
	// run executes admitted work and returns its completed payload.
	run func(context.Context, operation.Reporter[domainui.Frame]) (domainui.Frame, error)
	// failureCode classifies accepted-operation failures.
	failureCode func(error) string
	// release frees all admission reservations once.
	release func()
	// releaseOnce limits reservation cleanup to one call.
	releaseOnce sync.Once
}

var _ operation.Prepared[domainui.Frame, domainui.Frame] = (*preparedUIOperation)(nil)

// Run executes admitted work and maps its terminal state.
func (prepared *preparedUIOperation) Run(
	ctx context.Context,
	reporter operation.Reporter[domainui.Frame],
) operation.Outcome[domainui.Frame] {
	result, err := prepared.run(ctx, reporter)
	remainingErr := withoutCancellationLeaves(err)
	if err != nil && remainingErr == nil {
		return operation.Canceled[domainui.Frame]()
	}
	if remainingErr != nil {
		return operation.Failed[domainui.Frame](prepared.failureCode(remainingErr), remainingErr)
	}
	return operation.Completed(result)
}

// withoutCancellationLeaves removes only pure cancellation leaves from joined errors.
func withoutCancellationLeaves(err error) error {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		remaining := make([]error, 0, len(joined.Unwrap()))
		for _, cause := range joined.Unwrap() {
			if filtered := withoutCancellationLeaves(cause); filtered != nil {
				remaining = append(remaining, filtered)
			}
		}
		return errors.Join(remaining...)
	}
	cause := errors.Unwrap(err)
	if cause != nil {
		if !errors.Is(cause, context.Canceled) && !errors.Is(cause, context.DeadlineExceeded) {
			return err
		}
		filtered := withoutCancellationLeaves(cause)
		if filtered == nil {
			return nil
		}
		return fmt.Errorf("%s: %w", err.Error(), filtered)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	return err
}

// Release frees operation admission exactly once.
func (prepared *preparedUIOperation) Release() { prepared.releaseOnce.Do(prepared.release) }

// RunOperations initializes the UI and runs the prepared Host operation receiver.
func (s *Session) RunOperations(ctx context.Context, initialization domainui.Initialization) error {
	if err := s.channel.Initialize(ctx, initializationFrame(initialization)); err != nil {
		return fmt.Errorf("send UI initialization: %w", err)
	}
	authenticationContext, cancelAuthentication := context.WithCancelCause(ctx)
	var authenticationWork sync.WaitGroup
	runErr := s.channel.RunOperations(ctx, func() {
		s.afterInitialization(ctx)
		authenticationWork.Go(func() { s.checkOperationAuthentication(authenticationContext) })
	}, s.Prepare)
	cancelAuthentication(context.Canceled)
	authenticationWork.Wait()
	if runErr != nil {
		return fmt.Errorf("run UI operations: %w", runErr)
	}
	return nil
}

// checkOperationAuthentication resolves startup readiness outside request receipt.
func (s *Session) checkOperationAuthentication(ctx context.Context) {
	err := s.authenticator.CheckAuthentication(ctx)
	remainingErr := withoutCancellationLeaves(err)
	if err != nil && remainingErr == nil {
		return
	}
	availability := domainui.AvailabilityIdle
	if remainingErr != nil {
		availability = domainui.AvailabilityAuthenticationFailed
		code := failureCodeInternal
		if s.authenticator.IsSignInRequired(remainingErr) {
			code = failureCodeAuthentication
		}
		// Channel.Send reports delivery failure to the connection owner because this worker has no result channel.
		_ = s.channel.Send(classifiedErrorFrame(code, remainingErr.Error()))
	}
	s.setOperationAvailability(availability)
	// Channel.Send reports delivery failure to the connection owner because this worker has no result channel.
	_ = s.sendAvailability(availability)
}

// Prepare performs bounded validation and admission for one UI operation.
func (s *Session) Prepare(
	ctx context.Context,
	command domainui.Command,
) (operation.Prepared[domainui.Frame, domainui.Frame], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if command.OperationID == "" {
		return nil, rejectOperation(rejectionCodeInvalidArgument, errors.New("UI operation identifier is required"))
	}
	if command.Kind == domainui.CommandSubmit {
		return s.prepareSubmit(command)
	}
	if command.Kind == domainui.CommandRetryAuthentication {
		return s.prepareAuthentication()
	}
	if command.Kind == domainui.CommandSelectModel || command.Kind == domainui.CommandSelectReasoningChoice {
		return s.prepareSelection(command)
	}
	return s.prepareSessionOperation(command)
}

// prepareSubmit reserves the agent-run gate before acceptance.
func (s *Session) prepareSubmit(command domainui.Command) (operation.Prepared[domainui.Frame, domainui.Frame], error) {
	text, present := command.Text.Get()
	if !present || text == "" {
		return nil, rejectOperation(rejectionCodeInvalidArgument, errors.New("UI submit text is required"))
	}
	if s.operationAvailabilitySnapshot() != domainui.AvailabilityIdle {
		return nil, rejectOperation(rejectionCodeNotReady, errors.New("host is not ready for a user request"))
	}
	runID, err := s.runner.PrepareRun()
	if err != nil {
		if errors.Is(err, session.ErrBusy) {
			return nil, rejectOperation(rejectionCodeBusy, fmt.Errorf("prepare UI run: %w", err))
		}
		return nil, err
	}
	s.setOperationAvailability(domainui.AvailabilityRunning)
	var terminalRunErr error
	return &preparedUIOperation{
		run: func(ctx context.Context, reporter operation.Reporter[domainui.Frame]) (domainui.Frame, error) {
			releaseProgress := s.channel.BindProgress(reporter)
			defer releaseProgress()
			if deliveryErr := s.sendAvailability(domainui.AvailabilityRunning); deliveryErr != nil {
				return domainui.Frame{}, fmt.Errorf("report running availability: %w", deliveryErr)
			}
			_, runErr := s.runner.RunPrepared(ctx, runID, text)
			terminalRunErr = runErr
			return domainui.NewFrame(domainui.FrameSubmitCompleted), runErr
		},
		failureCode: func(error) string { return failureCodeInternal },
		release: func() {
			s.runner.CancelPrepared(runID)
			availability := domainui.AvailabilityIdle
			if terminalRunErr != nil && s.authenticator.IsSignInRequired(terminalRunErr) {
				availability = domainui.AvailabilityAuthenticationFailed
			}
			s.setOperationAvailability(availability)
			// Channel.Send reports delivery failure to the connection owner because Release cannot return it.
			_ = s.sendAvailability(availability)
		},
		releaseOnce: sync.Once{},
	}, nil
}

// prepareAuthentication reserves one interactive authentication attempt.
func (s *Session) prepareAuthentication() (operation.Prepared[domainui.Frame, domainui.Frame], error) {
	if s.operationAvailabilitySnapshot() != domainui.AvailabilityAuthenticationFailed {
		return nil, rejectOperation(rejectionCodeNotReady, errors.New("authentication retry is not available"))
	}
	s.setOperationAvailability(domainui.AvailabilityAuthenticating)
	authenticationSucceeded := false
	return &preparedUIOperation{
		run: func(ctx context.Context, reporter operation.Reporter[domainui.Frame]) (domainui.Frame, error) {
			releaseProgress := s.channel.BindProgress(reporter)
			defer releaseProgress()
			if deliveryErr := s.sendAvailability(domainui.AvailabilityAuthenticating); deliveryErr != nil {
				return domainui.Frame{}, fmt.Errorf("report authentication availability: %w", deliveryErr)
			}
			err := s.authenticator.SignIn(ctx)
			authenticationSucceeded = err == nil
			return domainui.NewFrame(domainui.FrameAuthenticationCompleted), err
		},
		failureCode: func(error) string { return failureCodeAuthentication },
		release: func() {
			availability := domainui.AvailabilityAuthenticationFailed
			if authenticationSucceeded {
				availability = domainui.AvailabilityIdle
			}
			s.setOperationAvailability(availability)
			// Channel.Send reports delivery failure to the connection owner because Release cannot return it.
			_ = s.sendAvailability(availability)
		},
		releaseOnce: sync.Once{},
	}, nil
}

// prepareSelection validates in-memory selection data before acceptance.
func (s *Session) prepareSelection(
	command domainui.Command,
) (operation.Prepared[domainui.Frame, domainui.Frame], error) {
	if err := s.reserveSelection(); err != nil {
		return nil, err
	}
	if err := validateSelectionCommand(command, s.modelCatalog.Models(), s.modelCatalog.ActiveSelection()); err != nil {
		s.releaseSelection()
		return nil, err
	}
	return &preparedUIOperation{
		run: func(ctx context.Context, _ operation.Reporter[domainui.Frame]) (domainui.Frame, error) {
			var selection model.Selection
			var err error
			if command.Kind == domainui.CommandSelectModel {
				selection, err = s.modelCatalog.SelectModel(
					ctx, model.ProviderID(command.ProviderID.MustGet()), model.ID(command.ModelID.MustGet()),
				)
			} else {
				choice, _ := reasoningChoiceFromUI(command.ReasoningChoice.MustGet())
				selection, err = s.modelCatalog.SelectReasoningChoice(choice)
			}
			return modelSelectionChangedFrame(selectionToUI(selection)), err
		},
		failureCode: selectionFailureCode,
		release:     s.releaseSelection, releaseOnce: sync.Once{},
	}, nil
}

// reserveSelection serializes selection commits and checks readiness atomically.
func (s *Session) reserveSelection() error {
	s.operationMutex.Lock()
	defer s.operationMutex.Unlock()
	if s.operationAvailability == domainui.AvailabilityCheckingAuthentication ||
		s.operationAvailability == domainui.AvailabilityAuthenticating {
		return rejectOperation(rejectionCodeNotReady, errors.New("model selection is not ready"))
	}
	if s.selectionActive {
		return rejectOperation(rejectionCodeBusy, errors.New("another model selection is active"))
	}
	s.selectionActive = true
	return nil
}

// releaseSelection frees the in-memory selection reservation.
func (s *Session) releaseSelection() {
	s.operationMutex.Lock()
	s.selectionActive = false
	s.operationMutex.Unlock()
}

// validateSelectionCommand checks only the in-memory model catalog and request fields.
func validateSelectionCommand(
	command domainui.Command,
	models []model.Descriptor,
	active model.Selection,
) error {
	if command.Kind == domainui.CommandSelectReasoningChoice {
		return validateReasoningSelection(command, models, active)
	}
	providerID, providerPresent := command.ProviderID.Get()
	modelID, modelPresent := command.ModelID.Get()
	if !providerPresent || providerID == "" || !modelPresent || modelID == "" {
		return rejectOperation(rejectionCodeInvalidArgument, errors.New("provider and model are required"))
	}
	for index := range models {
		descriptor := &models[index]
		if descriptor.Provider == model.ProviderID(providerID) && descriptor.Model == model.ID(modelID) {
			return nil
		}
	}
	return rejectOperation("NOT_FOUND", errors.New("configured model was not found"))
}

// validateReasoningSelection checks one choice against the active in-memory descriptor.
func validateReasoningSelection(
	command domainui.Command,
	models []model.Descriptor,
	active model.Selection,
) error {
	choice, present := command.ReasoningChoice.Get()
	if !present {
		return rejectOperation(rejectionCodeInvalidArgument, errors.New("reasoning choice is required"))
	}
	mapped, valid := reasoningChoiceFromUI(choice)
	if !valid {
		return rejectOperation(rejectionCodeInvalidArgument, errors.New("reasoning choice is invalid"))
	}
	for index := range models {
		descriptor := &models[index]
		if descriptor.Provider != active.Provider || descriptor.Model != active.Model {
			continue
		}
		if !slices.Contains(descriptor.ReasoningCapabilities.Choices, mapped) {
			return rejectOperation(
				rejectionCodeInvalidArgument,
				errors.New("reasoning choice is not supported by the active model"),
			)
		}
		return nil
	}
	return rejectOperation("NOT_FOUND", errors.New("active configured model was not found"))
}

// selectionFailureCode classifies accepted selection failures.
func selectionFailureCode(err error) string {
	failure, ok := errors.AsType[interface {
		error
		SelectionCode() string
	}](err)
	if !ok {
		return failureCodeInternal
	}
	switch failure.SelectionCode() {
	case selectionCodeNotFound:
		return failureCodeNotFound
	case selectionCodeReasoning:
		return failureCodeReasoning
	case selectionCodeProviderAuth:
		return failureCodeProviderAuth
	default:
		return failureCodeInternal
	}
}

// prepareSessionOperation validates fields and reserves the session-mutation gate when required.
func (s *Session) prepareSessionOperation(
	command domainui.Command,
) (operation.Prepared[domainui.Frame, domainui.Frame], error) {
	if s.operationAvailabilitySnapshot() == domainui.AvailabilityCheckingAuthentication ||
		s.operationAvailabilitySnapshot() == domainui.AvailabilityAuthenticating {
		return nil, rejectOperation(rejectionCodeNotReady, errors.New("host UI is not ready"))
	}
	if err := validateSessionCommand(command); err != nil {
		return nil, err
	}
	release := func() {}
	if isUISessionMutation(command.Kind) {
		var acquired bool
		release, acquired = s.sessionControl.TryAcquire()
		if !acquired {
			return nil, rejectOperation(
				rejectionCodeBusy,
				errors.New("Session replacement is unavailable: another operation is active"),
			)
		}
	}
	return &preparedUIOperation{
		run: func(ctx context.Context, _ operation.Reporter[domainui.Frame]) (domainui.Frame, error) {
			return s.runSessionOperation(ctx, command)
		},
		failureCode: sessionOperationFailureCode,
		release:     release, releaseOnce: sync.Once{},
	}, nil
}

// validateSessionCommand checks required request fields without domain work.
//
//nolint:gocyclo // The closed request union has distinct required fields.
func validateSessionCommand(command domainui.Command) error {
	switch command.Kind {
	case domainui.CommandCreateSession, domainui.CommandListSessions, domainui.CommandGetSessionInfo,
		domainui.CommandGetSessionTree, domainui.CommandCloneSession:
		return nil
	case domainui.CommandResumeSession:
		if command.SessionID.IsNone() || command.SessionID.OrEmpty() == "" {
			return rejectOperation(rejectionCodeInvalidArgument, errors.New("session identifier is required"))
		}
	case domainui.CommandSetSessionName:
		if command.SessionName.IsNone() || strings.TrimSpace(command.SessionName.OrEmpty()) == "" {
			return rejectOperation(rejectionCodeInvalidArgument, errors.New("session name is required"))
		}
	case domainui.CommandNavigateSessionTree:
		target, present := command.TargetEntryID.Get()
		mode, validMode := summaryModeFromUI(command.SummaryMode)
		focus := strings.TrimSpace(command.CustomFocus.OrEmpty())
		invalidFocus := mode == sessionnavigation.SummaryModeSummarizeWithCustomPrompt && focus == "" ||
			mode != sessionnavigation.SummaryModeSummarizeWithCustomPrompt && focus != ""
		if !present || target == "" || !validMode || invalidFocus {
			return rejectOperation(rejectionCodeInvalidArgument, errors.New("tree navigation request is invalid"))
		}
	case domainui.CommandForkSession:
		if command.TargetEntryID.IsNone() || command.TargetEntryID.OrEmpty() == "" {
			return rejectOperation(rejectionCodeInvalidArgument, errors.New("fork target is required"))
		}
	case domainui.CommandSetEntryLabel:
		if command.TargetEntryID.IsNone() || command.TargetEntryID.OrEmpty() == "" || command.EntryLabel.IsNone() {
			return rejectOperation(rejectionCodeInvalidArgument, errors.New("entry label request is incomplete"))
		}
	case domainui.CommandSubmit,
		domainui.CommandRetryAuthentication,
		domainui.CommandSelectModel,
		domainui.CommandSelectReasoningChoice:
		return rejectOperation(rejectionCodeInvalidArgument, errors.New("UI operation kind is invalid"))
	default:
		return rejectOperation(rejectionCodeInvalidArgument, errors.New("UI operation kind is unknown"))
	}
	return nil
}

// isUISessionMutation reports operation kinds that reserve the shared mutation gate.
func isUISessionMutation(kind domainui.CommandKind) bool {
	switch kind {
	case domainui.CommandCreateSession, domainui.CommandResumeSession, domainui.CommandSetSessionName,
		domainui.CommandNavigateSessionTree, domainui.CommandForkSession, domainui.CommandCloneSession,
		domainui.CommandSetEntryLabel:
		return true
	case domainui.CommandListSessions, domainui.CommandGetSessionInfo, domainui.CommandGetSessionTree,
		domainui.CommandSubmit, domainui.CommandRetryAuthentication, domainui.CommandSelectModel,
		domainui.CommandSelectReasoningChoice:
		return false
	default:
		return false
	}
}

// runSessionOperation executes admitted session work and returns one complete result frame.
//
//nolint:gocyclo // The closed operation union maps directly to distinct domain calls.
func (s *Session) runSessionOperation(ctx context.Context, command domainui.Command) (domainui.Frame, error) {
	switch command.Kind {
	case domainui.CommandCreateSession:
		replacement, err := s.sessionControl.Create(ctx)
		if err != nil {
			return domainui.Frame{}, err
		}
		return sessionChangedFrame(replacement.Info, replacement.Entries)
	case domainui.CommandListSessions:
		listed, err := s.sessionControl.List(ctx)
		return sessionListFrame(listed), err
	case domainui.CommandResumeSession:
		replacement, err := s.sessionControl.Resume(ctx, session.ID(command.SessionID.MustGet()))
		if err != nil {
			return domainui.Frame{}, err
		}
		return sessionChangedFrame(replacement.Info, replacement.Entries)
	case domainui.CommandSetSessionName:
		if _, err := s.sessionControl.SetName(ctx, command.SessionName.MustGet()); err != nil {
			return domainui.Frame{}, err
		}
		snapshot := s.sessionControl.Information()
		return sessionInformationFrame(snapshot.Info, snapshot.Statistics), nil
	case domainui.CommandGetSessionInfo:
		snapshot := s.sessionControl.Information()
		return sessionInformationFrame(snapshot.Info, snapshot.Statistics), nil
	case domainui.CommandGetSessionTree:
		return sessionTreeFrame(s.sessionControl.Tree())
	case domainui.CommandNavigateSessionTree:
		mode, _ := summaryModeFromUI(command.SummaryMode)
		result, err := s.sessionControl.Navigate(ctx, sessionnavigation.Request{
			TargetEntryID: command.TargetEntryID.MustGet(), SummaryMode: mode, CustomFocus: command.CustomFocus,
		})
		if err != nil {
			return domainui.Frame{}, err
		}
		return navigationFrame(result)
	case domainui.CommandForkSession:
		replacement, nextInput, err := s.sessionControl.Fork(ctx, command.TargetEntryID.MustGet())
		if err != nil {
			return domainui.Frame{}, err
		}
		frame, err := sessionChangedFrame(replacement.Info, replacement.Entries)
		frame.Kind, frame.Text = domainui.FrameSessionForked, mo.Some(nextInput)
		return frame, err
	case domainui.CommandCloneSession:
		replacement, err := s.sessionControl.Clone(ctx)
		if err != nil {
			return domainui.Frame{}, err
		}
		frame, err := sessionChangedFrame(replacement.Info, replacement.Entries)
		frame.Kind = domainui.FrameSessionCloned
		return frame, err
	case domainui.CommandSetEntryLabel:
		tree, err := s.sessionControl.SetLabel(ctx, command.TargetEntryID.MustGet(), command.EntryLabel.MustGet())
		if err != nil {
			return domainui.Frame{}, err
		}
		frame, err := sessionTreeFrame(tree)
		frame.Kind = domainui.FrameEntryLabelSet
		return frame, err
	case domainui.CommandSubmit,
		domainui.CommandRetryAuthentication,
		domainui.CommandSelectModel,
		domainui.CommandSelectReasoningChoice:
		return domainui.Frame{}, errors.New("run UI session operation: invalid operation kind")
	default:
		return domainui.Frame{}, errors.New("run UI session operation: unknown operation kind")
	}
}

// sessionOperationFailureCode classifies accepted session operation errors.
func sessionOperationFailureCode(err error) string {
	switch {
	case errors.Is(err, session.ErrPersistenceUnavailable):
		return failureCodePersistence
	case errors.Is(err, sessionnavigation.ErrModelUnavailable):
		return failureCodeModelUnavailable
	case errors.Is(err, sessionnavigation.ErrCredentialUnavailable):
		return failureCodeProviderAuth
	case errors.Is(err, sessionnavigation.ErrModelFailed):
		return failureCodeModelFailed
	case errors.Is(err, sessionnavigation.ErrExtensionInvalidResult):
		return failureCodeExtensionInvalid
	case errors.Is(err, sessionnavigation.ErrExtensionUnavailable):
		return failureCodeExtension
	case errors.Is(err, session.ErrEntryNotFound):
		return failureCodeSession
	default:
		return failureCodeInternal
	}
}

// operationAvailabilitySnapshot returns current operation-mode admission state.
func (s *Session) operationAvailabilitySnapshot() domainui.Availability {
	s.operationMutex.Lock()
	defer s.operationMutex.Unlock()
	return s.operationAvailability
}

// setOperationAvailability updates operation-mode admission state.
func (s *Session) setOperationAvailability(availability domainui.Availability) {
	s.operationMutex.Lock()
	s.operationAvailability = availability
	s.operationMutex.Unlock()
}
