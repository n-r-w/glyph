//go:build !integration

// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// policyRequestInvocationSuite isolates captured invocation authority and lifetime ownership.
type policyRequestInvocationSuite struct {
	// Suite supplies scenario assertions.
	suite.Suite
}

// TestPolicyRequestInvocation checks one selected participant, authority, and acquired causes.
func TestPolicyRequestInvocation(t *testing.T) {
	t.Parallel()
	// Arrange isolated participant-authority and lifetime scenarios.
	// Act through captured participant admission and invocation.
	// Assert independent actions, authority rejection, and acquired causes in each scenario.
	suite.Run(t, &policyRequestInvocationSuite{Suite: suite.Suite{}})
}

// TestSelectedActionAndAuthority checks typed independent actions without participant authority in a shared runtime.
func (s *policyRequestInvocationSuite) TestSelectedActionAndAuthority() {
	t := s.T()
	// Arrange two captured handlers in the policy's own runtime.
	handlers := []contextcompaction.Handler{
		{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "not-selected"},
		{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "selected"},
	}
	service, runtime, owner := newManualPolicyOperationFixture(
		t,
		contextcompaction.HandlerSet{Requests: handlers, Generators: nil, Results: nil, Successes: nil, Failures: nil},
	)
	runtime.EXPECT().
		InvokeCompactionRequest(
			gomock.Any(),
			Participant{
				ExtensionID: handlers[1].ExtensionID,
				RuntimeID:   handlers[1].RuntimeID,
				HandlerID:   handlers[1].HandlerID,
			},
			gomock.Any(), gomock.Any(),
		).
		DoAndReturn(
			func(
				ctx context.Context, target Participant, bound extension.Context, input RequestInvocation,
			) (RequestAction, error) {
				require.Equal(t, "selected", target.HandlerID)
				require.Equal(t, owner.RuntimeID, bound.RuntimeInstanceID)
				lifetime, present := input.ParentCompactionID.Get()
				require.True(t, present)
				// Host admits ordinary nested work under a participant lifetime.
				// Host rejects policy replacement and participant invocation under that lifetime.
				release, err := service.BeginCompactionChild(owner.ExtensionID, owner.RuntimeID, lifetime)
				require.NoError(t, err)
				nested, unlink := service.LinkCompactionChild(ctx, lifetime)
				require.NoError(t, nested.Err())
				_, err = service.CommitPolicyReplacement(
					nested,
					owner.ExtensionID,
					owner.RuntimeID,
					lifetime,
					session.CompactionEntry{},
				)
				require.ErrorContains(t, err, "authority is unavailable")
				command := newPolicyRequestCommand(lifetime, handlers[1], 1)
				_, err = service.InvokePolicyRequest(nested, owner.ExtensionID, owner.RuntimeID, command)
				require.ErrorContains(t, err, "authority is unavailable")
				unlink()
				release(nil)
				request := input.Current
				request.Instructions = mo.Some("handler-selected request")
				return RequestAction{
					Cancel:        false,
					RequestAction: RequestActionReplace,
					Request:       mo.Some(request),
					ResultAction:  ResultActionClear,
					Result:        mo.None[Result](),
				}, nil
			},
		)
	runtime.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, captured PolicyOwner, bound extension.Context, input PolicyInvocation) (bool, error) {
			require.Equal(t, newParticipants(handlers), input.Requests)
			command := newPolicyRequestCommand(input.OperationID, handlers[1], 1)
			response, err := service.InvokePolicyRequest(ctx, captured.ExtensionID, bound.RuntimeInstanceID, command)
			require.NoError(t, err)
			require.False(t, response.Cancel)
			require.Equal(t, extensioncontroller.CompactionRequestReplace, response.RequestAction)
			require.Equal(t, "handler-selected request", response.Request.MustGet().Instructions.MustGet())
			require.Equal(t, extensioncontroller.CompactionResultClear, response.ResultAction)
			require.True(t, response.Result.IsNone())
			require.Equal(t, "initial request", command.Original.Instructions.MustGet())
			require.Equal(t, "initial request", command.Current.Instructions.MustGet())
			require.True(t, command.CurrentResult.IsSome(), "Host must return rather than apply the action")
			return true, nil
		},
	)
	// Act through the admitted policy operation.
	result, err := service.CompactManualPolicy(t.Context(), modelexecution.ProviderRequest{}, mo.None[string]())
	// Assert that no Host-selected replacement was applied.
	require.NoError(t, err)
	require.True(t, result.Canceled)
	require.True(t, result.Committed.IsNone())
}

// TestAuthorityAndMembershipRejection checks caller authority and every captured target identity component.
func (s *policyRequestInvocationSuite) TestAuthorityAndMembershipRejection() {
	t := s.T()
	// Arrange one captured request handler without a dispatch expectation.
	handler := contextcompaction.Handler{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "selected"}
	service, runtime, owner := newManualPolicyOperationFixture(
		t,
		contextcompaction.HandlerSet{
			Requests:   []contextcompaction.Handler{handler},
			Generators: nil,
			Results:    nil,
			Successes:  nil,
			Failures:   nil,
		},
	)
	runtime.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, captured PolicyOwner, bound extension.Context, input PolicyInvocation) (bool, error) {
			cases := []struct {
				name       string
				mutate     func(*extensioncontroller.CompactionRequestCommand)
				caller     string
				generation string
			}{
				{
					name:       "unknown authority",
					mutate:     func(c *extensioncontroller.CompactionRequestCommand) { c.OperationID = "unknown" },
					caller:     captured.ExtensionID,
					generation: bound.RuntimeInstanceID,
				},
				{
					name:       "lifetime is not authority",
					mutate:     func(c *extensioncontroller.CompactionRequestCommand) { c.OperationID = input.LifecycleID },
					caller:     captured.ExtensionID,
					generation: bound.RuntimeInstanceID,
				},
				{name: "wrong caller", mutate: nil, caller: "other", generation: bound.RuntimeInstanceID},
				{name: "wrong caller generation", mutate: nil, caller: captured.ExtensionID, generation: "replacement"},
				{
					name:       "wrong extension",
					mutate:     func(c *extensioncontroller.CompactionRequestCommand) { c.Participant.ExtensionID = "other" },
					caller:     captured.ExtensionID,
					generation: bound.RuntimeInstanceID,
				},
				{
					name:       "wrong generation",
					mutate:     func(c *extensioncontroller.CompactionRequestCommand) { c.Participant.RuntimeID = "replacement" },
					caller:     captured.ExtensionID,
					generation: bound.RuntimeInstanceID,
				},
				{
					name:       "wrong handler",
					mutate:     func(c *extensioncontroller.CompactionRequestCommand) { c.Participant.HandlerID = "not-captured" },
					caller:     captured.ExtensionID,
					generation: bound.RuntimeInstanceID,
				},
				{
					name:       "negative position",
					mutate:     func(c *extensioncontroller.CompactionRequestCommand) { c.Participant.RegistrationOrder = -1 },
					caller:     captured.ExtensionID,
					generation: bound.RuntimeInstanceID,
				},
				{
					name:       "uncaptured position",
					mutate:     func(c *extensioncontroller.CompactionRequestCommand) { c.Participant.RegistrationOrder = 1 },
					caller:     captured.ExtensionID,
					generation: bound.RuntimeInstanceID,
				},
			}
			for _, testCase := range cases {

				// Arrange an invalid command for this captured operation.
				command := newPolicyRequestCommand(input.OperationID, handler, 0)
				if testCase.mutate != nil {
					testCase.mutate(&command)
				}
				// Act through the authority and membership boundary.
				_, err := service.InvokePolicyRequest(ctx, testCase.caller, testCase.generation, command)
				// Assert rejection before any dispatch.
				assert.Error(t, err, testCase.name)
			}
			return true, nil
		},
	)
	// Act through the owning operation.
	result, err := service.CompactManualPolicy(t.Context(), modelexecution.ProviderRequest{}, mo.None[string]())
	// Assert explicit policy cancellation with no committed replacement.
	require.NoError(t, err)
	require.True(t, result.Canceled)
	require.True(t, result.Committed.IsNone())
}

// TestHandledNestedCause checks acquired participant-child failure in a later terminal policy failure.
func (s *policyRequestInvocationSuite) TestHandledNestedCause() {
	t := s.T()
	// Arrange a participant that handles a nested failure before returning a valid action.
	handler := contextcompaction.Handler{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "selected"}
	service, runtime, owner := newManualPolicyOperationFixture(
		t,
		contextcompaction.HandlerSet{
			Requests:   []contextcompaction.Handler{handler},
			Generators: nil,
			Results:    nil,
			Successes:  nil,
			Failures:   nil,
		},
	)
	nestedCause := errors.New("handled participant child source")
	terminalCause := errors.New("terminal policy source")
	runtime.EXPECT().
		InvokeCompactionRequest(
			gomock.Any(),
			Participant{ExtensionID: handler.ExtensionID, RuntimeID: handler.RuntimeID, HandlerID: handler.HandlerID},
			gomock.Any(), gomock.Any(),
		).
		DoAndReturn(
			func(
				ctx context.Context, target Participant, bound extension.Context, input RequestInvocation,
			) (RequestAction, error) {
				release, err := service.BeginCompactionChild(
					target.ExtensionID,
					bound.RuntimeInstanceID,
					input.ParentCompactionID.MustGet(),
				)
				require.NoError(t, errors.Join(err, ctx.Err()))
				release(nestedCause)
				return RequestAction{
					Cancel:        false,
					RequestAction: RequestActionPreserve,
					Request:       mo.None[Request](),
					ResultAction:  ResultActionPreserve,
					Result:        mo.None[Result](),
				}, nil
			},
		)
	runtime.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, captured PolicyOwner, bound extension.Context, input PolicyInvocation) (bool, error) {
			// Act through one participant and then a terminal policy failure.
			_, err := service.InvokePolicyRequest(
				ctx,
				captured.ExtensionID,
				bound.RuntimeInstanceID,
				newPolicyRequestCommand(input.OperationID, handler, 0),
			)
			require.NoError(t, err, "handled child failure must not fail a valid participant action")
			return false, terminalCause
		},
	)
	// Act through the owning policy operation.
	result, err := service.CompactManualPolicy(t.Context(), modelexecution.ProviderRequest{}, mo.None[string]())
	// Assert complete cause identity and no replacement.
	require.ErrorIs(t, err, nestedCause)
	require.ErrorIs(t, err, terminalCause)
	require.True(t, result.Committed.IsNone())
}

// TestUnavailableCapturedGeneration checks that a replacement runtime cannot receive a captured call.
func (s *policyRequestInvocationSuite) TestUnavailableCapturedGeneration() {
	t := s.T()
	// Arrange a captured participant whose current runtime is a replacement generation.
	controller := gomock.NewController(t)
	runtimeState := NewMockRuntimeState(controller)
	sessions := NewMockSessionState(controller)
	runtime := NewMockPolicyRuntime(controller)
	snapshots := NewMockCompactionSessions(controller)
	owner := PolicyOwner{ExtensionID: "policy", RuntimeID: "policy-generation", HandlerID: "manual"}
	handler := contextcompaction.Handler{
		ExtensionID: "participant",
		RuntimeID:   "captured-generation",
		HandlerID:   "selected",
	}
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	runtimeState.EXPECT().ContextRuntime(owner.ExtensionID).Return(owner.RuntimeID, true)
	runtimeState.EXPECT().ContextRuntime(handler.ExtensionID).Return("replacement-generation", true)
	sessions.EXPECT().ContextSession().Return(identity).AnyTimes()
	runtime.EXPECT().SnapshotCompactionPolicies().Return([]PolicyOwner{owner})
	runtime.EXPECT().
		SnapshotCompactionHandlers().
		Return(contextcompaction.HandlerSet{Requests: []contextcompaction.Handler{handler}, Generators: nil, Results: nil, Successes: nil, Failures: nil})
	snapshots.EXPECT().
		CaptureCompaction().
		Return(CompactionSnapshot{Identity: identity, ActiveLeafID: mo.Some("leaf"), Entries: nil, History: nil, Previous: mo.None[session.CompactionEntry]()})
	service := New(runtimeState, sessions)
	service.BindManualPolicy(runtime, snapshots, 5*time.Minute)
	runtime.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, captured PolicyOwner, bound extension.Context, input PolicyInvocation) (bool, error) {
			_, err := service.InvokePolicyRequest(
				ctx,
				captured.ExtensionID,
				bound.RuntimeInstanceID,
				newPolicyRequestCommand(input.OperationID, handler, 0),
			)
			assert.ErrorContains(t, err, "captured request participant runtime or session changed")
			return false, err
		},
	)
	// Act through the owning operation without an expectation for participant dispatch.
	result, err := service.CompactManualPolicy(t.Context(), modelexecution.ProviderRequest{}, mo.None[string]())
	// Assert terminal generation rejection rather than a replacement-runtime fallback.
	require.ErrorContains(t, err, "captured request participant runtime or session changed")
	require.True(t, result.Committed.IsNone())
}

// newPolicyRequestCommand supplies complete independent request and result input values.
func newPolicyRequestCommand(
	operationID string,
	handler contextcompaction.Handler,
	order int64,
) extensioncontroller.CompactionRequestCommand {
	request := extensioncontroller.CompactionRequestValue{
		Trigger:                extensioncontroller.CompactionRequestManual,
		RetryIntent:            false,
		Instructions:           mo.Some("initial request"),
		Model:                  model.Descriptor{},
		ReasoningChoice:        "",
		Prefix:                 nil,
		Suffix:                 nil,
		Previous:               mo.None[session.CompactionEntry](),
		ContextTokens:          0,
		ContextTokensEstimated: true,
		ContextWindow:          0,
		ResponseBudget:         0,
		RetainedBudget:         0,
	}
	result := session.CompactionEntry{
		Summary:          "supplied result",
		FirstKeptEntryID: "leaf",
		Source: session.CompactionSource{
			ExtensionID: mo.Some(handler.ExtensionID),
			Model:       mo.None[session.BranchSummaryModelSource](),
		},
		EstimatedCost: mo.None[session.EstimatedCost](),
		Details:       mo.None[[]byte](),
	}
	return extensioncontroller.CompactionRequestCommand{
		OperationID: operationID,
		Participant: extensioncontroller.CompactionParticipant{
			ExtensionID:       handler.ExtensionID,
			RuntimeID:         handler.RuntimeID,
			HandlerID:         handler.HandlerID,
			RegistrationOrder: order,
		},
		Original:      request,
		Current:       request,
		CurrentResult: mo.Some(result),
	}
}
