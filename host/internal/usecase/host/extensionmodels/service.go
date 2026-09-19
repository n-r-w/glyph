package extensionmodels

import (
	"context"
	"errors"
	"fmt"
	"time"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/errtree"
)

const (
	// internalCode identifies an unavailable or unclassified model dependency.
	internalCode = "INTERNAL"
	// modelUnavailableCode identifies an unknown selection or unsupported reasoning choice.
	modelUnavailableCode = "MODEL_UNAVAILABLE"
	// credentialUnavailableCode identifies provider credentials that cannot authorize a request.
	credentialUnavailableCode = "CREDENTIAL_UNAVAILABLE" //nolint:gosec // This is a public error category.
	// selectionCodeNotFound identifies a provider selection that is not configured.
	selectionCodeNotFound = "not_found"
	// selectionCodeReasoningUnsupported identifies a reasoning choice unsupported by the selected model.
	selectionCodeReasoningUnsupported = "reasoning_unsupported"
	// selectionCodeCredentialUnavailable identifies unavailable provider credentials.
	selectionCodeCredentialUnavailable = "credential_unavailable" //nolint:gosec // This is a provider error code.
)

// Error preserves a model-operation category and its complete cause.
type Error struct {
	// code is the closed failure category.
	code string
	// cause retains the original operation error.
	cause error
}

var _ extensioncontroller.ModelFailure = (*Error)(nil)

// Error returns the complete model-operation failure text.
func (f *Error) Error() string {
	return fmt.Sprintf("extension context %s: %v", f.code, f.cause)
}

// Unwrap retains the original cause for callers.
func (f *Error) Unwrap() error { return f.cause }

// ModelCode returns the category without replacing diagnostic text.
func (f *Error) ModelCode() string { return f.code }

// Service owns extension-facing model catalog and configured-request operations.
type Service struct {
	// catalog supplies configured model queries.
	catalog Catalog
	// modelRequester executes configured model requests.
	modelRequester ModelRequester
	// contexts validates the caller binding before and after model work.
	contexts ContextValidator
}

var _ extensioncontroller.ModelOperations = (*Service)(nil)

// New constructs extension-facing model operations from their direct dependencies.
func New(catalog Catalog, modelRequester ModelRequester, contexts ContextValidator) *Service {
	return &Service{catalog: catalog, modelRequester: modelRequester, contexts: contexts}
}

// ReadModels returns complete defensive model descriptors and active selection.
func (s *Service) ReadModels(
	ctx context.Context,
	extensionID, runtimeID string,
	reference extensiondomain.ContextRef,
) (extensioncontroller.ModelCatalog, error) {
	if err := s.validateResult(ctx, extensionID, runtimeID, reference); err != nil {
		return extensioncontroller.ModelCatalog{}, err
	}
	result := extensioncontroller.ModelCatalog{Models: s.catalog.Models(), Selection: s.catalog.ActiveSelection()}
	if err := s.validateResult(ctx, extensionID, runtimeID, reference); err != nil {
		return extensioncontroller.ModelCatalog{}, err
	}
	return result, nil
}

// ReadProviders returns provider identifiers and their ordered model identifiers.
func (s *Service) ReadProviders(
	ctx context.Context,
	extensionID, runtimeID string,
	reference extensiondomain.ContextRef,
) ([]extensioncontroller.Provider, error) {
	if err := s.validateResult(ctx, extensionID, runtimeID, reference); err != nil {
		return nil, err
	}
	providers := make([]extensioncontroller.Provider, 0)
	descriptors := s.catalog.Models()
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
	if err := s.validateResult(ctx, extensionID, runtimeID, reference); err != nil {
		return nil, err
	}
	return providers, nil
}

// Request executes one explicit configured model request and revalidates its binding before completion.
func (s *Service) Request(
	ctx context.Context,
	extensionID, runtimeID string,
	reference extensiondomain.ContextRef,
	selection model.Selection,
	instructions string,
	history []agent.HistoryEntry,
	progress func(extensioncontroller.ConfiguredRetryProgress) error,
) (model.Response, error) {
	if err := s.validateResult(ctx, extensionID, runtimeID, reference); err != nil {
		return model.Response{}, err
	}
	response, err := s.modelRequester.RequestConfigured(
		ctx, selection, instructions, history,
		func(completedAttempts, attemptLimit int64, delay time.Duration, failure string) error {
			if progress == nil {
				return nil
			}
			return progress(extensioncontroller.ConfiguredRetryProgress{
				CompletedAttempts: completedAttempts, AttemptLimit: attemptLimit,
				Delay: delay, Error: failure,
			})
		},
	)
	if err != nil {
		code := internalCode
		if failure, found := errors.AsType[interface {
			error
			FailureCode() string
		}](err); found {
			code = failure.FailureCode()
		} else if ctx.Err() != nil && isPureCancellation(err) {
			return model.Response{}, fmt.Errorf("request configured model: %w", err)
		}
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
		return model.Response{}, &Error{code: code, cause: fmt.Errorf("request configured model: %w", err)}
	}
	if validationErr := s.validateResult(ctx, extensionID, runtimeID, reference); validationErr != nil {
		return model.Response{}, validationErr
	}
	return response, nil
}

// isPureCancellation reports whether every configured-request failure leaf is caller cancellation.
func isPureCancellation(err error) bool {
	return errtree.AllLeavesMatch(err, func(cause error) bool {
		return errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded)
	})
}

// validateResult checks cancellation and binding before work starts or a result leaves the owner.
func (s *Service) validateResult(
	ctx context.Context,
	extensionID, runtimeID string,
	reference extensiondomain.ContextRef,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("complete extension context operation: %w", err)
	}
	return s.contexts.ValidateContext(extensionID, runtimeID, reference)
}
