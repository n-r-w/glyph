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
	// mappedRequest and err contain the decoded operation selector and decoding failure.
	mappedRequest, err := mapContextRequest(request)
	if err != nil {
		return nil, extensionsdk.Reject(invalidArgumentCode, err)
	}
	// reference identifies the caller's issued session binding.
	reference := mappedRequest.reference
	if reference == nil || reference.GetContextId() == "" || reference.GetRuntimeInstanceId() == "" ||
		reference.GetSessionId() == "" {
		return nil, extensionsdk.Reject(
			invalidArgumentCode,
			errors.New("complete extension context reference is required"),
		)
	}
	// bound contains the session binding used for Host admission.
	bound := extensiondomain.ContextRef{
		ID:                reference.GetContextId(),
		RuntimeInstanceID: reference.GetRuntimeInstanceId(),
		SessionID:         reference.GetSessionId(),
	}
	// validationErr contains the session binding validation failure.
	if validationErr := s.contexts.ValidateContext(s.extensionID, s.runtimeID, bound); validationErr != nil {
		return nil, extensionsdk.Reject(contextFailureCode(validationErr), validationErr)
	}
	// release and err contain the runtime reservation release and admission failure.
	release, err := s.runtime.BeginContextOperation(ctx, s.extensionID, s.runtimeID)
	if err != nil {
		return nil, extensionsdk.Reject(contextFailureCode(err), err)
	}
	// childRelease, release, and err contain the admitted child owner, scope release, and admission failure.
	childRelease, release, err := s.prepareCompactionAdmission(
		request.GetParentCompactionId(),
		mappedRequest,
		release,
	)
	if err != nil {
		return nil, err
	}
	// prepared owns common admission and the selected executable.
	prepared := &contextOperation{
		service: s, reference: bound, action: nil, selectionRelease: mo.None[func()](),
		parentID:     mo.EmptyableToOption(request.GetParentCompactionId()),
		childRelease: mo.TupleToOption(childRelease, childRelease != nil), release: release, id: operationID,
	}
	// action and prepareErr contain the bound executable and preparation failure.
	action, prepareErr := mappedRequest.prepare(prepared)
	if prepareErr != nil {
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
	// err contains any child lifetime admission failure.
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
		// runtimeRelease owns the runtime reservation within the child scope.
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
		// gateRelease owns the manual-operation gate; acquired indicates admission.
		gateRelease, acquired := s.compactionGate.TryAcquire()
		if !acquired {
			release()
			return nil, nil, extensionsdk.Reject(publicSelectionCodeBusy, errors.New("another operation is active"))
		}
		// runtimeRelease owns the runtime reservation within the manual gate scope.
		runtimeRelease := release
		release = sync.OnceFunc(func() {
			gateRelease()
			runtimeRelease()
		})
	}
	return childRelease, release, nil
}
