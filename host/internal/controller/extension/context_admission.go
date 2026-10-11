// Package extension maps public runtime requests to Host operations.
package extension

import (
	"context"
	"errors"
	"sync"

	"github.com/samber/mo"

	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// Prepare returns admitted executable work for one extension request.
func (s *Service) Prepare(
	ctx context.Context,
	operationID string,
	request *extensionpb.ExtensionRequest,
) (extensionsdk.HostOperation, error) {
	mappedRequest, err := newContextRequest(request)
	if err != nil {
		return nil, extensionsdk.Reject(invalidArgumentCode, err)
	}
	reference := mappedRequest.reference
	if reference == nil || reference.GetContextId() == "" || reference.GetRuntimeInstanceId() == "" ||
		reference.GetSessionId() == "" {
		return nil, extensionsdk.Reject(
			invalidArgumentCode,
			errors.New("complete extension context reference is required"),
		)
	}
	bound := extensiondomain.ContextRef{
		ID:                reference.GetContextId(),
		RuntimeInstanceID: reference.GetRuntimeInstanceId(),
		SessionID:         reference.GetSessionId(),
	}
	if validationErr := s.contexts.ValidateContext(s.extensionID, s.runtimeID, bound); validationErr != nil {
		return nil, extensionsdk.Reject(contextFailureCode(validationErr), validationErr)
	}
	release, err := s.runtime.BeginContextOperation(ctx, s.extensionID, s.runtimeID)
	if err != nil {
		return nil, extensionsdk.Reject(contextFailureCode(err), err)
	}
	childRelease, release, err := s.prepareCompactionAdmission(
		request.GetParentCompactionId(),
		mappedRequest,
		release,
	)
	if err != nil {
		return nil, err
	}
	prepared := &contextOperation{
		service: s, reference: bound, action: nil, selectionRelease: mo.None[func()](),
		parentID:     mo.EmptyableToOption(request.GetParentCompactionId()),
		childRelease: mo.TupleToOption(childRelease, childRelease != nil), release: release, id: operationID,
	}
	action, prepareErr := mappedRequest.prepare(prepared)
	if prepareErr != nil {
		// Preserve the acquired cause before the once-only scope release settles the child.
		if childRelease != nil {
			childRelease(prepareErr)
		}
		release()
		return nil, prepareErr
	}
	prepared.action = action
	return prepared, nil
}

// prepareCompactionAdmission reserves the manual gate or the owning policy lifetime.
func (s *Service) prepareCompactionAdmission(
	parentID string,
	mappedRequest contextRequest,
	release func(),
) (childRelease func(error), scopeRelease func(), admissionErr error) {
	var err error
	if parentID != "" {
		if mappedRequest.admission == contextAdmissionManual {
			release()
			return nil, nil, extensionsdk.Reject(
				invalidArgumentCode,
				errors.New("manual compaction cannot be nested in a compaction operation"),
			)
		}

		childRelease, err = s.compactionPolicy.BeginCompactionChild(s.extensionID, s.runtimeID, parentID)
		if err != nil {
			release()
			return nil, nil, extensionsdk.Reject(contextFailureCode(err), err)
		}
		runtimeRelease := release
		release = sync.OnceFunc(func() {
			childRelease(nil)
			runtimeRelease()
		})
	}
	if mappedRequest.admission == contextAdmissionCandidate && childRelease == nil {
		release()
		return nil, nil, extensionsdk.Reject(
			invalidArgumentCode,
			errors.New("compaction candidate requires a parent lifetime binding"),
		)
	}
	if mappedRequest.admission == contextAdmissionManual {
		if s.compaction == nil || s.compactionGate == nil {
			release()
			return nil, nil, extensionsdk.Reject(internalFailureCode, errors.New("manual compaction is not bound"))
		}
		gateRelease, acquired := s.compactionGate.TryAcquire()
		if !acquired {
			release()
			return nil, nil, extensionsdk.Reject(publicSelectionCodeBusy, errors.New("another operation is active"))
		}
		runtimeRelease := release
		release = sync.OnceFunc(func() {
			gateRelease()
			runtimeRelease()
		})
	}
	return childRelease, release, nil
}
