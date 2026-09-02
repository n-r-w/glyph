package programmatic

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/internal/operation"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// streamDelivery maps typed operation events into the Programmatic writer queue.
type streamDelivery struct {
	// context bounds terminal delivery acknowledgement waits to the connection lifetime.
	context context.Context
	// writer owns ordered transport sends.
	writer *operation.Writer[*programmaticv1.OpenResponse]
	// registry tracks request kinds and cancellation order.
	registry *targetRegistry
	// fail reports connection-fatal delivery failures.
	fail func(error)
}

var _ operation.Delivery[AgentEvent, Response] = (*streamDelivery)(nil)

// Accepted queues operation acceptance and returns its delivery acknowledgement.
func (d *streamDelivery) Accepted(id string) (*operation.Acknowledgement, error) {
	event := new(programmaticv1.HostEvent)
	event.SetAccepted(new(operationv1.Accepted))
	return d.enqueueAcknowledged(id, event)
}

// Running queues the operation running event.
func (d *streamDelivery) Running(id string) error {
	event := new(programmaticv1.HostEvent)
	event.SetRunning(new(operationv1.Running))
	return d.enqueue(id, event)
}

// Progress maps and queues one agent-run progress event.
func (d *streamDelivery) Progress(id string, progress AgentEvent) error {
	mapped, err := mapEvent(progress)
	if err != nil {
		return d.failed(fmt.Errorf("map Programmatic progress: %w", err))
	}
	payload := new(programmaticv1.HostProgress)
	payload.SetAgentEvent(mapped)
	event := new(programmaticv1.HostEvent)
	event.SetProgress(payload)
	return d.enqueue(id, event)
}

// Terminal maps and queues exactly one operation terminal event.
func (d *streamDelivery) Terminal(id string, outcome operation.Outcome[Response]) (*operation.Acknowledgement, error) {
	event := new(programmaticv1.HostEvent)
	switch outcome.State() {
	case operation.TerminalStateCompleted:
		result, present := outcome.Result()
		if !present {
			return nil, d.failed(errors.New("map Programmatic completion: result is absent"))
		}
		if !completionMatches(d.registry.kind(id), result.Kind) {
			return nil, d.failed(
				status.Error(codes.FailedPrecondition, "Programmatic completed payload does not match request kind"),
			)
		}
		completed, err := mapResponse(result)
		if err != nil {
			return nil, d.failed(fmt.Errorf("map Programmatic completion: %w", err))
		}
		event.SetCompleted(completed)
	case operation.TerminalStateCanceled:
		event.SetCanceled(new(operationv1.Canceled))
	case operation.TerminalStateFailed:
		code := outcome.Code()
		if code == "" {
			code = FailureCodeInternal
		}
		failed := new(operationv1.Failed)
		failed.SetCode(code)
		event.SetFailed(failed)
		slog.Error("Programmatic operation failed",
			"operation_id", id,
			"operation_kind", d.registry.kind(id),
			"peer_kind", "controller",
			"failure_code", code,
			"error", outcome.Err(),
		)
	default:
		return nil, d.failed(fmt.Errorf("map Programmatic terminal: unknown state %d", outcome.State()))
	}
	acknowledgement, err := d.enqueueAcknowledged(id, event)
	if err != nil {
		return nil, err
	}
	if err = acknowledgement.Wait(d.context); err != nil {
		return nil, d.failed(err)
	}
	d.registry.finish(id, outcome.State())
	return acknowledgement, nil
}

// completionMatches verifies the completed payload for one tracked request kind.
//
//nolint:gocyclo // The switch exhaustively pairs each request kind with its completed payload.
func completionMatches(command CommandKind, response ResponseKind) bool {
	switch command {
	case CommandUserRequest:
		return response == ResponseUserRequestCompleted
	case CommandCancel:
		return response == ResponseCancelCompleted
	case CommandGetRunState:
		return response == ResponseRunState
	case CommandGetMessages:
		return response == ResponseMessages
	case CommandGetModels:
		return response == ResponseModels
	case CommandSelectModel, CommandSelectReasoningChoice:
		return response == ResponseModelSelection
	case CommandCreateSession, CommandResumeSession, CommandSetSessionName, CommandGetSessionInfo:
		return response == ResponseSessionInfo
	case CommandListSessions:
		return response == ResponseSessions
	case CommandGetSessionEntries:
		return response == ResponseSessionEntries
	case CommandGetSessionStats:
		return response == ResponseSessionStats
	case CommandGetSessionTree:
		return response == ResponseSessionTree
	case CommandNavigateSessionTree:
		return response == ResponseSessionTreeNavigation
	case CommandForkSession:
		return response == ResponseForkSession
	case CommandCloneSession:
		return response == ResponseCloneSession
	case CommandSetEntryLabel:
		return response == ResponseSetEntryLabel
	case CommandUnspecified:
		return false
	default:
		return false
	}
}

// reject queues one per-request rejection without creating an operation.
func (d *streamDelivery) reject(id, code string) error {
	rejected := new(operationv1.Rejected)
	rejected.SetCode(code)
	event := new(programmaticv1.HostEvent)
	event.SetRejected(rejected)
	return d.enqueue(id, event)
}

// closeConnection queues a Host-requested connection close.
func (d *streamDelivery) closeConnection() error {
	response := new(programmaticv1.OpenResponse)
	response.SetClose(new(operationv1.CloseConnection))
	if err := d.writer.Enqueue(response); err != nil {
		return d.failed(err)
	}
	return nil
}

// enqueue queues one operation event without waiting for transport delivery.
func (d *streamDelivery) enqueue(id string, event *programmaticv1.HostEvent) error {
	response := new(programmaticv1.OpenResponse)
	response.SetOperationId(id)
	response.SetEvent(event)
	if err := d.writer.Enqueue(response); err != nil {
		return d.failed(err)
	}
	return nil
}

// enqueueAcknowledged queues one operation event with a delivery acknowledgement.
func (d *streamDelivery) enqueueAcknowledged(
	id string,
	event *programmaticv1.HostEvent,
) (*operation.Acknowledgement, error) {
	response := new(programmaticv1.OpenResponse)
	response.SetOperationId(id)
	response.SetEvent(event)
	acknowledgement, err := d.writer.EnqueueAcknowledged(response)
	if err != nil {
		return nil, d.failed(err)
	}
	return acknowledgement, nil
}

// failed maps bounded writer failure and starts connection failure once.
func (d *streamDelivery) failed(err error) error {
	if errors.Is(err, operation.ErrQueueFull) {
		err = fmt.Errorf("programmatic delivery overflow: %w", err)
	}
	d.fail(err)
	return err
}
