package extensioncontext

import (
	"errors"

	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// PolicyPurpose identifies the caller's preparation intent.
type PolicyPurpose int

const (
	// PolicyPurposeManual means the caller requests an explicit replacement.
	PolicyPurposeManual PolicyPurpose = iota + 1
	// PolicyPurposeAutomatic lets the owner choose whether to replace history.
	PolicyPurposeAutomatic
	// PolicyPurposeOverflow lets the owner recover a rejected conversation input.
	PolicyPurposeOverflow
)

// PolicyDisposition identifies the owner's completed decision.
type PolicyDisposition int

const (
	// PolicyPreserve means the owner keeps captured history during automatic preparation or declines overflow recovery.
	// Invalid for manual compaction.
	PolicyPreserve PolicyDisposition = iota + 1
	// PolicyReplaced means the owner acknowledges a durable replacement.
	PolicyReplaced
	// PolicyCanceled means the owner cancels preparation without provider dispatch.
	PolicyCanceled
)

// validateCompletion checks agreement between the owner's decision and durable outcome.
func (d PolicyDisposition) validateCompletion(purpose PolicyPurpose, committed bool, cause error) error {
	switch d {
	case PolicyPreserve:
		if committed || purpose == PolicyPurposeManual {
			return errors.New("compaction policy preserve conflicts with replacement intent or receipt")
		}
		return nil
	case PolicyCanceled:
		if committed {
			return errors.New("compaction policy canceled after durable replacement")
		}
		return nil
	case PolicyReplaced:
	}
	if !committed && cause == nil {
		return errors.New("compaction policy finished without replacement or cancellation")
	}
	return nil
}

// OutgoingInput contains final captured history and lifecycle correlation.
type OutgoingInput struct {
	// Snapshot identifies the outgoing session projection.
	Snapshot CompactionSnapshot
	// Input contains the frozen conversation binding and history.
	Input modelexecution.ProviderRequest
	// LifecycleID correlates nested work without mutation authority.
	LifecycleID string
}
