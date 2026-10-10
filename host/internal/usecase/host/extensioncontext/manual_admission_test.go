//go:build !integration

package extensioncontext

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// parentAdmissionSuite owns the ended-parent admission failure scenario.
type parentAdmissionSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestManualChildAdmissionRetainsDistinctParentCause checks the rejection before parent settlement.
func TestManualChildAdmissionRetainsDistinctParentCause(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Parallel()
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &parentAdmissionSuite{Suite: suite.Suite{}})
}

// TestScenario checks cause identity and text at the admission boundary.
func (s *parentAdmissionSuite) TestScenario() {
	t := s.T()
	// Arrange a captured operation canceled before a correlated request is admitted.
	service, policy, owner := newManualPolicyOperationFixture(t)
	// cause is the original parent cancellation failure.
	cause := errors.New("original owning cancellation before child admission")
	// parent and cancel own the canceled invocation lifetime.
	parent, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	policy.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, target PolicyOwner, binding extension.Context, invocation PolicyInvocation) (bool, error) {
			cancel(cause)
			// release and err contain child admission ownership and its failure.
			release, err := service.BeginCompactionChild(owner.ExtensionID, owner.RuntimeID, invocation.LifecycleID)
			// Assert the rejected child's acquired cause independently of the eventual parent error.
			require.Nil(t, release)
			require.ErrorIs(t, err, cause)
			require.ErrorContains(t, err, cause.Error())
			return true, nil
		})
	// Act through the admitted parent operation.
	_, err := service.CompactManualPolicy(parent, modelexecution.ProviderRequest{}, mo.None[string]())
	// Assert that settlement preserves the owning cause too.
	require.ErrorIs(t, err, cause)
}
