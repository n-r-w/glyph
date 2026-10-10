//go:build integration

// Package extensionv1 supplies the public Extension SDK.
package extensionv1

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

// compactionPolicyReceivesScopedInputSuite owns an independent scenario context.
type compactionPolicyReceivesScopedInputSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestCompactionPolicyReceivesScopedInput checks detached policy input and scoped nested operations
// through the SDK connection.
func TestCompactionPolicyReceivesScopedInput(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Parallel()
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &compactionPolicyReceivesScopedInputSuite{Suite: suite.Suite{}})
}
