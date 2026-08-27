package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// operationKind identifies the single active asynchronous Host operation.
type operationKind uint8

const (
	operationAuthenticationCheck operationKind = iota + 1
	operationSignIn
	operationRun
)

// operationResult carries one active operation completion.
type operationResult struct {
	kind operationKind
	err  error
}

// receivedCommand carries one command or authoritative stream termination.
type receivedCommand struct {
	command domainui.Command
	err     error
}

// Session coordinates authentication, one active run, UI commands, and stream termination.
type Session struct {
	channel             Channel
	runner              AgentRunner
	authenticator       Authenticator
	modelCatalog        ModelCatalog
	sessionControl      SessionControl
	afterInitialization func(context.Context)
}

var _ controllerui.Session = (*Session)(nil)

// NewSession creates one UI lifecycle session.
func NewSession(
	channel Channel,
	runner AgentRunner,
	authenticator Authenticator,
	modelCatalog ModelCatalog,
	sessionControl SessionControl,
	afterInitialization func(context.Context),
) *Session {
	return &Session{
		channel: channel, runner: runner, authenticator: authenticator,
		modelCatalog:   modelCatalog,
		sessionControl: sessionControl, afterInitialization: afterInitialization,
	}
}

// Run sends initialization and owns the selected UI stream until termination.
func (s *Session) Run(ctx context.Context, initialization domainui.Initialization) error {
	if err := s.channel.Send(initializationFrame(initialization)); err != nil {
		return fmt.Errorf("send UI initialization: %w", err)
	}
	s.afterInitialization(ctx)

	receiverContext, cancelReceiver := context.WithCancel(ctx)
	commands := make(chan receivedCommand)
	receiverDone := make(chan struct{})
	go func() {
		defer close(receiverDone)
		s.receiveCommands(receiverContext, commands)
	}()
	results := make(chan operationResult)
	availability := domainui.AvailabilityCheckingAuthentication
	activeCancel, activeKind := s.startAuthenticationCheck(ctx, results)
	defer func() {
		s.shutdown(activeCancel, activeKind, results, cancelReceiver, receiverDone)
	}()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("run UI session: %w", ctx.Err())
		case received := <-commands:
			if received.err != nil {
				if errors.Is(received.err, io.EOF) || errors.Is(received.err, context.Canceled) {
					return nil
				}
				return received.err
			}
			if received.command.Kind == domainui.CommandQuit {
				return nil
			}
			var err error
			availability, activeCancel, activeKind, err = s.applyCommand(
				ctx, availability, activeCancel, activeKind, received.command, results,
			)
			if err != nil {
				return err
			}
		case result := <-results:
			var resultErr error
			availability, activeCancel, activeKind, resultErr = s.applyResult(
				ctx, availability, result, results,
			)
			if resultErr != nil {
				return s.resolveDeliveryFailure(ctx, commands, resultErr)
			}
		}
	}
}

// receiveCommands performs the one blocking receive operation for the session.
func (s *Session) receiveCommands(ctx context.Context, commands chan<- receivedCommand) {
	for {
		command, err := s.channel.Receive()
		select {
		case commands <- receivedCommand{command: command, err: err}:
		case <-ctx.Done():
			return
		}
		if err != nil || command.Kind == domainui.CommandQuit {
			return
		}
	}
}

// startAuthenticationCheck begins provider-owned credential validation or refresh.
func (s *Session) startAuthenticationCheck(
	ctx context.Context,
	results chan<- operationResult,
) (context.CancelFunc, operationKind) {
	operationContext, cancel := context.WithCancel(ctx)
	go func() {
		results <- operationResult{
			kind: operationAuthenticationCheck,
			err:  s.authenticator.CheckAuthentication(operationContext),
		}
	}()
	return cancel, operationAuthenticationCheck
}

// startSignIn begins one explicit or startup-required browser OAuth operation.
func (s *Session) startSignIn(
	ctx context.Context,
	results chan<- operationResult,
) (context.CancelFunc, operationKind) {
	operationContext, cancel := context.WithCancel(ctx)
	go func() {
		results <- operationResult{
			kind: operationSignIn, err: s.authenticator.SignIn(operationContext),
		}
	}()
	return cancel, operationSignIn
}

// startRun begins one user request against the retained Agent Core session.
func (s *Session) startRun(
	ctx context.Context,
	text string,
	results chan<- operationResult,
) (context.CancelFunc, operationKind) {
	operationContext, cancel := context.WithCancel(ctx)
	go func() {
		_, err := s.runner.Run(operationContext, text)
		results <- operationResult{kind: operationRun, err: err}
	}()
	return cancel, operationRun
}

// applyCommand enforces idle-only submission and explicit stop/retry behavior.
func (s *Session) applyCommand(
	ctx context.Context,
	availability domainui.Availability,
	activeCancel context.CancelFunc,
	activeKind operationKind,
	command domainui.Command,
	results chan<- operationResult,
) (domainui.Availability, context.CancelFunc, operationKind, error) {
	if handled, err := s.applySessionCommand(ctx, command); handled {
		return availability, activeCancel, activeKind, err
	}
	if command.Kind == domainui.CommandSubmit {
		return s.applySubmit(ctx, availability, activeCancel, activeKind, command, results)
	}
	switch command.Kind {
	case domainui.CommandStop:
		if activeKind != operationRun {
			return availability, activeCancel, activeKind, s.sendInformation("No agent run is active.")
		}
		activeCancel()
		return availability, activeCancel, activeKind, nil
	case domainui.CommandRetryAuthentication:
		if availability != domainui.AvailabilityAuthenticationFailed {
			return availability, activeCancel, activeKind, s.sendInformation("Authentication retry is not available.")
		}
		if err := s.sendAvailability(domainui.AvailabilityAuthenticating); err != nil {
			return availability, activeCancel, activeKind, err
		}
		activeCancel, activeKind = s.startSignIn(ctx, results)
		return domainui.AvailabilityAuthenticating, activeCancel, activeKind, nil
	case domainui.CommandQuit:
		return availability, activeCancel, activeKind, nil
	case domainui.CommandSelectModel, domainui.CommandSelectReasoningChoice:
		if activeKind == operationAuthenticationCheck || activeKind == operationSignIn {
			return availability, activeCancel, activeKind, s.sendSelectionError()
		}
		return availability, activeCancel, activeKind, s.applySelectionCommand(ctx, command)
	case domainui.CommandSubmit, domainui.CommandCreateSession, domainui.CommandListSessions,
		domainui.CommandResumeSession, domainui.CommandSetSessionName,
		domainui.CommandGetSessionInfo:
		return availability, activeCancel, activeKind, s.sendInformation("Session command was not handled.")
	default:
		return availability, activeCancel, activeKind, s.sendInformation("Unsupported UI command.")
	}
}

// applySubmit publishes running availability before starting work, so clients never observe a run while idle.
func (s *Session) applySubmit(
	ctx context.Context,
	availability domainui.Availability,
	activeCancel context.CancelFunc,
	activeKind operationKind,
	command domainui.Command,
	results chan<- operationResult,
) (domainui.Availability, context.CancelFunc, operationKind, error) {
	if availability != domainui.AvailabilityIdle {
		return availability, activeCancel, activeKind, s.sendInformation("Glyph is not ready for another request.")
	}
	text, present := command.Text.Get()
	if !present || strings.TrimSpace(text) == "" {
		return availability, activeCancel, activeKind, s.sendInformation("A nonempty request is required.")
	}
	if err := s.sendAvailability(domainui.AvailabilityRunning); err != nil {
		return availability, activeCancel, activeKind, err
	}
	activeCancel, activeKind = s.startRun(ctx, text, results)
	return domainui.AvailabilityRunning, activeCancel, activeKind, nil
}

// applySessionCommand maps lifecycle results to frames only after the active-session operation succeeds.
func (s *Session) applySessionCommand(ctx context.Context, command domainui.Command) (bool, error) {
	switch command.Kind {
	case domainui.CommandCreateSession:
		replacement, err := s.sessionControl.Create(ctx)
		if err != nil {
			return true, s.sendInformation("Session replacement is unavailable.")
		}
		return true, s.sendSessionChanged(replacement)
	case domainui.CommandListSessions:
		listed, err := s.sessionControl.List(ctx)
		if err != nil {
			return true, s.sendInformation("Sessions are unavailable.")
		}
		return true, s.channel.Send(sessionListFrame(listed))
	case domainui.CommandResumeSession:
		id, present := command.SessionID.Get()
		if !present || id == "" {
			return true, s.sendInformation("A session ID is required.")
		}
		replacement, err := s.sessionControl.Resume(ctx, session.ID(id))
		if err != nil {
			return true, s.sendInformation("Session replacement is unavailable.")
		}
		return true, s.sendSessionChanged(replacement)
	case domainui.CommandSetSessionName:
		name, present := command.SessionName.Get()
		if !present {
			return true, s.sendInformation("A session name is required.")
		}
		info, err := s.sessionControl.SetName(ctx, name)
		if err != nil {
			return true, s.sendInformation("Session naming is unavailable.")
		}
		return true, s.channel.Send(sessionInfoFrame(domainui.FrameSessionInformation, info))
	case domainui.CommandGetSessionInfo:
		return true, s.channel.Send(sessionInfoFrame(domainui.FrameSessionInformation, s.sessionControl.Info()))
	case domainui.CommandSubmit, domainui.CommandStop, domainui.CommandRetryAuthentication,
		domainui.CommandQuit, domainui.CommandSelectModel, domainui.CommandSelectReasoningChoice:
		return false, nil
	default:
		return false, nil
	}
}

// applySelectionCommand commits one model or reasoning selection without changing run state.
func (s *Session) applySelectionCommand(ctx context.Context, command domainui.Command) error {
	var selection model.Selection
	var err error
	switch command.Kind {
	case domainui.CommandSelectModel:
		providerID, providerPresent := command.ProviderID.Get()
		modelID, modelPresent := command.ModelID.Get()
		if !providerPresent || !modelPresent || providerID == "" || modelID == "" {
			return s.sendSelectionError()
		}
		selection, err = s.modelCatalog.SelectModel(
			ctx, model.ProviderID(providerID), model.ID(modelID),
		)
	case domainui.CommandSelectReasoningChoice:
		reasoningChoice, present := command.ReasoningChoice.Get()
		if !present {
			return s.sendSelectionError()
		}
		level, valid := reasoningChoiceFromUI(reasoningChoice)
		if !valid {
			return s.sendSelectionError()
		}
		selection, err = s.modelCatalog.SelectReasoningChoice(level)
	case domainui.CommandSubmit, domainui.CommandStop,
		domainui.CommandRetryAuthentication, domainui.CommandQuit,
		domainui.CommandCreateSession, domainui.CommandListSessions,
		domainui.CommandResumeSession, domainui.CommandSetSessionName,
		domainui.CommandGetSessionInfo:
		return s.sendSelectionError()
	default:
		return s.sendSelectionError()
	}
	if err != nil {
		return s.sendSelectionError()
	}
	return s.channel.Send(modelSelectionChangedFrame(selectionToUI(selection)))
}

// applyResult advances authentication or run availability after one completion.
func (s *Session) applyResult(
	ctx context.Context,
	availability domainui.Availability,
	result operationResult,
	results chan<- operationResult,
) (domainui.Availability, context.CancelFunc, operationKind, error) {
	switch result.kind {
	case operationAuthenticationCheck:
		return s.applyAuthenticationCheck(ctx, availability, result.err, results)
	case operationSignIn:
		return s.applySignInResult(availability, result.err)
	case operationRun:
		return s.applyRunResult(availability, result.err)
	default:
		return availability, nil, 0, nil
	}
}

// applyAuthenticationCheck enters ready state or starts the one automatic OAuth attempt.
func (s *Session) applyAuthenticationCheck(
	ctx context.Context,
	availability domainui.Availability,
	checkErr error,
	results chan<- operationResult,
) (domainui.Availability, context.CancelFunc, operationKind, error) {
	if checkErr == nil {
		if err := s.sendAvailability(domainui.AvailabilityIdle); err != nil {
			return availability, nil, 0, err
		}
		return domainui.AvailabilityIdle, nil, 0, nil
	}
	if s.authenticator.IsSignInRequired(checkErr) {
		if err := s.sendAvailability(domainui.AvailabilityAuthenticating); err != nil {
			return availability, nil, 0, err
		}
		cancel, kind := s.startSignIn(ctx, results)
		return domainui.AvailabilityAuthenticating, cancel, kind, nil
	}
	if err := s.sendAuthenticationError(checkErr); err != nil {
		return availability, nil, 0, err
	}
	return domainui.AvailabilityAuthenticationFailed, nil, 0, nil
}

// applySignInResult enters ready or explicit-retry state after browser OAuth.
func (s *Session) applySignInResult(
	availability domainui.Availability,
	signInErr error,
) (domainui.Availability, context.CancelFunc, operationKind, error) {
	if signInErr == nil {
		if err := s.sendAvailability(domainui.AvailabilityIdle); err != nil {
			return availability, nil, 0, err
		}
		return domainui.AvailabilityIdle, nil, 0, nil
	}
	if err := s.sendAuthenticationError(signInErr); err != nil {
		return availability, nil, 0, err
	}
	return domainui.AvailabilityAuthenticationFailed, nil, 0, nil
}

// applyRunResult reports one run failure and returns the authenticated session to idle.
func (s *Session) applyRunResult(
	availability domainui.Availability,
	runErr error,
) (domainui.Availability, context.CancelFunc, operationKind, error) {
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		if s.authenticator.IsSignInRequired(runErr) {
			if err := s.sendAuthenticationError(runErr); err != nil {
				return availability, nil, 0, err
			}
			return domainui.AvailabilityAuthenticationFailed, nil, 0, nil
		}
		if err := s.channel.Send(errorFrame(runErr.Error(), false)); err != nil {
			return availability, nil, 0, err
		}
	}
	if err := s.sendAvailability(domainui.AvailabilityIdle); err != nil {
		return availability, nil, 0, err
	}
	return domainui.AvailabilityIdle, nil, 0, nil
}

// resolveDeliveryFailure accepts terminal EOF only when command reception confirms termination.
func (*Session) resolveDeliveryFailure(
	ctx context.Context,
	commands <-chan receivedCommand,
	deliveryErr error,
) error {
	if !errors.Is(deliveryErr, io.EOF) {
		return deliveryErr
	}
	select {
	case received := <-commands:
		if received.command.Kind == domainui.CommandQuit ||
			errors.Is(received.err, io.EOF) ||
			errors.Is(received.err, context.Canceled) {
			return nil
		}
		if received.err != nil {
			return errors.Join(deliveryErr, received.err)
		}
		return deliveryErr
	case <-ctx.Done():
		return errors.Join(deliveryErr, fmt.Errorf("run UI session: %w", ctx.Err()))
	}
}

// shutdown cancels and joins the active operation and command receiver.
func (s *Session) shutdown(
	activeCancel context.CancelFunc,
	activeKind operationKind,
	results <-chan operationResult,
	cancelReceiver context.CancelFunc,
	receiverDone <-chan struct{},
) {
	if activeKind != 0 {
		activeCancel()
	}
	cancelReceiver()
	s.channel.Close()
	if activeKind != 0 {
		<-results
	}
	<-receiverDone
}

// sendAvailability emits one ordered lifecycle availability update.
func (s *Session) sendAvailability(availability domainui.Availability) error {
	return s.channel.Send(lifecycleFrame(availabilityLifecycle(availability)))
}

// sendSessionChanged replaces public session text only after Host replacement commits.
func (s *Session) sendSessionChanged(replacement session.Replacement) error {
	frame, err := sessionChangedFrame(replacement.Info, replacement.Entries)
	if err != nil {
		return err
	}
	return s.channel.Send(frame)
}

// sendInformation emits one non-terminal command rejection or notification.
func (s *Session) sendInformation(text string) error {
	return s.channel.Send(informationFrame(text))
}

// sendSelectionError emits one fixed error without exposing catalog details.
func (s *Session) sendSelectionError() error {
	return s.channel.Send(errorFrame("Could not change model selection.", false))
}

// sendAuthenticationError emits a safe state that permits explicit retry.
func (s *Session) sendAuthenticationError(err error) error {
	if sendErr := s.channel.Send(errorFrame(err.Error(), true)); sendErr != nil {
		return sendErr
	}
	return s.sendAvailability(domainui.AvailabilityAuthenticationFailed)
}
