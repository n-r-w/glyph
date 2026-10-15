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

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// policyResultSuite checks operation-bound result selection and lifetime ownership.
type policyResultSuite struct {
	// Suite supplies scenario assertions.
	suite.Suite
}

// TestPolicyResultInvocation checks captured result dispatch through admitted policy authority.
func TestPolicyResultInvocation(t *testing.T) {
	t.Parallel()
	// Arrange a captured operation per scenario.
	// Act through the consumer-owned result contract.
	// Assert selection, authority, binding and lifetime behavior.
	suite.Run(t, &policyResultSuite{Suite: suite.Suite{}})
}

// TestSelectedAction checks original and current requests and results,
// a non-authorizing lifetime and joined normal cleanup.
func (s *policyResultSuite) TestSelectedAction() {
	t := s.T()
	// Arrange two result registrations and one explicit selection.
	first := Participant{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "first-result"}
	selected := Participant{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "second-result"}
	service, runtime, owner := newManualPolicyOperationFixture(
		t,
		HandlerSet{
			Requests:   nil,
			Generators: nil,
			Results:    []Participant{first, selected},
			Successes:  nil,
			Failures:   nil,
		},
	)
	target := Participant(selected)
	joined := make(chan struct{})
	runtime.EXPECT().
		InvokeCompactionResult(gomock.Any(), target, gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, participant Participant, binding extension.Context, input ResultInvocation) (ResultAction, error) {
			require.Equal(t, "initial request", input.OriginalRequest.Instructions.MustGet())
			require.Equal(t, "final request", input.CurrentRequest.Instructions.MustGet())
			require.Equal(t, "original result", input.OriginalResult.Summary)
			require.Equal(t, "current result", input.CurrentResult.Summary)
			_, bounded := ctx.Deadline()
			require.True(t, bounded)
			lifetime := input.ParentCompactionID.MustGet()
			_, err := service.findCompactionOperation(participant.ExtensionID, participant.RuntimeID, lifetime)
			require.ErrorContains(t, err, "authority is unavailable")
			release, err := service.BeginCompactionChild(participant.ExtensionID, binding.RuntimeInstanceID, lifetime)
			require.NoError(t, err)
			go func() {
				defer release(context.Canceled)
				<-ctx.Done()
				close(joined)
			}()
			return ResultAction{Cancel: false, ResultAction: ResultActionPreserve, Result: mo.None[Result]()}, nil
		})
	runtime.EXPECT().
		InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, captured PolicyOwner, binding extension.Context, input PolicyInvocation) (PolicyDisposition, error) {
			require.Equal(t, []Participant{first, selected}, input.Results)
			command := newPolicyResultCommand(input.OperationID, selected, 1)
			response, err := service.InvokePolicyResult(ctx, captured.ExtensionID, binding.RuntimeInstanceID, command)
			require.NoError(t, err)
			require.Equal(t, extensioncontroller.CompactionResultPreserve, response.ResultAction)
			require.True(t, response.Result.IsNone())
			select {
			case <-joined:
			default:
				require.FailNow(t, "result children were not joined")
			}
			return PolicyCanceled, nil
		})
	// Act through manual operation ownership.
	outcome, err := service.CompactManualPolicy(t.Context(), modelexecution.ProviderRequest{}, mo.None[string]())
	// Assert that the Service preserves success and releases all bindings during normal participant teardown.
	require.NoError(t, err)
	require.True(t, outcome.Canceled)
	require.Empty(t, service.compactionOperations)
}

// TestMembership checks authority, wrong-kind selection and captured result identity before dispatch.
func (s *policyResultSuite) TestMembership() {
	t := s.T()
	// Arrange disjoint request and result registrations with no allowed participant dispatch.
	handler := Participant{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "result"}
	wrong := Participant{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "request"}
	service, runtime, owner := newManualPolicyOperationFixture(
		t,
		HandlerSet{
			Requests:   []Participant{wrong},
			Generators: nil,
			Results:    []Participant{handler},
			Successes:  nil,
			Failures:   nil,
		},
	)
	runtime.EXPECT().
		InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, captured PolicyOwner, binding extension.Context, input PolicyInvocation) (PolicyDisposition, error) {
			for _, scenario := range []string{"authority", "caller", "runtime", "handler", "negative order", "outside order", "wrong kind"} {
				t.Run(scenario, func(t *testing.T) {
					command := newPolicyResultCommand(input.OperationID, handler, 0)
					caller := captured.ExtensionID
					want := "identity does not match"
					switch scenario {
					case "authority":
						command.OperationID = input.LifecycleID
						want = "authority is unavailable"
					case "caller":
						caller = "foreign"
						want = "does not belong to extension"
					case "runtime":
						command.Participant.RuntimeID = "successor"
					case "handler":
						command.Participant.HandlerID = "unknown"
					case "negative order":
						command.Participant.RegistrationOrder = -1
						want = "not captured"
					case "outside order":
						command.Participant.RegistrationOrder = 1
						want = "not captured"
					case "wrong kind":
						command.Participant.HandlerID = wrong.HandlerID
					}
					// Act at the selected invocation boundary.
					_, err := service.InvokePolicyResult(ctx, caller, binding.RuntimeInstanceID, command)
					// Assert rejected authority or membership without runtime dispatch.
					require.ErrorContains(t, err, want)
				})
			}
			return PolicyCanceled, nil
		})
	// Act through the admitted parent.
	_, err := service.CompactManualPolicy(t.Context(), modelexecution.ProviderRequest{}, mo.None[string]())
	// Assert complete release without mutation.
	require.NoError(t, err)
	require.Empty(t, service.compactionOperations)
}

// TestRetainedFailure checks participant and acquired child causes after cleanup.
func (s *policyResultSuite) TestRetainedFailure() {
	t := s.T()
	// Arrange an invoked participant with independent execution and child causes.
	handler := Participant{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "result"}
	service, runtime, owner := newManualPolicyOperationFixture(
		t,
		HandlerSet{
			Requests:   nil,
			Generators: nil,
			Results:    []Participant{handler},
			Successes:  nil,
			Failures:   nil,
		},
	)
	invokeCause := errors.New("result invocation cause")
	cleanupCause := errors.New("result nested cleanup cause")
	runtime.EXPECT().
		InvokeCompactionResult(gomock.Any(), Participant(handler), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, participant Participant, binding extension.Context, input ResultInvocation) (ResultAction, error) {
			release, err := service.BeginCompactionChild(
				participant.ExtensionID,
				binding.RuntimeInstanceID,
				input.ParentCompactionID.MustGet(),
			)
			require.NoError(t, err)
			go func() { <-ctx.Done(); release(cleanupCause) }()
			return ResultAction{}, invokeCause
		})
	runtime.EXPECT().
		InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, captured PolicyOwner, binding extension.Context, input PolicyInvocation) (PolicyDisposition, error) {
			_, err := service.InvokePolicyResult(
				ctx,
				captured.ExtensionID,
				binding.RuntimeInstanceID,
				newPolicyResultCommand(input.OperationID, handler, 0),
			)
			require.ErrorIs(t, err, invokeCause)
			require.ErrorIs(t, err, cleanupCause)
			return PolicyReplaced, err
		})
	// Act through the owning manual operation.
	_, err := service.CompactManualPolicy(t.Context(), modelexecution.ProviderRequest{}, mo.None[string]())
	// Assert that the Service retains all acquired causes after settlement and releases bindings.
	require.ErrorIs(t, err, invokeCause)
	require.ErrorIs(t, err, cleanupCause)
	require.Empty(t, service.compactionOperations)
}

// newPolicyResultCommand supplies detached original and current inputs for layer scenarios.
func newPolicyResultCommand(
	operationID string,
	handler Participant,
	order int64,
) extensioncontroller.CompactionResultCommand {
	request := newPolicyRequestCommand(operationID, handler, order)
	current := request.Current
	current.Instructions = mo.Some("final request")
	original := session.CompactionEntry{
		Summary:          "original result",
		FirstKeptEntryID: "leaf",
		Source: session.CompactionSource{
			ExtensionID: mo.Some("previous-producer"),
			Requests:    nil,
		},
		Details: mo.None[[]byte](),
	}
	result := original
	result.Summary = "current result"
	return extensioncontroller.CompactionResultCommand{
		OperationID:     operationID,
		Participant:     request.Participant,
		OriginalRequest: request.Original,
		CurrentRequest:  current,
		OriginalResult:  original,
		CurrentResult:   result,
	}
}
