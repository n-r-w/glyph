//go:build !integration

// Package extension adapts Host-owned extension operations to the public protocol.
package extension

import (
	"errors"
	"sync"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// selectionPreparationSuite owns preparation failure and cleanup scenarios.
type selectionPreparationSuite struct {
	// Suite supplies independent scenario assertions.
	suite.Suite
}

// TestSelectionPreparationRetainsAdmittedChildCause checks correlated and ordinary preparation rejection.
func TestSelectionPreparationRetainsAdmittedChildCause(t *testing.T) {
	// Arrange independent suite execution.
	t.Parallel()
	// Act and Assert preparation rejection and cleanup through the suite.
	suite.Run(t, &selectionPreparationSuite{Suite: suite.Suite{}})
}

// TestScenario checks the returned cause and exactly-once child/runtime settlement.
func (s *selectionPreparationSuite) TestScenario() {
	const selectionPreparationNotFound = "not_found"
	const publicSelectionNotFound = "NOT_FOUND"
	t := s.T()
	// Arrange both the admitted-child request and the ordinary uncorrelated request.
	for _, scenario := range []struct {
		name   string
		parent mo.Option[string]
	}{{name: "admitted child", parent: mo.Some("lifetime")}, {name: "ordinary selection", parent: mo.None[string]()}} {
		t.Run(scenario.name, func(t *testing.T) {
			parent, correlated := scenario.parent.Get()
			mocks := gomock.NewController(t)
			contexts := NewMockContextOperations(mocks)
			models := NewMockModelOperations(mocks)
			runtime := NewMockRuntimeOperations(mocks)
			selection := NewMockModelSelection(mocks)
			policy := NewMockCompactionPolicyOperations(mocks)
			reference := extensiondomain.ContextRef{ID: "context", RuntimeInstanceID: "runtime", SessionID: "session"}
			source := errors.New("catalog preparation source")
			classified := selectionFailureValue{code: selectionPreparationNotFound, cause: source}
			runtimeReleases := 0
			childReleases := 0
			var childCause error
			// childOnce models the production child release's once-only callback contract.
			var childOnce sync.Once
			contexts.EXPECT().ValidateContext("extension", "runtime", reference).Return(nil)
			runtime.EXPECT().BeginContextOperation(gomock.Any(), "extension", "runtime").Return(
				func() { runtimeReleases++ }, nil,
			)
			if correlated {
				policy.EXPECT().BeginCompactionChild("extension", "runtime", parent).Return(
					func(cause error) { childOnce.Do(func() { childReleases++; childCause = cause }) }, nil,
				)
			}
			selection.EXPECT().PrepareExtensionSelection(SelectionCommand{
				Kind: SelectionCommandModel, ExtensionID: "extension", RuntimeID: "runtime", Context: reference,
				Provider: "provider", Model: "missing", ReasoningChoice: "",
			}).Return(nil, classified)
			service := New(models, contexts, runtime, "extension", "runtime")
			service.BindSelection(selection)
			service.BindPolicyOperations(policy)
			request := new(extensionpb.ExtensionRequest)
			request.SetParentCompactionId(parent)
			request.SetSelectModel(extensionpb.SelectModelRequest_builder{
				Context: extensionpb.ExtensionContextRef_builder{
					ContextId: new(
						reference.ID,
					),
					RuntimeInstanceId: new(reference.RuntimeInstanceID),
					SessionId:         new(reference.SessionID),
				}.Build(), ProviderId: new("provider"), ModelId: new("missing"),
			}.Build())
			// Act through preparation after runtime and optional child admission.
			operation, err := service.Prepare(t.Context(), "selection-operation", request)
			// Assert preserved rejection, once-only scope cleanup, and the original admitted child cause.
			require.Nil(t, operation)
			require.ErrorIs(t, err, source)
			require.ErrorContains(t, err, source.Error())
			var rejection *extensionsdk.RejectionError
			require.ErrorAs(t, err, &rejection)
			require.Equal(t, publicSelectionNotFound, rejection.Code())
			require.Equal(t, 1, runtimeReleases)
			if correlated {
				require.Equal(t, 1, childReleases)
				require.ErrorIs(t, childCause, source)
				require.Same(t, err, childCause)
			} else {
				require.Zero(t, childReleases)
				require.NoError(t, childCause)
			}
		})
	}
}
