// Package output delivers Programmatic run and connection events through one ordered writer.
package output

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/n-r-w/glyph/host/internal/usecase/host/events"
	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessions"

	"github.com/samber/mo"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	host "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	"github.com/n-r-w/glyph/internal/operation"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// activeOutput correlates one prepared run with its operation-owned reporter.
type activeOutput struct {
	// operationID identifies the public operation.
	operationID string
	// runID identifies the Core run.
	runID string
	// reporter is attached only while prepared application work executes.
	reporter mo.Option[operation.Reporter[controller.OperationProgress]]
	// failed stops further progress after delivery fails and cancels application work.
	failed bool
}

// Service owns run-output correlation and the connection's attached writer.
type Service struct {
	// mutex protects active output and writer binding.
	mutex sync.Mutex
	// active is the sole authoritative run-output association.
	active *activeOutput
	// writer is also used by the input controller's operation delivery.
	writer *operation.Writer[*programmaticv1.OpenResponse]
}

var (
	_ host.RunOutput                   = (*Service)(nil)
	_ controller.ConnectionOutput      = (*Service)(nil)
	_ events.ClientDelivery            = (*Service)(nil)
	_ runcontrol.SettledDelivery       = (*Service)(nil)
	_ extensionruntime.FailureReporter = (*Service)(nil)
	_ lifecycle.IssueDelivery          = (*Service)(nil)
	_ sessions.EntryPublisher          = (*Service)(nil)
)

// New creates an output owner before the connection is opened.
func New() *Service {
	return &Service{mutex: sync.Mutex{}, active: nil, writer: nil}
}

// Reserve associates the public operation with its prepared run before acceptance.
func (s *Service) Reserve(operationID, runID string) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.active != nil {
		return false
	}
	s.active = &activeOutput{
		operationID: operationID, runID: runID,
		reporter: mo.None[operation.Reporter[controller.OperationProgress]](), failed: false,
	}
	return true
}

// ActiveOperation returns the operation associated with active output.
func (s *Service) ActiveOperation() string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.active == nil {
		return ""
	}
	return s.active.operationID
}

// BindProgress attaches an operation-scoped reporter without owning application execution.
func (s *Service) BindProgress(runID string, reporter operation.Reporter[controller.OperationProgress]) func() {
	s.mutex.Lock()
	if s.active != nil && s.active.runID == runID {
		s.active.reporter = mo.Some(reporter)
	}
	s.mutex.Unlock()
	return func() {
		s.mutex.Lock()
		defer s.mutex.Unlock()
		if s.active != nil && s.active.runID == runID {
			s.active.reporter = mo.None[operation.Reporter[controller.OperationProgress]]()
		}
	}
}

// CancelPrepared releases output correlation only for a run that never started.
func (s *Service) CancelPrepared(runID string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.active != nil && s.active.runID == runID {
		s.active = nil
	}
}

// DeliverAgent maps one agent fact and delivers it through the bound operation reporter.
func (s *Service) DeliverAgent(ctx context.Context, event agent.Event) error {
	s.mutex.Lock()
	active := s.active
	if active == nil || active.runID != event.RunID {
		s.mutex.Unlock()
		return fmt.Errorf("deliver Programmatic Control agent event: inactive Host run %q", event.RunID)
	}
	if active.failed {
		s.mutex.Unlock()
		return nil
	}
	operationID := active.operationID
	reporter, present := active.reporter.Get()
	s.mutex.Unlock()
	if !present {
		return errors.New("deliver Programmatic Control agent event: operation reporter is not bound")
	}
	mapped, err := mapAgentEvent(event)
	if err != nil {
		return fmt.Errorf("deliver Programmatic Control agent event: %w", err)
	}
	mapped.OperationID = operationID
	if err = ctx.Err(); err != nil {
		return context.Cause(ctx)
	}
	if err = reporter.Report(controller.OperationProgress{
		AgentEvent: mo.Some(mapped), TreeNavigation: mo.None[controller.TreeNavigationProgress](),
	}); err != nil {
		s.mutex.Lock()
		if s.active == active {
			active.failed = true
		}
		s.mutex.Unlock()
		return fmt.Errorf("deliver Programmatic Control agent event: %w", err)
	}
	return nil
}

// DeliverSettled clears output correlation at the dispatcher settlement step.
func (s *Service) DeliverSettled(_ context.Context, runID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.active == nil || s.active.runID != runID {
		return fmt.Errorf("deliver Programmatic Control settlement: inactive Host run %q", runID)
	}
	s.active = nil
	return nil
}

// BindWriter attaches the same ordered writer used for operation events.
func (s *Service) BindWriter(writer *operation.Writer[*programmaticv1.OpenResponse]) func() {
	s.mutex.Lock()
	s.writer = writer
	s.mutex.Unlock()
	return func() {
		s.mutex.Lock()
		defer s.mutex.Unlock()
		if s.writer == writer {
			s.writer = nil
		}
	}
}
