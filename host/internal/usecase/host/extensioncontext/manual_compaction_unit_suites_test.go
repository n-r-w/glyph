//go:build !integration

// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

// manualPolicyRejectsAmbiguousOwnersSuite isolates admission with competing policy owners.
type manualPolicyRejectsAmbiguousOwnersSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestManualPolicyRejectsAmbiguousOwners checks manual invocation rejects multiple policy owners before session capture.
func TestManualPolicyRejectsAmbiguousOwners(t *testing.T) {
	// Arrange parallel execution of the owner-ambiguity suite.
	t.Parallel()
	// Act through admission with competing owners.
	// Assert rejection before session capture in TestScenario.
	suite.Run(t, &manualPolicyRejectsAmbiguousOwnersSuite{Suite: suite.Suite{}})
}

// manualPolicyRejectsAbsentOwnerSuite isolates admission with no registered policy.
type manualPolicyRejectsAbsentOwnerSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestManualPolicyRejectsAbsentOwner checks manual invocation reports an absent policy owner before session capture.
func TestManualPolicyRejectsAbsentOwner(t *testing.T) {
	// Arrange parallel execution of the owner-absence suite.
	t.Parallel()
	// Act through admission with no registered policy.
	// Assert explicit owner absence before session capture in TestScenario.
	suite.Run(t, &manualPolicyRejectsAbsentOwnerSuite{Suite: suite.Suite{}})
}

// manualPolicyUsesOneCapturedSessionSuite isolates session capture and policy failure.
type manualPolicyUsesOneCapturedSessionSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestManualPolicyUsesOneCapturedSession checks captured history and runtime retention after policy failure.
func TestManualPolicyUsesOneCapturedSession(t *testing.T) {
	// Arrange parallel execution of the captured-session suite.
	t.Parallel()
	// Act through policy failure after session capture.
	// Assert captured history and the original runtime cause in TestScenario.
	suite.Run(t, &manualPolicyUsesOneCapturedSessionSuite{Suite: suite.Suite{}})
}

// manualPolicyRetainsReceiptAfterPolicyFailureSuite isolates durable receipt retention after policy failure.
type manualPolicyRetainsReceiptAfterPolicyFailureSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestManualPolicyRetainsReceiptAfterPolicyFailure checks durable receipt retention after policy failure.
func TestManualPolicyRetainsReceiptAfterPolicyFailure(t *testing.T) {
	// Arrange parallel execution of the durable-receipt suite.
	t.Parallel()
	// Act through policy failure after persistence.
	// Assert the durable receipt and acquired causes in TestScenario.
	suite.Run(t, &manualPolicyRetainsReceiptAfterPolicyFailureSuite{Suite: suite.Suite{}})
}

// manualPolicyCanRejectItsOwnProposalSuite isolates replacement after a handled projection rejection.
type manualPolicyCanRejectItsOwnProposalSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestManualPolicyCanRejectItsOwnProposal checks successful replacement after a handled proposal rejection.
func TestManualPolicyCanRejectItsOwnProposal(t *testing.T) {
	// Arrange parallel execution of the handled-proposal suite.
	t.Parallel()
	// Act through replacement after a handled projection rejection.
	// Assert that the handled rejection does not become Host policy in TestScenario.
	suite.Run(t, &manualPolicyCanRejectItsOwnProposalSuite{Suite: suite.Suite{}})
}

// manualObserverFailureRetainsHandledPolicyCausesSuite isolates observer failure after handled child failures.
type manualObserverFailureRetainsHandledPolicyCausesSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestManualObserverFailureRetainsHandledPolicyCauses checks observer failure retains handled child causes after commit.
func TestManualObserverFailureRetainsHandledPolicyCauses(t *testing.T) {
	// Arrange parallel execution of the observer-failure suite.
	t.Parallel()
	// Act through observer failure after successful persistence.
	// Assert all handled child causes and the durable receipt in TestScenario.
	suite.Run(t, &manualObserverFailureRetainsHandledPolicyCausesSuite{Suite: suite.Suite{}})
}

// manualUnavailableObserverRetainsCausesSuite isolates failure of a captured observer binding.
type manualUnavailableObserverRetainsCausesSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestManualUnavailableObserverRetainsCauses checks unavailable observer settlement retains causes and the durable receipt.
func TestManualUnavailableObserverRetainsCauses(t *testing.T) {
	// Arrange parallel execution of the unavailable-observer suite.
	t.Parallel()
	// Act through failure to bind a captured observer.
	// Assert acquired causes and the durable receipt in TestScenario.
	suite.Run(t, &manualUnavailableObserverRetainsCausesSuite{Suite: suite.Suite{}})
}

// manualPolicyOwnsNestedTeardownSuite isolates parent cancellation and joined child teardown.
type manualPolicyOwnsNestedTeardownSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestManualPolicyOwnsNestedTeardown checks owning cancellation joins nested work and retains post-commit causes.
func TestManualPolicyOwnsNestedTeardown(t *testing.T) {
	// Arrange parallel execution of the nested-teardown suite.
	t.Parallel()
	// Act through policy completion with active nested work.
	// Assert joined work and independent teardown failures in TestScenario.
	suite.Run(t, &manualPolicyOwnsNestedTeardownSuite{Suite: suite.Suite{}})
}

// manualPolicySeparatesAuthorityFromLifetimeSuite isolates mutation authority from nested-work admission.
type manualPolicySeparatesAuthorityFromLifetimeSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestManualPolicySeparatesAuthorityFromLifetime checks separate mutation authority and nested-work lifetime bindings.
func TestManualPolicySeparatesAuthorityFromLifetime(t *testing.T) {
	// Arrange parallel execution of the authority-separation suite.
	t.Parallel()
	// Act through child admission with distinct authority and lifetime bindings.
	// Assert preserved Run values and parent cancellation in TestScenario.
	suite.Run(t, &manualPolicySeparatesAuthorityFromLifetimeSuite{Suite: suite.Suite{}})
}
