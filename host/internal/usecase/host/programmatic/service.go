package programmatic

import (
	"context"
	"errors"
	"fmt"
	"strings"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// ErrCorrelationRequired reports a command that cannot receive a correlated response.
var ErrCorrelationRequired = errors.New("correlation ID is required")

// Service coordinates one Programmatic Control connection.
type Service struct {
	coordinator     Coordinator
	stateSnapshot   func() run.State
	historySnapshot func() []agent.HistoryEntry
	delivery        *Delivery
}

var _ controller.HostSession = (*Service)(nil)

// New creates one Programmatic Control session over a synchronous delivery router.
func New(
	coordinator Coordinator,
	stateSnapshot func() run.State,
	historySnapshot func() []agent.HistoryEntry,
	delivery *Delivery,
) *Service {
	return &Service{
		coordinator: coordinator, stateSnapshot: stateSnapshot,
		historySnapshot: historySnapshot, delivery: delivery,
	}
}

// Handle executes one transport-independent command.
func (s *Service) Handle(ctx context.Context, command controller.Command) error {
	current, rejected, err := s.preflight(ctx, command)
	if rejected || err != nil {
		return err
	}
	switch command.Kind {
	case controller.CommandAbort:
		return s.abort(ctx, command.CorrelationID, current)
	case controller.CommandGetRunState:
		return s.sendState(ctx, command.CorrelationID, current)
	case controller.CommandGetMessages:
		return s.sendMessages(ctx, command.CorrelationID)
	case controller.CommandUserRequest:
	case controller.CommandUnspecified:
		return s.reject(ctx, command, controller.RejectionInvalidArgument, "invalid command payload")
	}

	runID, err := s.coordinator.PrepareRun()
	if err != nil {
		return s.reject(ctx, command, controller.RejectionInternal, "Host run ID allocation failed")
	}
	runContext, cancel := context.WithCancel(ctx)
	active := &activeRun{
		correlationID: command.CorrelationID,
		runID:         runID,
		cancel:        cancel,
		done:          make(chan struct{}),
		err:           nil,
	}
	if !s.delivery.reserve(active) {
		cancel()
		return s.reject(ctx, command, controller.RejectionBusy, "a run is active")
	}
	if sendErr := s.delivery.sendResponse(ctx, emptyResponse(
		command.CorrelationID,
		controller.ResponseUserRequestAccepted,
	)); sendErr != nil {
		s.delivery.release(active)
		cancel()
		return fmt.Errorf("deliver Programmatic Control response: %w", sendErr)
	}
	go func() {
		outcome, runErr := s.coordinator.RunPrepared(runContext, runID, command.UserText)
		s.delivery.finish(active, filterRunError(outcome, runErr))
	}()
	return nil
}

// CancelAndWait cancels and joins active work owned by the controller connection.
func (s *Service) CancelAndWait(context.Context) error {
	return s.delivery.cancelAndWaitAll()
}

func (s *Service) abort(ctx context.Context, correlationID string, active *activeRun) error {
	if err := s.delivery.cancelAndWait(active); err != nil {
		return fmt.Errorf("abort Programmatic Control run: %w", err)
	}
	if err := s.delivery.sendResponse(ctx, emptyResponse(
		correlationID,
		controller.ResponseAbortCompleted,
	)); err != nil {
		return fmt.Errorf("deliver Programmatic Control abort: %w", err)
	}
	return nil
}

func (s *Service) sendState(ctx context.Context, correlationID string, active *activeRun) error {
	state := s.stateSnapshot()
	publicState := controller.RunStateIdle
	if active != nil || state.Status == run.StatusRunning || state.Status == run.StatusAwaitingSettlement {
		publicState = controller.RunStateRunning
	}
	activeCorrelationID := ""
	if publicState == controller.RunStateRunning && active != nil {
		activeCorrelationID = active.correlationID
	}
	response := emptyResponse(correlationID, controller.ResponseRunState)
	response.State = controller.RunStateResult{State: publicState, ActiveCorrelationID: activeCorrelationID}
	if err := s.delivery.sendResponse(ctx, response); err != nil {
		return fmt.Errorf("deliver Programmatic Control run state: %w", err)
	}
	return nil
}

func (s *Service) sendMessages(ctx context.Context, correlationID string) error {
	response := emptyResponse(correlationID, controller.ResponseMessages)
	response.Messages = mapHistory(s.historySnapshot())
	if err := s.delivery.sendResponse(ctx, response); err != nil {
		return fmt.Errorf("deliver Programmatic Control messages: %w", err)
	}
	return nil
}

func (s *Service) preflight(
	ctx context.Context,
	command controller.Command,
) (*activeRun, bool, error) {
	if command.CorrelationID == "" {
		return nil, false, ErrCorrelationRequired
	}
	if invalidCommand(command) {
		return nil, true, s.reject(ctx, command, controller.RejectionInvalidArgument, "invalid command payload")
	}
	active := s.delivery.activeSnapshot()
	if active != nil && active.correlationID == command.CorrelationID {
		return active, true, s.reject(
			ctx, command, controller.RejectionCorrelationInUse, "correlation ID is active",
		)
	}
	if command.Kind == controller.CommandUserRequest && active != nil {
		return active, true, s.reject(ctx, command, controller.RejectionBusy, "a run is active")
	}
	if command.Kind == controller.CommandAbort && active == nil {
		return nil, true, s.reject(ctx, command, controller.RejectionNoActiveRun, "no run is active")
	}
	return active, false, nil
}

func invalidCommand(command controller.Command) bool {
	switch command.Kind {
	case controller.CommandUserRequest:
		return strings.TrimSpace(command.UserText) == ""
	case controller.CommandAbort, controller.CommandGetRunState, controller.CommandGetMessages:
		return command.UserText != ""
	case controller.CommandUnspecified:
		return true
	}
	return true
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
		filtered := make([]error, 0, len(joined.Unwrap()))
		for _, child := range joined.Unwrap() {
			if childErr := removeCancellation(child); childErr != nil {
				filtered = append(filtered, childErr)
			}
		}
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
		State: controller.RunStateResult{
			State: controller.RunStateUnspecified, ActiveCorrelationID: "",
		},
		Messages: nil,
		Rejection: controller.Rejection{
			Command: controller.CommandUnspecified, Code: controller.RejectionUnspecified, Message: "",
		},
	}
}

func (s *Service) reject(
	ctx context.Context,
	command controller.Command,
	code controller.RejectionCode,
	message string,
) error {
	response := emptyResponse(command.CorrelationID, controller.ResponseRejected)
	response.Rejection = controller.Rejection{Command: command.Kind, Code: code, Message: message}
	if err := s.delivery.sendResponse(ctx, response); err != nil {
		return fmt.Errorf("deliver Programmatic Control rejection: %w", err)
	}
	return nil
}
