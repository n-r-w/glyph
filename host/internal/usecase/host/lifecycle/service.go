package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/usecase/host/events"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelselection"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	"github.com/n-r-w/glyph/internal/errtree"
)

const (
	// IssueCodeObserverError identifies an ordinary lifecycle observer failure.
	IssueCodeObserverError = "OBSERVER_ERROR"
)

// Kind identifies one lifecycle observer registration.
type Kind uint8

const (
	// KindAgentStart observes agent start.
	KindAgentStart Kind = iota + 1
	// KindAgentEnd observes agent end.
	KindAgentEnd
	// KindAgentSettled observes Host settlement.
	KindAgentSettled
	// KindTurnStart observes turn start.
	KindTurnStart
	// KindTurnEnd observes turn end.
	KindTurnEnd
	// KindMessageStart observes message start.
	KindMessageStart
	// KindMessageUpdate observes message content transitions.
	KindMessageUpdate
	// KindMessageEnd observes message end.
	KindMessageEnd
	// KindToolExecutionStart observes tool execution start.
	KindToolExecutionStart
	// KindToolExecutionUpdate observes tool execution progress.
	KindToolExecutionUpdate
	// KindToolExecutionEnd observes terminal tool execution.
	KindToolExecutionEnd
	// KindModelSelection observes committed provider or model selection changes.
	KindModelSelection
	// KindReasoningSelection observes committed reasoning selection changes.
	KindReasoningSelection
)

// Handler identifies one accepted lifecycle observer.
type Handler struct {
	// ExtensionID identifies the owning extension.
	ExtensionID string
	// HandlerID identifies the observer within the extension.
	HandlerID string
	// Kind identifies the observed lifecycle group.
	Kind Kind
}

// Issue contains one nonterminal observer failure.
type Issue struct {
	// ExtensionID identifies the owning extension.
	ExtensionID string
	// HandlerID identifies the failed observer.
	HandlerID string
	// Code is the stable issue code.
	Code string
	// Err preserves the complete observer cause.
	Err error
}

// Service owns lifecycle registrations and synchronous observer policy.
type Service struct {
	// runtime invokes available extension processes.
	runtime Runtime
	// contexts issues delivery-time session bindings.
	contexts ContextIssuer
	// issues delivers observer failures to the connected client.
	issues IssueDelivery
	// mutex protects accepted registrations.
	mutex sync.RWMutex
	// handlers retains accepted registration order.
	handlers []Handler
}

var (
	_ startup.LifecycleRegistrar = (*Service)(nil)
	_ events.Observer            = (*Service)(nil)
	_ modelselection.Observer    = (*Service)(nil)
)

// New creates an empty lifecycle owner.
func New(runtime Runtime, contexts ContextIssuer) *Service {
	return &Service{runtime: runtime, contexts: contexts, issues: nil, mutex: sync.RWMutex{}, handlers: nil}
}

// BindIssueDelivery installs the mode-specific ordered client issue recipient.
func (s *Service) BindIssueDelivery(delivery IssueDelivery) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.issues = delivery
}

// ValidateLifecycleHandlers validates one extension's lifecycle registrations.
func (s *Service) ValidateLifecycleHandlers(
	registration startup.PendingRegistration,
) ([]startup.AcceptedHandler, error) {
	accepted := make([]startup.AcceptedHandler, 0, len(registration.Handlers))
	for _, handler := range registration.Handlers {
		if !handler.Present || strings.TrimSpace(handler.ID) == "" {
			return nil, errors.New("handler ID is empty")
		}
		if _, valid := lifecycleKind(handler.Kind); !valid {
			return nil, fmt.Errorf("handler %q has unknown kind %d", handler.ID, handler.Kind)
		}
		accepted = append(accepted, startup.AcceptedHandler{ID: handler.ID, Kind: handler.Kind})
	}
	return accepted, nil
}

// CommitLifecycleHandlers publishes handlers from registrations accepted by every startup validator.
func (s *Service) CommitLifecycleHandlers(registrations []startup.AcceptedRegistration) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for _, registration := range registrations {
		for _, handler := range registration.Handlers {
			kind, valid := lifecycleKind(handler.Kind)
			if !valid {
				continue
			}
			s.handlers = append(s.handlers, Handler{ExtensionID: registration.ID, HandlerID: handler.ID, Kind: kind})
		}
	}
}

// Observe delivers one Agent Core event to matching observers.
func (s *Service) Observe(ctx context.Context, event agent.Event) error {
	kind, observed := eventKind(event.Type)
	if !observed {
		return nil
	}
	return s.observe(ctx, kind, Event{
		Agent: event, Settled: false, Selection: modelselection.SelectionChange{},
		SelectionEvent: false, ReasoningSelection: false,
	})
}

// ObserveSettled delivers Host settlement to matching observers.
func (s *Service) ObserveSettled(ctx context.Context, runID string) error {
	return s.observe(ctx, KindAgentSettled, Event{
		Agent: settledSource(runID), Settled: true, Selection: modelselection.SelectionChange{},
		SelectionEvent: false, ReasoningSelection: false,
	})
}

// selectionObservers captures eligible observers in reasoning-then-model order before any invocation.
func (s *Service) selectionObservers(change modelselection.SelectionChange) ([]Handler, IssueDelivery) {
	var kinds []Kind
	if change.Preceding.ReasoningChoice != change.Committed.ReasoningChoice {
		kinds = append(kinds, KindReasoningSelection)
	}
	if change.Preceding.Provider != change.Committed.Provider || change.Preceding.Model != change.Committed.Model {
		kinds = append(kinds, KindModelSelection)
	}
	return s.captureObservers(kinds)
}

// captureObservers fixes eligible participants before dispatching any observer in the chain.
func (s *Service) captureObservers(kinds []Kind) ([]Handler, IssueDelivery) {
	s.mutex.RLock()
	registered := slices.Clone(s.handlers)
	delivery := s.issues
	s.mutex.RUnlock()
	availability := make(map[string]bool)
	handlers := make([]Handler, 0, len(registered))
	for _, kind := range kinds {
		for _, handler := range registered {
			if handler.Kind != kind {
				continue
			}
			available, checked := availability[handler.ExtensionID]
			if !checked {
				available = s.runtime.HandlerRuntimeAvailable(handler.ExtensionID)
				availability[handler.ExtensionID] = available
			}
			if available {
				handlers = append(handlers, handler)
			}
		}
	}
	return handlers, delivery
}

// ObserveSelection delivers a committed selection change and stops after the first selected observer failure.
func (s *Service) ObserveSelection(
	ctx context.Context,
	change modelselection.SelectionChange,
) ([]modelselection.Issue, error) {
	handlers, delivery := s.selectionObservers(change)
	result := make([]modelselection.Issue, 0)
	for _, handler := range handlers {
		issue, deliveryErr, failure := s.invokeObserver(ctx, handler, delivery, Event{
			Agent: agent.Event{}, Settled: false, Selection: change, SelectionEvent: true,
			ReasoningSelection: handler.Kind == KindReasoningSelection,
		})
		if failure == nil {
			continue
		}
		if issue.Err == nil {
			return result, failure
		}
		result = append(result, modelselection.Issue{
			ExtensionID: handler.ExtensionID, HandlerID: handler.HandlerID,
			Code: modelselection.IssueCodeObserverError, Err: issue.Err,
		})
		if deliveryErr != nil {
			result = append(result, modelselection.Issue{
				ExtensionID: handler.ExtensionID, HandlerID: handler.HandlerID,
				Code: modelselection.IssueCodeDeliveryFailed, Err: deliveryErr,
			})
		}
		return result, failure
	}
	return result, nil
}

// observe invokes one matching observer chain outside service state locks.
func (s *Service) observe(ctx context.Context, kind Kind, event Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	handlers, delivery := s.captureObservers([]Kind{kind})
	for _, handler := range handlers {
		if _, _, failure := s.invokeObserver(ctx, handler, delivery, event); failure != nil {
			return failure
		}
	}
	return nil
}

// invokeObserver applies selected-participant failure policy and returns causes for caller-specific issue projection.
func (s *Service) invokeObserver(
	ctx context.Context,
	handler Handler,
	delivery IssueDelivery,
	event Event,
) (issue Issue, deliveryErr, failure error) {
	if err := ctx.Err(); err != nil {
		return Issue{}, nil, err
	}
	binding, err := s.contexts.IssueContext(handler.ExtensionID)
	unavailable := false
	switch {
	case err == nil:
		unavailable, err = s.runtime.ObserveLifecycle(ctx, handler.ExtensionID, handler.HandlerID, binding, event)
	case event.SelectionEvent:
		err = fmt.Errorf("issue selection lifecycle context for extension %q: %w", handler.ExtensionID, err)
	default:
		err = fmt.Errorf("issue lifecycle context for extension %q: %w", handler.ExtensionID, err)
	}
	if unavailable && err == nil {
		if event.SelectionEvent {
			err = fmt.Errorf(
				"selection observer %q in extension %q became unavailable",
				handler.HandlerID,
				handler.ExtensionID,
			)
		} else {
			err = fmt.Errorf(
				"lifecycle observer %q in extension %q became unavailable",
				handler.HandlerID,
				handler.ExtensionID,
			)
		}
	}
	if err == nil {
		return Issue{}, nil, nil
	}
	if !unavailable && ctx.Err() != nil && errtree.IsCurrentCancellation(err, func(cause error) bool {
		return errors.Is(cause, ctx.Err()) || errors.Is(cause, context.Cause(ctx))
	}) {
		return Issue{}, nil, err
	}
	issue = Issue{
		ExtensionID: handler.ExtensionID,
		HandlerID:   handler.HandlerID,
		Code:        IssueCodeObserverError,
		Err:         err,
	}
	if delivery == nil {
		deliveryErr = errors.New("lifecycle issue delivery is not bound")
	} else {
		deliveryErr = delivery.DeliverExtensionIssue(ctx, issue)
	}
	return issue, deliveryErr, modelselection.NewExtensionFailure(errors.Join(err, deliveryErr, ctx.Err()))
}

// lifecycleKind maps one accepted startup kind to lifecycle policy.
func lifecycleKind(kind startup.RawHandlerKind) (Kind, bool) {
	//nolint:exhaustive // Retry variants are handled by their owning path before this partial switch.
	switch kind {
	case startup.RawHandlerKindAgentStart:
		return KindAgentStart, true
	case startup.RawHandlerKindAgentEnd:
		return KindAgentEnd, true
	case startup.RawHandlerKindAgentSettled:
		return KindAgentSettled, true
	case startup.RawHandlerKindTurnStart:
		return KindTurnStart, true
	case startup.RawHandlerKindTurnEnd:
		return KindTurnEnd, true
	case startup.RawHandlerKindMessageStart:
		return KindMessageStart, true
	case startup.RawHandlerKindMessageUpdate:
		return KindMessageUpdate, true
	case startup.RawHandlerKindMessageEnd:
		return KindMessageEnd, true
	case startup.RawHandlerKindToolExecutionStart:
		return KindToolExecutionStart, true
	case startup.RawHandlerKindToolExecutionUpdate:
		return KindToolExecutionUpdate, true
	case startup.RawHandlerKindToolExecutionEnd:
		return KindToolExecutionEnd, true
	case startup.RawHandlerKindModelSelectionObserver:
		return KindModelSelection, true
	case startup.RawHandlerKindReasoningSelectionObserver:
		return KindReasoningSelection, true
	case startup.RawHandlerKindUnspecified,
		startup.RawHandlerKindSessionBeforeTreeRequest,
		startup.RawHandlerKindSessionBeforeTreeResult,
		startup.RawHandlerKindSessionTree,
		startup.RawHandlerKindModelSelection,
		startup.RawHandlerKindReasoningSelection:
		return 0, false
	default:
		return 0, false
	}
}

// eventKind maps source events to the approved lifecycle groups.
func eventKind(eventType agent.EventType) (Kind, bool) {
	//nolint:exhaustive // Retry variants are handled by their owning path before this partial switch.
	switch eventType {
	case agent.EventAgentStart:
		return KindAgentStart, true
	case agent.EventAgentEnd:
		return KindAgentEnd, true
	case agent.EventTurnStart:
		return KindTurnStart, true
	case agent.EventTurnEnd:
		return KindTurnEnd, true
	case agent.EventMessageStart:
		return KindMessageStart, true
	case agent.EventContentStart, agent.EventTextDelta, agent.EventContentEnd,
		agent.EventToolCallStart, agent.EventToolCallDelta, agent.EventToolCallEnd:
		return KindMessageUpdate, true
	case agent.EventMessageEnd:
		return KindMessageEnd, true
	case agent.EventToolExecutionStart:
		return KindToolExecutionStart, true
	case agent.EventToolExecutionUpdate:
		return KindToolExecutionUpdate, true
	case agent.EventToolExecutionEnd:
		return KindToolExecutionEnd, true
	case agent.EventToolResult:
		return 0, false
	default:
		return 0, false
	}
}
