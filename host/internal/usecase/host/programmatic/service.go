package programmatic

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/mo"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// ErrCorrelationRequired reports a command that cannot receive a correlated response.
var ErrCorrelationRequired = errors.New("correlation ID is required")

// Service coordinates one Programmatic Control connection.
type Service struct {
	coordinator     Coordinator
	modelCatalog    ModelCatalog
	stateSnapshot   func() run.State
	historySnapshot func() []agent.HistoryEntry
	delivery        *Delivery
}

var _ controller.HostSession = (*Service)(nil)

// New creates one Programmatic Control session over a synchronous delivery router.
func New(
	coordinator Coordinator,
	modelCatalog ModelCatalog,
	stateSnapshot func() run.State,
	historySnapshot func() []agent.HistoryEntry,
	delivery *Delivery,
) *Service {
	return &Service{
		coordinator: coordinator, modelCatalog: modelCatalog, stateSnapshot: stateSnapshot,
		historySnapshot: historySnapshot, delivery: delivery,
	}
}

// Handle executes one transport-independent command and returns its single response.
func (s *Service) Handle(
	ctx context.Context,
	command controller.Command,
) (controller.Response, controller.Operation, error) {
	current, rejection, err := s.preflight(command)
	if err != nil {
		return controller.Response{}, nil, err
	}
	if rejection != nil {
		return *rejection, nil, nil
	}

	switch command.Kind {
	case controller.CommandAbort:
		response, abortErr := s.abort(command.CorrelationID, current)
		return response, nil, abortErr
	case controller.CommandGetRunState:
		return s.runState(command.CorrelationID, current), nil, nil
	case controller.CommandGetMessages:
		return s.messages(command.CorrelationID), nil, nil
	case controller.CommandGetModels:
		return s.models(command.CorrelationID), nil, nil
	case controller.CommandSelectModel:
		return s.selectModel(ctx, command), nil, nil
	case controller.CommandSelectReasoningChoice:
		return s.selectReasoningChoice(command), nil, nil
	case controller.CommandUserRequest:
	case controller.CommandUnspecified:
		return s.rejection(command, controller.RejectionInvalidArgument, "invalid command payload"), nil, nil
	}

	runID, err := s.coordinator.PrepareRun()
	if err != nil {
		return s.runPreparationRejected(command)
	}

	userText, _ := command.UserText.Get()
	runContext, cancel := context.WithCancel(ctx)
	operation := &activeRun{
		delivery:      s.delivery,
		correlationID: command.CorrelationID,
		runID:         runID,
		coordinator:   s.coordinator,
		userText:      userText,
		runContext:    runContext,
		cancel:        cancel,
		events:        make(chan controller.AgentEvent),
		streamDone:    make(chan struct{}),
		done:          make(chan struct{}),
		state:         operationAccepted,
		streamStopped: false,
		err:           nil,
	}
	if !s.delivery.reserve(operation) {
		cancel()
		close(operation.events)
		close(operation.streamDone)
		close(operation.done)
		return s.rejection(command, controller.RejectionBusy, "a run is active"), nil, nil
	}

	return emptyResponse(command.CorrelationID, controller.ResponseUserRequestAccepted), operation, nil
}

// CancelAndWait cancels and joins work owned by the controller connection.
func (s *Service) CancelAndWait(context.Context) error {
	return s.delivery.cancelAndWaitAll()
}

func (s *Service) abort(correlationID string, active *activeRun) (controller.Response, error) {
	if err := s.delivery.cancelAndWait(active); err != nil {
		return controller.Response{}, fmt.Errorf("abort Programmatic Control run: %w", err)
	}
	return emptyResponse(correlationID, controller.ResponseAbortCompleted), nil
}

func (s *Service) runState(correlationID string, active *activeRun) controller.Response {
	state := s.stateSnapshot()
	publicState := controller.RunStateIdle
	if active != nil || state.Status == run.StatusRunning || state.Status == run.StatusAwaitingSettlement {
		publicState = controller.RunStateRunning
	}
	activeCorrelationID := mo.None[string]()
	if publicState == controller.RunStateRunning && active != nil {
		activeCorrelationID = mo.Some(active.correlationID)
	}
	response := emptyResponse(correlationID, controller.ResponseRunState)
	response.State = mo.Some(controller.RunStateResult{
		State:               publicState,
		ActiveCorrelationID: activeCorrelationID,
	})
	return response
}

func (s *Service) messages(correlationID string) controller.Response {
	response := emptyResponse(correlationID, controller.ResponseMessages)
	response.Messages = mapHistory(s.historySnapshot())
	return response
}

func (s *Service) models(correlationID string) controller.Response {
	response := emptyResponse(correlationID, controller.ResponseModels)
	response.Models = mo.Some(controller.ModelsResult{
		Models:          s.modelCatalog.Models(),
		ActiveSelection: mo.Some(s.modelCatalog.Selection()),
	})
	return response
}

func (s *Service) selectModel(ctx context.Context, command controller.Command) controller.Response {
	providerID, _ := command.ProviderID.Get()
	modelID, _ := command.ModelID.Get()
	selection, err := s.modelCatalog.SelectModel(ctx, providerID, modelID)
	if err != nil {
		return s.selectionRejected(command, err)
	}
	response := emptyResponse(command.CorrelationID, controller.ResponseModelSelection)
	response.Selection = mo.Some(selection)
	return response
}

func (s *Service) selectReasoningChoice(command controller.Command) controller.Response {
	reasoningChoice, _ := command.ReasoningChoice.Get()
	selection, err := s.modelCatalog.SelectReasoningChoice(reasoningChoice)
	if err != nil {
		return s.selectionRejected(command, err)
	}
	response := emptyResponse(command.CorrelationID, controller.ResponseModelSelection)
	response.Selection = mo.Some(selection)
	return response
}

func (s *Service) selectionRejected(command controller.Command, err error) controller.Response {
	var selectionFailure SelectionFailure
	if !errors.As(err, &selectionFailure) {
		return s.rejection(command, controller.RejectionInternal, "model selection failed")
	}
	code := controller.RejectionInternal
	switch SelectionCode(selectionFailure.SelectionCode()) {
	case SelectionNotFound:
		code = controller.RejectionNotFound
	case SelectionReasoningUnsupported:
		code = controller.RejectionReasoningUnsupported
	case SelectionCredentialUnavailable:
		code = controller.RejectionCredentialUnavailable
	default:
	}
	return s.rejection(command, code, selectionFailure.Error())
}

func (s *Service) preflight(
	command controller.Command,
) (*activeRun, *controller.Response, error) {
	if command.CorrelationID == "" {
		return nil, nil, ErrCorrelationRequired
	}
	if invalidCommand(command) {
		response := s.rejection(command, controller.RejectionInvalidArgument, "invalid command payload")
		return nil, &response, nil
	}
	active := s.delivery.activeSnapshot()
	if active != nil && active.correlationID == command.CorrelationID {
		response := s.rejection(command, controller.RejectionCorrelationInUse, "correlation ID is active")
		return active, &response, nil
	}
	if command.Kind == controller.CommandUserRequest && active != nil {
		response := s.rejection(command, controller.RejectionBusy, "a run is active")
		return active, &response, nil
	}
	if command.Kind == controller.CommandAbort && active == nil {
		response := s.rejection(command, controller.RejectionNoActiveRun, "no run is active")
		return nil, &response, nil
	}
	return active, nil, nil
}

func invalidCommand(command controller.Command) bool {
	switch command.Kind {
	case controller.CommandUserRequest:
		return invalidUserRequest(command)
	case controller.CommandAbort, controller.CommandGetRunState,
		controller.CommandGetMessages, controller.CommandGetModels:
		return command.UserText.IsSome() || hasModelArguments(command)
	case controller.CommandSelectModel:
		return invalidModelSelection(command)
	case controller.CommandSelectReasoningChoice:
		return invalidReasoningSelection(command)
	case controller.CommandUnspecified:
		return true
	}
	return true
}

// invalidUserRequest validates the payload selected by a user-request discriminator.
func invalidUserRequest(command controller.Command) bool {
	userText, ok := command.UserText.Get()
	return !ok || strings.TrimSpace(userText) == "" || hasModelArguments(command)
}

// invalidModelSelection validates the payload selected by a model-selection discriminator.
func invalidModelSelection(command controller.Command) bool {
	providerID, providerPresent := command.ProviderID.Get()
	modelID, modelPresent := command.ModelID.Get()
	return command.UserText.IsSome() || !providerPresent || providerID == "" ||
		!modelPresent || modelID == "" || command.ReasoningChoice.IsSome()
}

// invalidReasoningSelection validates the payload selected by a reasoning-selection discriminator.
func invalidReasoningSelection(command controller.Command) bool {
	reasoningChoice, reasoningPresent := command.ReasoningChoice.Get()
	return command.UserText.IsSome() || command.ProviderID.IsSome() || command.ModelID.IsSome() ||
		!reasoningPresent || !validReasoningChoice(reasoningChoice)
}

func hasModelArguments(command controller.Command) bool {
	return command.ProviderID.IsSome() || command.ModelID.IsSome() || command.ReasoningChoice.IsSome()
}

func validReasoningChoice(level model.ReasoningChoice) bool {
	switch level {
	case model.ReasoningChoiceOff, model.ReasoningChoiceOn, model.ReasoningChoiceMinimal, model.ReasoningChoiceLow,
		model.ReasoningChoiceMedium, model.ReasoningChoiceHigh, model.ReasoningChoiceXHigh,
		model.ReasoningChoiceMax:
		return true
	default:
		return false
	}
}

func filterRunError(outcome agent.RunOutcome, runErr error) error {
	if outcome != agent.RunOutcomeAborted {
		return runErr
	}
	return removeCancellation(runErr)
}

func removeCancellation(err error) error {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		filtered := lo.FilterMap(joined.Unwrap(), func(child error, _ int) (error, bool) {
			childErr := removeCancellation(child)
			return childErr, childErr != nil
		})
		return errors.Join(filtered...)
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		child := wrapped.Unwrap()
		filtered := removeCancellation(child)
		if filtered == nil {
			return nil
		}
		if errors.Is(err, context.Canceled) {
			return filtered
		}
		return err
	}
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func emptyResponse(correlationID string, kind controller.ResponseKind) controller.Response {
	return controller.Response{
		CorrelationID: correlationID,
		Kind:          kind,
		State:         mo.None[controller.RunStateResult](),
		Messages:      nil,
		Models:        mo.None[controller.ModelsResult](),
		Selection:     mo.None[model.Selection](),
		Rejection:     mo.None[controller.Rejection](),
	}
}

// runPreparationRejected keeps run ID allocation failure inside the command response.
func (s *Service) runPreparationRejected(
	command controller.Command,
) (controller.Response, controller.Operation, error) {
	return s.rejection(
		command, controller.RejectionInternal, "Host run ID allocation failed",
	), nil, nil
}

func (s *Service) rejection(
	command controller.Command,
	code controller.RejectionCode,
	message string,
) controller.Response {
	response := emptyResponse(command.CorrelationID, controller.ResponseRejected)
	response.Rejection = mo.Some(controller.Rejection{Command: command.Kind, Code: code, Message: message})
	return response
}
