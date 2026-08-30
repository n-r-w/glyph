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
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
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
	// kind identifies the completed asynchronous operation.
	kind operationKind
	// err contains the operation failure.
	err error
}

// receivedCommand carries one command or authoritative stream termination.
type receivedCommand struct {
	// command contains one UI request.
	command domainui.Command
	// err contains authoritative stream termination.
	err error
}

// deliveryFailureError keeps an undelivered source separate from its frame delivery failure.
type deliveryFailureError struct {
	// sourceErr contains the undelivered source failure.
	sourceErr error
	// deliveryErr contains the frame delivery failure.
	deliveryErr error
}

// Error reports both undelivered causes without changing their order.
func (e *deliveryFailureError) Error() string {
	return errors.Join(e.sourceErr, e.deliveryErr).Error()
}

// Unwrap keeps source and delivery causes independently reachable.
func (e *deliveryFailureError) Unwrap() []error {
	return []error{e.sourceErr, e.deliveryErr}
}

// Session coordinates authentication, one active run, UI commands, and stream termination.
type Session struct {
	// channel sends frames to and receives commands from the selected UI.
	channel Channel
	// runner executes and cancels Agent Core runs.
	runner AgentRunner
	// authenticator manages provider authentication.
	authenticator Authenticator
	// modelCatalog owns configured models and the active selection.
	modelCatalog ModelCatalog
	// sessionControl owns active-session lifecycle operations.
	sessionControl SessionControl
	// afterInitialization starts work that requires a connected UI.
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
func (s *Session) Run(ctx context.Context, initialization domainui.Initialization) (returnErr error) {
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
		returnErr = errors.Join(
			returnErr,
			s.shutdown(activeCancel, activeKind, results, cancelReceiver, receiverDone),
		)
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
				return s.resolveDeliveryFailure(ctx, commands, err)
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
		domainui.CommandGetSessionInfo, domainui.CommandGetSessionTree, domainui.CommandNavigateSessionTree:
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
//
//nolint:gocyclo // The switch dispatches every closed UI session command.
func (s *Session) applySessionCommand(ctx context.Context, command domainui.Command) (bool, error) {
	switch command.Kind {
	case domainui.CommandCreateSession:
		replacement, err := s.sessionControl.Create(ctx)
		if err != nil {
			return true, preserveUndeliveredSource(
				err, s.sendInformation(sessionFailureText(err, "Session replacement is unavailable.")),
			)
		}
		return true, s.sendSessionChanged(replacement)
	case domainui.CommandListSessions:
		listed, err := s.sessionControl.List(ctx)
		if err != nil {
			return true, preserveUndeliveredSource(
				err, s.sendInformation(fmt.Sprintf("Sessions are unavailable: %v", err)),
			)
		}
		return true, s.channel.Send(sessionListFrame(listed))
	case domainui.CommandResumeSession:
		id, present := command.SessionID.Get()
		if !present || id == "" {
			return true, s.sendInformation("A session ID is required.")
		}
		replacement, err := s.sessionControl.Resume(ctx, session.ID(id))
		if err != nil {
			return true, preserveUndeliveredSource(
				err, s.sendInformation(sessionFailureText(err, "Session replacement is unavailable.")),
			)
		}
		return true, s.sendSessionChanged(replacement)
	case domainui.CommandSetSessionName:
		name, present := command.SessionName.Get()
		if !present {
			return true, s.sendInformation("A session name is required.")
		}
		if _, err := s.sessionControl.SetName(ctx, name); err != nil {
			return true, preserveUndeliveredSource(
				err, s.sendInformation(sessionFailureText(err, "Session naming is unavailable.")),
			)
		}
		snapshot := s.sessionControl.Information()
		return true, s.channel.Send(sessionInformationFrame(snapshot.Info, snapshot.Statistics))
	case domainui.CommandGetSessionInfo:
		snapshot := s.sessionControl.Information()
		return true, s.channel.Send(sessionInformationFrame(snapshot.Info, snapshot.Statistics))
	case domainui.CommandGetSessionTree:
		frame, err := sessionTreeFrame(s.sessionControl.Tree())
		if err != nil {
			return true, s.channel.Send(treeFailureFrame(domainui.TreeFailureInternal, err.Error()))
		}
		return true, s.channel.Send(frame)
	case domainui.CommandNavigateSessionTree:
		return true, s.navigateSessionTree(ctx, command)
	case domainui.CommandSubmit, domainui.CommandStop, domainui.CommandRetryAuthentication,
		domainui.CommandQuit, domainui.CommandSelectModel, domainui.CommandSelectReasoningChoice:
		return false, nil
	default:
		return false, nil
	}
}

// navigateSessionTree commits requested navigation or sends one closed terminal result.
func (s *Session) navigateSessionTree(ctx context.Context, command domainui.Command) error {
	targetID, present := command.TargetEntryID.Get()
	mode, validMode := summaryModeFromUI(command.SummaryMode)
	customFocus := strings.TrimSpace(command.CustomFocus.OrEmpty())
	invalidFocus := mode == sessionnavigation.SummaryModeSummarizeWithCustomPrompt && customFocus == "" ||
		mode != sessionnavigation.SummaryModeSummarizeWithCustomPrompt && customFocus != ""
	if !present || targetID == "" || invalidFocus || !validMode {
		return s.channel.Send(treeFailureFrame(domainui.TreeFailureInvalidArgument, "invalid tree navigation command"))
	}
	result, err := s.sessionControl.Navigate(ctx, sessionnavigation.Request{
		TargetEntryID: targetID, SummaryMode: mode, CustomFocus: command.CustomFocus,
	})
	if err != nil {
		return s.channel.Send(navigationFailureFrame(err))
	}
	frame, mapErr := navigationFrame(result)
	if mapErr != nil {
		return s.channel.Send(treeFailureFrame(domainui.TreeFailureInternal, mapErr.Error()))
	}
	return s.channel.Send(frame)
}

// navigationFailureFrame maps one navigation error to the closed UI result contract.
func navigationFailureFrame(err error) domainui.Frame {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return canceledNavigationFrame()
	case errors.Is(err, session.ErrBusy):
		return treeFailureFrame(domainui.TreeFailureBusy, "another operation is active")
	case errors.Is(err, session.ErrEntryNotFound):
		return treeFailureFrame(domainui.TreeFailureNotFound, "session tree entry was not found")
	case errors.Is(err, sessionnavigation.ErrModelUnavailable):
		return treeFailureFrame(domainui.TreeFailureModelUnavailable, err.Error())
	case errors.Is(err, sessionnavigation.ErrCredentialUnavailable):
		return treeFailureFrame(domainui.TreeFailureCredentialUnavailable, err.Error())
	case errors.Is(err, sessionnavigation.ErrModelFailed):
		return treeFailureFrame(domainui.TreeFailureModelFailed, err.Error())
	case errors.Is(err, session.ErrPersistenceUnavailable):
		return treeFailureFrame(domainui.TreeFailurePersistenceUnavailable, err.Error())
	default:
		return treeFailureFrame(domainui.TreeFailureInternal, fmt.Sprintf("tree navigation failed: %v", err))
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
		domainui.CommandGetSessionInfo, domainui.CommandGetSessionTree, domainui.CommandNavigateSessionTree:
		return s.sendSelectionError()
	default:
		return s.sendSelectionError()
	}
	if err != nil {
		return preserveUndeliveredSource(
			err, s.channel.Send(errorFrame(fmt.Sprintf("Could not change model selection: %v", err), false)),
		)
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
		if err := s.sendSourceError(checkErr, true); err != nil {
			return availability, nil, 0, err
		}
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
	visibleErr := withoutCancellation(runErr)
	if visibleErr != nil {
		if s.authenticator.IsSignInRequired(visibleErr) {
			if err := s.sendAuthenticationError(visibleErr); err != nil {
				return availability, nil, 0, err
			}
			return domainui.AvailabilityAuthenticationFailed, nil, 0, nil
		}
		if err := s.sendSourceError(visibleErr, false); err != nil {
			return availability, nil, 0, err
		}
	}
	if err := s.sendAvailability(domainui.AvailabilityIdle); err != nil {
		return availability, nil, 0, err
	}
	return domainui.AvailabilityIdle, nil, 0, nil
}

// withoutCancellation removes cancellation leaves while retaining independent run failures.
func withoutCancellation(err error) error {
	return withoutClosureLeaves(err, false)
}

// withoutConfirmedClosure removes only cancellation and EOF leaves owned by confirmed UI closure.
func withoutConfirmedClosure(err error) error {
	return withoutClosureLeaves(err, true)
}

// withoutClosureLeaves removes configured closure leaves and retains wrappers around surviving causes.
func withoutClosureLeaves(err error, removeEOF bool) error {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		filtered := make([]error, 0, len(joined.Unwrap()))
		for _, nested := range joined.Unwrap() {
			if remaining := withoutClosureLeaves(nested, removeEOF); remaining != nil {
				filtered = append(filtered, remaining)
			}
		}
		return errors.Join(filtered...)
	}
	if nested := errors.Unwrap(err); nested != nil {
		remaining := withoutClosureLeaves(nested, removeEOF)
		if remaining == nil {
			return nil
		}
		if isClosureLeaf(err, removeEOF) {
			return remaining
		}
		return err
	}
	if isClosureLeaf(err, removeEOF) {
		return nil
	}
	return err
}

// isClosureLeaf identifies cancellation and, after confirmed command closure, EOF leaves.
func isClosureLeaf(err error, includeEOF bool) bool {
	return errors.Is(err, context.Canceled) || includeEOF && errors.Is(err, io.EOF)
}

// resolveDeliveryFailure accepts terminal EOF only when command reception confirms termination.
func (*Session) resolveDeliveryFailure(
	ctx context.Context,
	commands <-chan receivedCommand,
	err error,
) error {
	sourceErr := error(nil)
	deliveryErr := err
	if failedDelivery, ok := errors.AsType[*deliveryFailureError](err); ok {
		sourceErr = failedDelivery.sourceErr
		deliveryErr = failedDelivery.deliveryErr
	}
	combinedErr := joinIndependentError(sourceErr, deliveryErr)
	if !errors.Is(deliveryErr, io.EOF) && !errors.Is(deliveryErr, context.Canceled) {
		return combinedErr
	}
	select {
	case received := <-commands:
		if received.command.Kind == domainui.CommandQuit ||
			errors.Is(received.err, io.EOF) ||
			errors.Is(received.err, context.Canceled) {
			return joinIndependentError(sourceErr, withoutConfirmedClosure(deliveryErr))
		}
		if received.err != nil {
			return joinIndependentError(combinedErr, received.err)
		}
		return combinedErr
	case <-ctx.Done():
		return joinIndependentError(combinedErr, fmt.Errorf("run UI session: %w", ctx.Err()))
	}
}

// shutdown cancels and joins the active operation and command receiver.
func (s *Session) shutdown(
	activeCancel context.CancelFunc,
	activeKind operationKind,
	results <-chan operationResult,
	cancelReceiver context.CancelFunc,
	receiverDone <-chan struct{},
) error {
	if activeKind != 0 {
		activeCancel()
	}
	cancelReceiver()
	s.channel.Close()
	var activeErr error
	if activeKind != 0 {
		activeErr = withoutCancellation((<-results).err)
	}
	<-receiverDone
	return activeErr
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

// sessionFailureText adds operation context without replacing the session error.
func sessionFailureText(err error, fallback string) string {
	return fmt.Sprintf("%s: %v", strings.TrimSuffix(fallback, "."), err)
}

// preserveUndeliveredSource keeps an operation source only when its error frame was not delivered.
func preserveUndeliveredSource(sourceErr, deliveryErr error) error {
	if deliveryErr == nil {
		return nil
	}
	return &deliveryFailureError{sourceErr: sourceErr, deliveryErr: deliveryErr}
}

// joinIndependentError keeps the broader chain when either error already contains the other.
func joinIndependentError(current, candidate error) error {
	if candidate == nil {
		return current
	}
	if current == nil || errors.Is(candidate, current) {
		return candidate
	}
	if errors.Is(current, candidate) {
		return current
	}
	return errors.Join(current, candidate)
}

// sendInformation emits one non-terminal command rejection or notification.
func (s *Session) sendInformation(text string) error {
	return s.channel.Send(informationFrame(text))
}

// sendSelectionError emits one fixed error without exposing catalog details.
func (s *Session) sendSelectionError() error {
	return s.channel.Send(errorFrame("Could not change model selection.", false))
}

// sendAuthenticationError emits the failure and a state that permits explicit retry.
// sendSourceError keeps source provenance only when its error frame cannot be delivered.
func (s *Session) sendSourceError(sourceErr error, retryAuthentication bool) error {
	return preserveUndeliveredSource(
		sourceErr,
		s.channel.Send(errorFrame(sourceErr.Error(), retryAuthentication)),
	)
}

func (s *Session) sendAuthenticationError(err error) error {
	if sendErr := s.sendSourceError(err, true); sendErr != nil {
		return sendErr
	}
	return s.sendAvailability(domainui.AvailabilityAuthenticationFailed)
}
