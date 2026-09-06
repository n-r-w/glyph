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
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
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
	return s.observe(ctx, kind, Event{Agent: event, Settled: false})
}

// ObserveSettled delivers Host settlement to matching observers.
func (s *Service) ObserveSettled(ctx context.Context, runID string) error {
	return s.observe(ctx, KindAgentSettled, Event{Agent: settledSource(runID), Settled: true})
}

// observe invokes one matching observer chain outside service state locks.
func (s *Service) observe(ctx context.Context, kind Kind, event Event) error {
	s.mutex.RLock()
	handlers := slices.Clone(s.handlers)
	s.mutex.RUnlock()
	availability := make(map[string]bool)
	var result error
	for _, handler := range handlers {
		if handler.Kind != kind {
			continue
		}
		if err := ctx.Err(); err != nil {
			return errors.Join(result, err)
		}
		available, checked := availability[handler.ExtensionID]
		if !checked {
			available = s.runtime.HandlerRuntimeAvailable(handler.ExtensionID)
			availability[handler.ExtensionID] = available
		}
		if !available {
			continue
		}
		binding, err := s.contexts.IssueContext(handler.ExtensionID)
		if err != nil {
			if !s.runtime.HandlerRuntimeAvailable(handler.ExtensionID) {
				continue
			}
			result = errors.Join(
				result,
				fmt.Errorf("issue lifecycle context for extension %q: %w", handler.ExtensionID, err),
			)
			continue
		}
		unavailable, err := s.runtime.ObserveLifecycle(ctx, handler.ExtensionID, handler.HandlerID, binding, event)
		if callerErr := ctx.Err(); callerErr != nil {
			return errors.Join(result, callerErr, err)
		}
		if err == nil || unavailable {
			continue
		}
		issue := Issue{
			ExtensionID: handler.ExtensionID, HandlerID: handler.HandlerID,
			Code: IssueCodeObserverError, Err: err,
		}
		s.mutex.RLock()
		delivery := s.issues
		s.mutex.RUnlock()
		if delivery == nil {
			result = errors.Join(result, err, errors.New("lifecycle issue delivery is not bound"))
			continue
		}
		if deliveryErr := delivery.DeliverExtensionIssue(ctx, issue); deliveryErr != nil {
			result = errors.Join(result, err, fmt.Errorf("deliver lifecycle observer issue: %w", deliveryErr))
		}
	}
	return result
}

// lifecycleKind maps one accepted startup kind to lifecycle policy.
func lifecycleKind(kind startup.RawHandlerKind) (Kind, bool) {
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
	case startup.RawHandlerKindUnspecified,
		startup.RawHandlerKindSessionBeforeTreeRequest,
		startup.RawHandlerKindSessionBeforeTreeResult,
		startup.RawHandlerKindSessionTree:
		return 0, false
	default:
		return 0, false
	}
}

// eventKind maps source events to the approved lifecycle groups.
func eventKind(eventType agent.EventType) (Kind, bool) {
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
