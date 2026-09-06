package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
)

const (
	// selectionCodeNotFound identifies an absent configured model.
	selectionCodeNotFound = "not_found"
	// selectionCodeReasoning identifies an unsupported configured reasoning choice.
	selectionCodeReasoning = "reasoning_unsupported"
	// selectionCodeProviderAuth identifies unavailable provider credentials.
	selectionCodeProviderAuth = "credential_unavailable"
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
	remainingErr := withoutCancellationLeaves(err)
	if err != nil && remainingErr == nil {
		return operation.Canceled[controllerui.Frame]()
	}
	if remainingErr != nil {
		return operation.Failed[controllerui.Frame](prepared.failureCode(remainingErr), remainingErr)
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

// Initialize sends startup state before any runtime activation or command execution.
func (s *Session) Initialize(ctx context.Context) error {
	if err := s.output.Initialize(ctx, s.initialization); err != nil {
		return fmt.Errorf("send UI initialization: %w", err)
	}
	return nil
}

// Activate starts readiness work after the controller attaches output and its failure owner.
func (s *Session) Activate(ctx context.Context) func() {
	authenticationContext, cancelAuthentication := context.WithCancelCause(ctx)
	var authenticationWork sync.WaitGroup
	s.afterInitialization(ctx)
	authenticationWork.Go(func() { s.checkOperationAuthentication(authenticationContext) })
	return func() {
		cancelAuthentication(context.Canceled)
		authenticationWork.Wait()
	}
}

// checkOperationAuthentication resolves startup readiness outside request receipt.
func (s *Session) checkOperationAuthentication(ctx context.Context) {
	err := s.authenticator.CheckAuthentication(ctx)
	remainingErr := withoutCancellationLeaves(err)
	if err != nil && remainingErr == nil {
		return
	}
	availability := AvailabilityIdle
	if remainingErr != nil {
		availability = AvailabilityAuthenticationFailed
		code := controllerui.FailureCodeInternal
		if s.authenticator.IsSignInRequired(remainingErr) {
			code = controllerui.FailureCodeAuthentication
		}
		// Output.ReportError routes writer failures to the connection failure owner.
		// This worker has no result channel.
		_ = s.output.ReportError(code, remainingErr.Error())
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
		return s.prepareAuthentication()
	}
	if command.Kind == controllerui.CommandSelectModel || command.Kind == controllerui.CommandSelectReasoningChoice {
		return s.prepareSelection(command)
	}
	return s.prepareSessionOperation(command)
}

// prepareSubmit reserves the agent-run gate before acceptance.
func (s *Session) prepareSubmit(
	command controllerui.Command,
) (operation.Prepared[controllerui.Frame, controllerui.Frame], error) {
	text, textErr := command.SubmittedText()
	if textErr != nil {
		return nil, rejectOperation(controllerui.RejectionCodeInvalidArgument, textErr)
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
			releaseProgress := s.output.BindProgress(reporter)
			defer releaseProgress()
			if deliveryErr := s.output.SetAvailability(AvailabilityRunning); deliveryErr != nil {
				return controllerui.Frame{}, fmt.Errorf("report running availability: %w", deliveryErr)
			}
			_, runErr := s.runner.RunPrepared(ctx, runID, text)
			terminalRunErr = runErr
			return controllerui.NewFrame(controllerui.FrameSubmitCompleted), runErr
		},
		failureCode: func(error) string { return controllerui.FailureCodeInternal },
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

// prepareAuthentication reserves one interactive authentication attempt.
func (s *Session) prepareAuthentication() (operation.Prepared[controllerui.Frame, controllerui.Frame], error) {
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
			releaseProgress := s.output.BindProgress(reporter)
			defer releaseProgress()
			if deliveryErr := s.output.SetAvailability(AvailabilityAuthenticating); deliveryErr != nil {
				return controllerui.Frame{}, fmt.Errorf("report authentication availability: %w", deliveryErr)
			}
			err := s.authenticator.SignIn(ctx)
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

// prepareSelection validates in-memory selection data before acceptance.
func (s *Session) prepareSelection(
	command controllerui.Command,
) (operation.Prepared[controllerui.Frame, controllerui.Frame], error) {
	if err := s.reserveSelection(); err != nil {
		return nil, err
	}
	if err := validateSelectionCommand(command, s.modelCatalog.Models(), s.modelCatalog.ActiveSelection()); err != nil {
		s.releaseSelection()
		return nil, err
	}
	return &preparedUIOperation{
		run: func(ctx context.Context, _ operation.Reporter[controllerui.Frame]) (controllerui.Frame, error) {
			var selection model.Selection
			var err error
			if command.Kind == controllerui.CommandSelectModel {
				selection, err = s.modelCatalog.SelectModel(
					ctx, model.ProviderID(command.ProviderID.MustGet()), model.ID(command.ModelID.MustGet()),
				)
			} else {
				choice := command.ReasoningChoice.MustGet()
				selection, err = s.modelCatalog.SelectReasoningChoice(choice)
			}
			return modelSelectionChangedFrame(selection), err
		},
		failureCode: selectionFailureCode,
		release:     s.releaseSelection, releaseOnce: sync.Once{},
	}, nil
}

// reserveSelection serializes selection commits and checks readiness atomically.
func (s *Session) reserveSelection() error {
	s.operationMutex.Lock()
	defer s.operationMutex.Unlock()
	if s.operationAvailability == AvailabilityCheckingAuthentication ||
		s.operationAvailability == AvailabilityAuthenticating {
		return rejectOperation(controllerui.RejectionCodeNotReady, errors.New("model selection is not ready"))
	}
	if s.selectionActive {
		return rejectOperation(controllerui.RejectionCodeBusy, errors.New("another model selection is active"))
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
	command controllerui.Command,
	models []model.Descriptor,
	active model.Selection,
) error {
	if command.Kind == controllerui.CommandSelectReasoningChoice {
		return validateReasoningSelection(command, models, active)
	}
	providerID, providerPresent := command.ProviderID.Get()
	modelID, modelPresent := command.ModelID.Get()
	if !providerPresent || providerID == "" || !modelPresent || modelID == "" {
		return rejectOperation(controllerui.RejectionCodeInvalidArgument, errors.New("provider and model are required"))
	}
	for index := range models {
		descriptor := &models[index]
		if descriptor.Provider == model.ProviderID(providerID) && descriptor.Model == model.ID(modelID) {
			return nil
		}
	}
	return rejectOperation(controllerui.RejectionCodeNotFound, errors.New("configured model was not found"))
}

// validateReasoningSelection checks one choice against the active in-memory descriptor.
func validateReasoningSelection(
	command controllerui.Command,
	models []model.Descriptor,
	active model.Selection,
) error {
	choice, validationErr := command.SelectedReasoningChoice()
	if validationErr != nil {
		return rejectOperation(controllerui.RejectionCodeInvalidArgument, validationErr)
	}
	for index := range models {
		descriptor := &models[index]
		if descriptor.Provider != active.Provider || descriptor.Model != active.Model {
			continue
		}
		if !slices.Contains(descriptor.ReasoningCapabilities.Choices, choice) {
			return rejectOperation(
				controllerui.RejectionCodeInvalidArgument,
				errors.New("reasoning choice is not supported by the active model"),
			)
		}
		return nil
	}
	return rejectOperation(controllerui.RejectionCodeNotFound, errors.New("active configured model was not found"))
}

// selectionFailureCode classifies accepted selection failures.
func selectionFailureCode(err error) string {
	failure, ok := errors.AsType[SelectionFailure](err)
	if !ok {
		return controllerui.FailureCodeInternal
	}
	switch failure.SelectionCode() {
	case selectionCodeNotFound:
		return controllerui.FailureCodeNotFound
	case selectionCodeReasoning:
		return controllerui.FailureCodeReasoning
	case selectionCodeProviderAuth:
		return controllerui.FailureCodeProviderAuth
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
		info, entries, err := s.activeSessions.ResumeActive(ctx, session.ID(command.SessionID.MustGet()))
		if err != nil {
			return controllerui.Frame{}, err
		}
		return sessionChangedFrame(info, entries)
	case controllerui.CommandSetSessionName:
		if _, err := s.activeSessions.SetActiveName(ctx, command.SessionName.MustGet()); err != nil {
			return controllerui.Frame{}, err
		}
		info, statistics := s.activeSessions.ActiveInformation()
		return sessionInformationFrame(info, statistics), nil
	case controllerui.CommandGetSessionInfo:
		info, statistics := s.activeSessions.ActiveInformation()
		return sessionInformationFrame(info, statistics), nil
	case controllerui.CommandGetSessionTree:
		return sessionTreeFrame(s.activeSessions.Tree())
	case controllerui.CommandNavigateSessionTree:
		result, err := s.navigator.NavigateUI(ctx, NavigationIntent{
			TargetEntryID: command.TargetEntryID.MustGet(),
			SummaryMode:   command.SummaryMode,
			CustomFocus:   command.CustomFocus,
		}, navigationProgressCallback(reporter))
		if err != nil {
			return controllerui.Frame{}, err
		}
		return result.frame()
	case controllerui.CommandForkSession:
		info, entries, nextInput, err := s.activeSessions.ForkActive(ctx, command.TargetEntryID.MustGet())
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
		tree, err := s.activeSessions.SetLabel(ctx, command.TargetEntryID.MustGet(), command.EntryLabel.MustGet())
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
	switch {
	case errors.Is(err, session.ErrPersistenceUnavailable):
		return controllerui.FailureCodePersistence
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
