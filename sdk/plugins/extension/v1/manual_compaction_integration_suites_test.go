//go:build integration

// Package extensionv1 supplies the public Extension SDK.
package extensionv1

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

// compactionPolicyReceivesScopedInputSuite isolates policy authority and input ownership over the SDK connection.
type compactionPolicyReceivesScopedInputSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestCompactionPolicyReceivesScopedInput checks detached policy input and scoped nested operations
// through the SDK connection.
func TestCompactionPolicyReceivesScopedInput(t *testing.T) {
	// Arrange parallel execution of the policy-input suite.
	t.Parallel()
	// Act through policy registration and nested SDK operations.
	// Assert detached input and scoped authority in TestScenario.
	suite.Run(t, &compactionPolicyReceivesScopedInputSuite{Suite: suite.Suite{}})
}
