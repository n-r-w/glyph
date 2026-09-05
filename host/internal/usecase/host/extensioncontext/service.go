package extensioncontext

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
	"github.com/n-r-w/glyph/host/internal/usecase/host/tools"
)

const (
	// staleContextCode identifies a permanently invalidated or mismatched binding.
	staleContextCode = "STALE_CONTEXT"
	// internalCode identifies unavailable catalog composition.
	internalCode = "INTERNAL"
	// modelUnavailableCode identifies an unknown selection or unsupported reasoning choice.
	modelUnavailableCode = "MODEL_UNAVAILABLE"
	// credentialUnavailableCode identifies provider credentials that cannot authorize a request.
	credentialUnavailableCode = "CREDENTIAL_UNAVAILABLE" //nolint:gosec // This is a public error category.
	// modelFailedCode identifies provider execution failure after selection validation.
	modelFailedCode = "MODEL_FAILED"
	// persistenceUnavailableCode identifies a durable append failure.
	persistenceUnavailableCode = "PERSISTENCE_UNAVAILABLE"
	// selectionCodeNotFound identifies a provider selection that is not configured.
	selectionCodeNotFound = "not_found"
	// selectionCodeReasoningUnsupported identifies a reasoning choice unsupported by the selected model.
	selectionCodeReasoningUnsupported = "reasoning_unsupported"
	// selectionCodeCredentialUnavailable identifies unavailable provider credentials.
	selectionCodeCredentialUnavailable = "credential_unavailable" //nolint:gosec // This is a provider error code.
)

// ContextError preserves the context-operation category and its complete cause.
type ContextError struct {
	// code is the closed failure category.
	code string
	// cause retains the original operation error.
	cause error
}

var _ extensioncontroller.ContextFailure = (*ContextError)(nil)

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
	// mutex protects catalog binding, sequence allocation, and issued contexts.
	mutex sync.Mutex
	// catalog is bound after provider construction.
	catalog Catalog
	// bindings retains only the latest binding for each extension.
	bindings map[string]binding
}

var (
	_ extensioncontroller.ContextOperations = (*Service)(nil)
	_ sessiontree.ContextIssuer             = (*Service)(nil)
	_ tools.ContextIssuer                   = (*Service)(nil)
)

// New constructs context ownership over the runtime and session state owners.
func New(runtime RuntimeState, sessionState SessionState) *Service {
	return &Service{
		runtime: runtime, session: sessionState, mutex: sync.Mutex{}, catalog: nil,
		bindings: make(map[string]binding),
	}
}

// BindCatalog installs the configured catalog after provider construction.
func (s *Service) BindCatalog(catalog Catalog) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.catalog = catalog
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

// ReadModels returns complete defensive model descriptors and active selection.
func (s *Service) ReadModels(
	ctx context.Context,
	extensionID, runtimeID string,
	reference extension.ContextRef,
) (extensioncontroller.ModelCatalog, error) {
	catalog, err := s.readCatalog(ctx, extensionID, runtimeID, reference)
	if err != nil {
		return extensioncontroller.ModelCatalog{}, err
	}
	result := extensioncontroller.ModelCatalog{Models: catalog.Models(), Selection: catalog.ActiveSelection()}
	if validationErr := s.validateResult(ctx, extensionID, runtimeID, reference); validationErr != nil {
		return extensioncontroller.ModelCatalog{}, validationErr
	}
	return result, nil
}

// ReadProviders returns provider identifiers and their ordered model identifiers.
func (s *Service) ReadProviders(
	ctx context.Context,
	extensionID, runtimeID string,
	reference extension.ContextRef,
) ([]extensioncontroller.Provider, error) {
	catalog, err := s.readCatalog(ctx, extensionID, runtimeID, reference)
	if err != nil {
		return nil, err
	}
	providers := make([]extensioncontroller.Provider, 0)
	descriptors := catalog.Models()
	for descriptorIndex := range descriptors {
		descriptor := &descriptors[descriptorIndex]
		index := -1
		for candidate := range providers {
			if providers[candidate].ID == descriptor.Provider {
				index = candidate
				break
			}
		}
		if index < 0 {
			index = len(providers)
			providers = append(providers, extensioncontroller.Provider{ID: descriptor.Provider, ModelIDs: nil})
		}
		providers[index].ModelIDs = append(providers[index].ModelIDs, descriptor.Model)
	}
	if validationErr := s.validateResult(ctx, extensionID, runtimeID, reference); validationErr != nil {
		return nil, validationErr
	}
	return providers, nil
}

// Request executes one explicit configured model request and revalidates its binding before completion.
func (s *Service) Request(
	ctx context.Context,
	extensionID, runtimeID string,
	reference extension.ContextRef,
	selection model.Selection,
	instructions string,
	history []agent.HistoryEntry,
) (model.Response, error) {
	catalog, err := s.readCatalog(ctx, extensionID, runtimeID, reference)
	if err != nil {
		return model.Response{}, err
	}
	response, err := catalog.Request(ctx, selection, instructions, history)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return model.Response{}, fmt.Errorf("request configured model: %w", err)
		}
		code := modelFailedCode
		if failure, found := errors.AsType[RequestFailure](err); found {
			switch failure.SelectionCode() {
			case selectionCodeNotFound, selectionCodeReasoningUnsupported:
				code = modelUnavailableCode
			case selectionCodeCredentialUnavailable:
				code = credentialUnavailableCode
			default:
				code = internalCode
			}
		}
		return model.Response{}, &ContextError{code: code, cause: fmt.Errorf("request configured model: %w", err)}
	}
	if validationErr := s.validateResult(ctx, extensionID, runtimeID, reference); validationErr != nil {
		return model.Response{}, validationErr
	}
	return response, nil
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
		code := internalCode
		if failure, found := errors.AsType[extensioncontroller.ContextFailure](err); found {
			code = failure.ContextCode()
		} else if errors.Is(err, session.ErrUnavailable) {
			code = staleContextCode
		} else if errors.Is(err, session.ErrPersistenceUnavailable) {
			code = persistenceUnavailableCode
		}
		return session.Entry{}, &ContextError{code: code, cause: fmt.Errorf("append extension entry: %w", err)}
	}
	return entry, nil
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
) (SessionIdentity, error) {
	if err := ctx.Err(); err != nil {
		return SessionIdentity{}, fmt.Errorf("complete extension context operation: %w", err)
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	issued, err := s.validateContextLocked(extensionID, runtimeID, reference)
	if err != nil {
		return SessionIdentity{}, err
	}
	return SessionIdentity{
		ID: issued.context.SessionID, WorkingDirectory: issued.context.WorkingDirectory,
		Incarnation: issued.incarnation,
	}, nil
}

// readCatalog validates admission and snapshots the late-bound catalog dependency.
func (s *Service) readCatalog(
	ctx context.Context,
	extensionID, runtimeID string,
	reference extension.ContextRef,
) (Catalog, error) {
	if err := s.validateResult(ctx, extensionID, runtimeID, reference); err != nil {
		return nil, err
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.catalog == nil {
		return nil, &ContextError{code: internalCode, cause: errors.New("provider catalog is not bound")}
	}
	return s.catalog, nil
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
