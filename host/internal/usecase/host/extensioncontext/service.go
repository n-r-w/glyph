package extensioncontext

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensionmodels"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelselection"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
	"github.com/n-r-w/glyph/host/internal/usecase/host/tools"
)

const (
	// staleContextCode identifies a permanently invalidated or mismatched binding.
	staleContextCode = "STALE_CONTEXT"
	// internalCode identifies an unclassified context operation failure.
	internalCode = "INTERNAL"
	// persistenceUnavailableCode identifies a durable append failure.
	persistenceUnavailableCode = "PERSISTENCE_UNAVAILABLE"
	// deliveryFailedIssueCode identifies failed client publication after commit.
	deliveryFailedIssueCode = "DELIVERY_FAILED"
)

// ContextError preserves the context-operation category and its complete cause.
type ContextError struct {
	// code is the closed failure category.
	code string
	// cause retains the original operation error.
	cause error
}

var (
	_ extensioncontroller.ContextFailure = (*ContextError)(nil)
	_ modelselection.BindingFailure      = (*ContextError)(nil)
)

// Error returns the complete context failure text.
func (f *ContextError) Error() string {
	return fmt.Sprintf("extension context %s: %v", f.code, f.cause)
}

// Unwrap retains the original cause for callers.
func (f *ContextError) Unwrap() error { return f.cause }

// ContextCode returns the category without replacing diagnostic text.
func (f *ContextError) ContextCode() string { return f.code }

// binding stores the active-session incarnation associated with an issued context.
type binding struct {
	// context contains the public immutable identity.
	context extension.Context
	// incarnation distinguishes replacements of the same durable session.
	incarnation uint64
}

// Service owns issued bindings and session-bound context operation coordination.
type Service struct {
	// runtime supplies accepted process identity without transferring runtime ownership.
	runtime RuntimeState
	// session supplies atomic active-session identity.
	session SessionState
	// mutex protects issued contexts.
	mutex sync.Mutex
	// bindings retains only the latest binding for each extension.
	bindings map[string]binding
}

var (
	_ extensioncontroller.ContextOperations = (*Service)(nil)
	_ extensionmodels.ContextValidator      = (*Service)(nil)
	_ sessiontree.ContextIssuer             = (*Service)(nil)
	_ lifecycle.ContextIssuer               = (*Service)(nil)
	_ modelselection.ContextIssuer          = (*Service)(nil)
	_ modelselection.BindingProtection      = (*Service)(nil)
	_ tools.ContextIssuer                   = (*Service)(nil)
)

// New constructs context ownership over the runtime and session state owners.
func New(runtime RuntimeState, sessionState SessionState) *Service {
	return &Service{
		runtime: runtime, session: sessionState, mutex: sync.Mutex{}, bindings: make(map[string]binding),
	}
}

// IssueContext returns the binding for one accepted runtime and active-session incarnation.
func (s *Service) IssueContext(extensionID string) (extension.Context, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	instance, available := s.runtime.ContextRuntime(extensionID)
	identity := s.session.ContextSession()
	if !available || identity.ID == "" || identity.Incarnation == 0 {
		return extension.Context{}, staleBinding(extensionID, "runtime or active session is unavailable")
	}
	preceding, found := s.bindings[extensionID]
	if found && preceding.context.RuntimeInstanceID == instance && preceding.incarnation == identity.Incarnation &&
		preceding.context.SessionID == identity.ID {
		return preceding.context, nil
	}
	issued := extension.Context{
		ID: rand.Text(), ExtensionID: extensionID, RuntimeInstanceID: instance,
		SessionID: identity.ID, WorkingDirectory: identity.WorkingDirectory,
	}
	s.bindings[extensionID] = binding{context: issued, incarnation: identity.Incarnation}
	return issued, nil
}

// ValidateContext checks all reference fields against the issued and active binding.
func (s *Service) ValidateContext(extensionID, runtimeID string, reference extension.ContextRef) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	_, err := s.validateContextLocked(extensionID, runtimeID, reference)
	return err
}

// validateContextLocked checks one reference and returns its exact issued binding.
func (s *Service) validateContextLocked(
	extensionID, runtimeID string,
	reference extension.ContextRef,
) (binding, error) {
	issued, found := s.bindings[extensionID]
	if !found || reference.ID != issued.context.ID || reference.RuntimeInstanceID != runtimeID ||
		runtimeID != issued.context.RuntimeInstanceID || reference.SessionID != issued.context.SessionID {
		return binding{}, staleBinding(extensionID, "reference does not match the context issued to this runtime")
	}
	instance, available := s.runtime.ContextRuntime(extensionID)
	identity := s.session.ContextSession()
	if !available || instance != runtimeID || identity.ID != reference.SessionID ||
		identity.Incarnation != issued.incarnation {
		return binding{}, staleBinding(extensionID, "runtime or active-session incarnation was replaced")
	}
	return issued, nil
}

// AppendExtension persists one caller-owned hidden entry under the issued session incarnation.
func (s *Service) AppendExtension(
	ctx context.Context,
	extensionID, runtimeID string,
	reference extension.ContextRef,
	entryType string,
	data []byte,
) (session.Entry, error) {
	expected, err := s.boundSession(ctx, extensionID, runtimeID, reference)
	if err != nil {
		return session.Entry{}, err
	}
	entry, err := s.session.AppendExtension(
		ctx,
		expected,
		session.ExtensionEnvelope{ExtensionID: extensionID, EntryType: entryType, Data: data},
		func() (func(), error) { return s.runtime.BeginContextCommit(extensionID, runtimeID) },
	)
	if err != nil {
		return session.Entry{}, &ContextError{
			code: appendFailureCode(err), cause: fmt.Errorf("append extension entry: %w", err),
		}
	}
	return entry, nil
}

// AppendExtensionMessage persists one caller-owned model-visible message and reports delivery failure after commit.
func (s *Service) AppendExtensionMessage(
	ctx context.Context,
	extensionID, runtimeID string,
	reference extension.ContextRef,
	entryType, text string,
	visibility session.ClientVisibility,
) (extensioncontroller.AppendMessageResult, error) {
	expected, err := s.boundSession(ctx, extensionID, runtimeID, reference)
	if err != nil {
		return extensioncontroller.AppendMessageResult{}, err
	}
	entry, err := s.session.AppendExtensionMessage(
		ctx,
		expected,
		session.ExtensionMessage{
			ExtensionID: extensionID, EntryType: entryType, Text: text, Visibility: visibility,
		},
		func() (func(), error) { return s.runtime.BeginContextCommit(extensionID, runtimeID) },
	)
	if entry.ID != "" && err != nil {
		return committedDeliveryFailure(entry, extensionID, err)
	}
	if err != nil {
		return extensioncontroller.AppendMessageResult{}, &ContextError{
			code: appendFailureCode(err), cause: fmt.Errorf("append extension message: %w", err),
		}
	}
	return extensioncontroller.AppendMessageResult{Entry: entry, Issues: nil}, nil
}

// appendFailureCode preserves an owner-supplied category before classifying session failures.
func appendFailureCode(err error) string {
	if failure, found := errors.AsType[extensioncontroller.ContextFailure](err); found {
		return failure.ContextCode()
	}
	if errors.Is(err, session.ErrUnavailable) {
		return staleContextCode
	}
	if errors.Is(err, session.ErrPersistenceUnavailable) {
		return persistenceUnavailableCode
	}
	return internalCode
}

// committedDeliveryFailure converts post-commit publication failure to a nonterminal operation issue.
func committedDeliveryFailure(
	entry session.Entry,
	extensionID string,
	cause error,
) (extensioncontroller.AppendMessageResult, error) {
	return extensioncontroller.AppendMessageResult{
		Entry: entry,
		Issues: []extensioncontroller.OperationIssue{{
			ExtensionID: extensionID, Code: deliveryFailedIssueCode, Message: cause.Error(),
		}},
	}, nil
}

// ReadSessionState returns one caller-filtered active-branch snapshot.
func (s *Service) ReadSessionState(
	ctx context.Context,
	extensionID, runtimeID string,
	reference extension.ContextRef,
) (extensioncontroller.SessionState, error) {
	expected, err := s.boundSession(ctx, extensionID, runtimeID, reference)
	if err != nil {
		return extensioncontroller.SessionState{}, err
	}
	snapshot, err := s.session.ExtensionState(ctx, expected, extensionID)
	if err != nil {
		code := internalCode
		if errors.Is(err, session.ErrUnavailable) {
			code = staleContextCode
		}
		return extensioncontroller.SessionState{}, &ContextError{
			code: code, cause: fmt.Errorf("read extension session state: %w", err),
		}
	}
	if validationErr := s.validateResult(ctx, extensionID, runtimeID, reference); validationErr != nil {
		return extensioncontroller.SessionState{}, validationErr
	}
	return extensioncontroller.SessionState{
		SessionID: snapshot.SessionID, ActiveLeafID: snapshot.ActiveLeafID, Entries: snapshot.Entries,
	}, nil
}

// boundSession validates admission and returns the incarnation stored with the issued binding.
func (s *Service) boundSession(
	ctx context.Context,
	extensionID, runtimeID string,
	reference extension.ContextRef,
) (contextcompaction.SessionIdentity, error) {
	if err := ctx.Err(); err != nil {
		return contextcompaction.SessionIdentity{}, fmt.Errorf("complete extension context operation: %w", err)
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	issued, err := s.validateContextLocked(extensionID, runtimeID, reference)
	if err != nil {
		return contextcompaction.SessionIdentity{}, err
	}
	return contextcompaction.SessionIdentity{
		ID: issued.context.SessionID, WorkingDirectory: issued.context.WorkingDirectory,
		Incarnation: issued.incarnation,
	}, nil
}

// validateResult checks cancellation and binding before a result leaves the capability owner.
func (s *Service) validateResult(
	ctx context.Context,
	extensionID, runtimeID string,
	reference extension.ContextRef,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("complete extension context operation: %w", err)
	}
	return s.ValidateContext(extensionID, runtimeID, reference)
}

// staleBinding supplies a closed category and a diagnostic cause for invalid references.
func staleBinding(extensionID, reason string) error {
	return &ContextError{code: staleContextCode, cause: fmt.Errorf("extension %q: %s", extensionID, reason)}
}
