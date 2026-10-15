//go:build integration

// Package app composes and runs the Host application.
package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/infra/plugins/extension/catalog"
	runtimeadapter "github.com/n-r-w/glyph/host/internal/infra/plugins/extension/runtime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensionmodels"
	extensionmanager "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// admissionWireSuite owns the actual controller and SDK rejection scenario.
type admissionWireSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestManualChildRejectionCarriesParentCauseThroughSDK checks a distinct canceled-parent cause before admission.
func TestManualChildRejectionCarriesParentCauseThroughSDK(t *testing.T) {
	// Arrange parallel execution of the public child-admission suite.
	t.Parallel()
	// Act through a nested SDK request after parent cancellation.
	// Assert the received parent cancellation cause before parent settlement in TestScenario.
	suite.Run(t, &admissionWireSuite{Suite: suite.Suite{}})
}

// TestScenario checks the received child rejection before parent settlement can supplement it.
func (s *admissionWireSuite) TestScenario() {
	const canceledParentAdmissionGuidance = "request-after-parent-cancellation"

	t := s.T()
	// Arrange real controller, runtime adapter, SDK peer, and a deterministically canceled parent registry entry.
	mocks := gomock.NewController(t)
	repository := sessions.NewMockRepository(mocks)
	ids := sessions.NewMockIDGenerator(mocks)
	clock := sessions.NewMockClock(mocks)
	ids.EXPECT().NewID().Return("session", nil)
	clock.EXPECT().Now().Return(time.Unix(1, 0))
	sessionOwner := sessions.New(repository, ids, clock, nil, "/project")
	_, _, err := sessionOwner.CreateActive()
	require.NoError(t, err)
	factory := runtimeadapter.NewFactory()
	reporter := extensionmanager.NewMockFailureReporter(mocks)
	reporter.EXPECT().ReportRuntimeFailure(gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
	runtimes := extensionmanager.New(catalog.New(), factory, reporter)
	t.Cleanup(func() { require.NoError(t, runtimes.Close()) })
	contexts := extensioncontext.New(runtimes, sessionOwner)
	models := extensionmodels.New(
		extensionmodels.NewMockCatalog(mocks),
		extensionmodels.NewMockModelRequester(mocks),
		contexts,
	)
	factory.BindHostServiceFactory(func(extensionID, runtimeID string) extensionsdk.HostService {
		controller := extensioncontroller.New(models, contexts, runtimes, extensionID, runtimeID)
		controller.BindPolicyOperations(contexts)
		return controller
	})
	tree := sessiontree.New(
		sessionOwner,
		sessiontree.NewMockModelSelection(mocks),
		sessiontree.NewMockModelRequester(mocks),
		runtimes,
	)
	startupService := startup.New(runtimes, toolservice.New(runtimes), tree, lifecycle.New(runtimes, nil), nil)
	require.NoError(t, startupService.BindCompaction(contexts))
	report, err := startupService.Load(
		t.Context(),
		startup.Request{DataDirectory: "", ExtensionDirectory: buildManualPolicyFixture(t)},
	)
	require.NoError(t, err)
	require.Empty(t, report.Issues)
	runtimes.Activate(t.Context())
	owner := runtimes.SnapshotCompactionPolicies()[0]
	policy := extensioncontext.NewMockPolicyRuntime(mocks)
	policy.EXPECT().SnapshotCompactionPolicies().Return([]extensioncontext.PolicyOwner{owner})
	policy.EXPECT().SnapshotCompactionHandlers().Return(extensioncontext.HandlerSet{})
	contexts.BindPolicy(policy, sessionOwner, 5*time.Minute)
	cause := errors.New("original owner cause before controller admission")
	parent, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	policy.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
		func(
			ctx context.Context, target extensioncontext.PolicyOwner,
			binding extension.Context, invocation extensioncontext.PolicyInvocation,
		) (extensioncontext.PolicyDisposition, error) {
			cancel(cause)
			// The explicit live test invocation isolates admission from cancellation of the peer handler itself.
			canceled, received := runtimes.InvokeCompactionPolicy(
				context.WithoutCancel(ctx),
				target,
				binding,
				invocation,
			)
			require.ErrorContains(t, received, cause.Error())
			require.ErrorContains(t, received, "compaction operation has ended")
			require.ErrorContains(t, received, "category INTERNAL")
			return canceled, received
		})
	// Act through the registered real policy's nested catalog request and controller rejection.
	result, err := contexts.CompactManualPolicy(
		parent,
		modelexecution.ProviderRequest{},
		mo.Some(canceledParentAdmissionGuidance),
	)
	// Assert no mutation and the independent original owning cause after settlement.
	require.ErrorIs(t, err, cause)
	require.True(t, result.Committed.IsNone())
	require.Empty(t, sessionOwner.ActiveEntries())
}
