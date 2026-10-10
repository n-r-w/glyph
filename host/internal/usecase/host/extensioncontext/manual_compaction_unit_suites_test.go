//go:build !integration

package extensioncontext

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

// manualPolicyRejectsAmbiguousOwnersSuite owns an independent scenario context.
type manualPolicyRejectsAmbiguousOwnersSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestManualPolicyRejectsAmbiguousOwners checks manual invocation rejects multiple policy owners before session capture.
func TestManualPolicyRejectsAmbiguousOwners(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Parallel()
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &manualPolicyRejectsAmbiguousOwnersSuite{Suite: suite.Suite{}})
}

// manualPolicyRejectsAbsentOwnerSuite owns an independent scenario context.
type manualPolicyRejectsAbsentOwnerSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestManualPolicyRejectsAbsentOwner checks manual invocation reports an absent policy owner before session capture.
func TestManualPolicyRejectsAbsentOwner(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Parallel()
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &manualPolicyRejectsAbsentOwnerSuite{Suite: suite.Suite{}})
}

// manualPolicyUsesOneCapturedSessionSuite owns an independent scenario context.
type manualPolicyUsesOneCapturedSessionSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestManualPolicyUsesOneCapturedSession checks captured history and runtime retention after policy failure.
func TestManualPolicyUsesOneCapturedSession(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Parallel()
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &manualPolicyUsesOneCapturedSessionSuite{Suite: suite.Suite{}})
}

// manualPolicyRetainsReceiptAfterPolicyFailureSuite owns an independent scenario context.
type manualPolicyRetainsReceiptAfterPolicyFailureSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestManualPolicyRetainsReceiptAfterPolicyFailure checks durable receipt retention after policy failure.
func TestManualPolicyRetainsReceiptAfterPolicyFailure(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Parallel()
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &manualPolicyRetainsReceiptAfterPolicyFailureSuite{Suite: suite.Suite{}})
}

// manualPolicyCanRejectItsOwnProposalSuite owns an independent scenario context.
type manualPolicyCanRejectItsOwnProposalSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestManualPolicyCanRejectItsOwnProposal checks successful replacement after a handled proposal rejection.
func TestManualPolicyCanRejectItsOwnProposal(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Parallel()
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &manualPolicyCanRejectItsOwnProposalSuite{Suite: suite.Suite{}})
}

// manualObserverFailureRetainsHandledPolicyCausesSuite owns an independent scenario context.
type manualObserverFailureRetainsHandledPolicyCausesSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestManualObserverFailureRetainsHandledPolicyCauses checks observer failure retains handled child causes after commit.
func TestManualObserverFailureRetainsHandledPolicyCauses(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Parallel()
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &manualObserverFailureRetainsHandledPolicyCausesSuite{Suite: suite.Suite{}})
}

// manualUnavailableObserverRetainsCausesSuite owns an independent scenario context.
type manualUnavailableObserverRetainsCausesSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestManualUnavailableObserverRetainsCauses checks unavailable observer settlement retains causes and the durable receipt.
func TestManualUnavailableObserverRetainsCauses(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Parallel()
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &manualUnavailableObserverRetainsCausesSuite{Suite: suite.Suite{}})
}

// manualPolicyOwnsNestedTeardownSuite owns an independent scenario context.
type manualPolicyOwnsNestedTeardownSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestManualPolicyOwnsNestedTeardown checks owning cancellation joins nested work and retains post-commit causes.
func TestManualPolicyOwnsNestedTeardown(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Parallel()
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &manualPolicyOwnsNestedTeardownSuite{Suite: suite.Suite{}})
}

// manualPolicySeparatesAuthorityFromLifetimeSuite owns an independent scenario context.
type manualPolicySeparatesAuthorityFromLifetimeSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestManualPolicySeparatesAuthorityFromLifetime checks separate mutation authority and nested-work lifetime bindings.
func TestManualPolicySeparatesAuthorityFromLifetime(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Parallel()
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &manualPolicySeparatesAuthorityFromLifetimeSuite{Suite: suite.Suite{}})
}
