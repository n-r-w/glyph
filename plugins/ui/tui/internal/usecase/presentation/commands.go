package presentation

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/mo"

	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
	tuiinput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"
)

// operationIDTemplate formats process-local operation identifiers.
const operationIDTemplate = "tui-%d"

// commandIntent records an interaction decision before asynchronous work is prepared.
type commandIntent struct {
	// command contains the selected Host action and immutable scalar payload.
	command Command
}

// pendingCommand retains application policy across independently scheduled acknowledgement and terminal input.
type pendingCommand struct {
	// command identifies the initiating interaction.
	command Command
	// dispatchPending retains correlation until command I/O returns to the event loop.
	dispatchPending bool
	// terminal records that the Host terminal notification was already consumed.
	terminal bool
}

// preparedDispatch executes only transport work and never accesses mutable application state.
type preparedDispatch struct {
	// host owns SDK I/O and its active context.
	host Host
	// identifier correlates acknowledgement with the prepared command.
	identifier string
	// command is the immutable operation payload.
	command Command
	// target is the captured foreground identifier for Stop.
	target string
	// failure records an input decision that needs an asynchronous acknowledgement.
	failure error
}

var _ tuiinput.Work = (*preparedDispatch)(nil)

// Execute returns the dispatch result without changing pending-command or interaction state.
func (work *preparedDispatch) Execute() tuiinput.Result {
	err := work.failure
	if err == nil {
		err = work.host.Send(work.identifier, work.command, work.target)
	}
	return tuiinput.Result{ID: work.identifier, Err: err}
}

// Key applies one decoded terminal interaction on the existing event loop.
func (service *Service) Key(key tuiinput.Key) tuiinput.Work {
	model, intent := service.model.updateKey(key)
	service.model = model
	service.publish()
	if intent == nil {
		return nil
	}
	return service.prepareCommand(intent.command)
}

// prepareCommand captures correlation and foreground policy before any background I/O starts.
func (service *Service) prepareCommand(command Command) tuiinput.Work {
	service.sequence++
	identifier := fmt.Sprintf(operationIDTemplate, service.sequence)
	target := ""
	var failure error
	if command.Kind == CommandStop {
		target = service.foreground
		if target == "" {
			failure = errors.New("cancel TUI foreground operation: no operation is active")
		}
	}
	service.pending[identifier] = pendingCommand{command: command, dispatchPending: true, terminal: false}
	if service.foreground == "" && (command.Kind == CommandSubmit || command.Kind == CommandNavigateSessionTree) {
		service.foreground = identifier
	}
	return &preparedDispatch{
		host:       service.host,
		identifier: identifier,
		command:    command,
		target:     target,
		failure:    failure,
	}
}

// Complete applies command-send outcomes on the event loop and reports a local Quit decision.
func (service *Service) Complete(result tuiinput.Result) bool {
	pending, present := service.pending[result.ID]
	if !present {
		return false
	}
	pending.dispatchPending = false
	if result.Err != nil || pending.terminal || pending.command.Kind == CommandQuit {
		delete(service.pending, result.ID)
	} else {
		service.pending[result.ID] = pending
	}
	if result.Err != nil && service.foreground == result.ID {
		service.foreground = ""
	}
	model, quit := service.model.applyEmissionResult(emissionResultMsg{command: pending.command, err: result.Err})
	service.model = model
	service.publish()
	return quit
}

// Notify applies operation correlation and payload transitions only on the event loop.
func (service *Service) Notify(input plugininput.Notification) error {
	if input.Kind == plugininput.NotificationConnection {
		if err := service.applyInput(input.Payload); err != nil {
			return err
		}
		service.publish()
		return nil
	}
	pending, present := service.pending[input.OperationID]
	if !present {
		switch input.Kind {
		case plugininput.NotificationProgress:
			return errors.New("UI progress operation is not tracked")
		case plugininput.NotificationCompleted:
			return errors.New("UI completed operation is not tracked")
		case plugininput.NotificationFailed:
			return errors.New("UI failed operation is not tracked")
		case plugininput.NotificationConnection:
		}
	}
	if input.Kind != plugininput.NotificationProgress {
		pending.terminal = true
		if pending.dispatchPending {
			service.pending[input.OperationID] = pending
		} else {
			delete(service.pending, input.OperationID)
		}
		if service.foreground == input.OperationID {
			service.foreground = ""
		}
	}
	if input.Kind == plugininput.NotificationFailed {
		if !errors.Is(input.Failure, context.Canceled) {
			service.model = service.model.applyEvent(
				operationErrorEvent(pending.command, input.FailureCode, input.Failure),
			)
		}
	} else if err := service.applyInput(input.Payload); err != nil {
		return err
	}
	service.publish()
	return nil
}

// operationErrorEvent chooses the failed interaction's display destination from its initiating command.
func operationErrorEvent(command Command, failureCode string, err error) event {
	if command.Kind == CommandResumeSession {
		return textEvent(eventInformation, err.Error())
	}
	if command.TreeCommand.IsSome() {
		update := newEvent(eventTreeOperationFailed)
		update.treeEvent = mo.Some(treeEvent{
			Tree: mo.None[SessionTree](), NavigationStatus: TreeNavigationUnspecified,
			SessionInfo: mo.None[SessionInfo](), RestoredTranscript: nil, NextInput: mo.None[string](),
			Issues: nil, FailureMessage: mo.Some(err.Error()), AddedEntry: mo.None[TreeEntry](),
		})
		return update
	}
	update := textEvent(eventError, err.Error())
	update.FailureCode = failureCode
	return update
}
