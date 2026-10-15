//go:build !integration

package extensioncontext

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestPolicyGeneratorInvocation checks generator membership in the captured set and execution without policy authority.
func TestPolicyGeneratorInvocation(t *testing.T) {
	t.Parallel()
	// Arrange a captured generator set in each scenario.
	// Act through manual policy authority in each scenario.
	// Assert exact dispatch, joined children and preserved result metadata.
	suite.Run(t, &policyGeneratorSuite{Suite: suite.Suite{}})
}

// policyGeneratorSuite checks the Host's individual generator operation contract.
type policyGeneratorSuite struct {
	// Suite supplies assertions for each operation scenario.
	suite.Suite
}

// TestSelectedResult checks exact selection, result metadata and successful normal teardown.
func (s *policyGeneratorSuite) TestSelectedResult() {
	t := s.T()
	// Arrange two captured generators with the second selected explicitly by the policy.
	first := Participant{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "first"}
	selected := Participant{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "second"}
	service, runtime, owner := newManualPolicyOperationFixture(t, HandlerSet{
		Requests:   nil,
		Generators: []Participant{first, selected},
		Results:    nil,
		Successes:  nil,
		Failures:   nil,
	})
	want := Result{
		Summary:          "generated summary",
		FirstKeptEntryID: "leaf",
		Source: session.CompactionSource{
			ExtensionID: mo.Some("policy"),
			Requests:    nil,
		},
		Details: mo.Some([]byte{}),
	}
	joined := make(chan struct{})
	target := Participant{
		ExtensionID: selected.ExtensionID, RuntimeID: selected.RuntimeID, HandlerID: selected.HandlerID,
	}
	runtime.EXPECT().
		InvokeCompactionGenerator(gomock.Any(), target, gomock.Any(), gomock.Any()).
		DoAndReturn(
			func(
				ctx context.Context, target Participant, binding extension.Context, invocation RequestInvocation,
			) (Result, error) {
				lifetime := invocation.ParentCompactionID.MustGet()
				require.NotEmpty(t, lifetime)
				require.Equal(t, "initial request", invocation.Original.Instructions.MustGet())
				require.Equal(t, "final request", invocation.Current.Instructions.MustGet())
				require.True(t, invocation.CurrentResult.IsNone())
				_, err := service.findCompactionOperation(target.ExtensionID, target.RuntimeID, lifetime)
				require.ErrorContains(t, err, "authority is unavailable")
				release, err := service.BeginCompactionChild(target.ExtensionID, binding.RuntimeInstanceID, lifetime)
				require.NoError(t, err)
				go func() {
					defer release(context.Canceled)
					<-ctx.Done()
					close(joined)
				}()
				return want, nil
			},
		)
	runtime.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, captured PolicyOwner, binding extension.Context, input PolicyInvocation) (PolicyDisposition, error) {
			require.Equal(t, []Participant{first, selected}, input.Generators)
			command := newPolicyGeneratorCommand(input.OperationID, selected, 1)
			command.Current.Instructions = mo.Some("final request")
			result, err := service.InvokePolicyGenerator(ctx, captured.ExtensionID, binding.RuntimeInstanceID, command)
			require.NoError(t, err)
			require.Equal(t, want.Summary, result.Summary)
			require.Equal(t, want.FirstKeptEntryID, result.FirstKeptEntryID)
			require.Equal(t, want.Source, result.Source)
			require.Equal(t, want.Details, result.Details)
			select {
			case <-joined:
			default:
				require.FailNow(t, "generator child was not joined")
			}
			return PolicyCanceled, nil
		},
	)
	// Act through the admitted manual operation.
	result, err := service.CompactManualPolicy(t.Context(), modelexecution.ProviderRequest{}, mo.None[string]())
	// Assert policy cancellation without mutation and complete lifetime release.
	require.NoError(t, err)
	require.True(t, result.Canceled)
	require.Empty(t, service.compactionOperations)
}

// TestMembership checks authority, captured order and identity before dispatch.
func (s *policyGeneratorSuite) TestMembership() {
	t := s.T()
	// Arrange a captured generator and no allowed runtime dispatch.
	handler := Participant{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "generator"}
	service, runtime, owner := newManualPolicyOperationFixture(t, HandlerSet{
		Requests: nil, Generators: []Participant{handler}, Results: nil, Successes: nil, Failures: nil,
	})
	runtime.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, captured PolicyOwner, binding extension.Context, input PolicyInvocation) (PolicyDisposition, error) {
			for _, scenario := range []string{
				"authority", "caller", "extension", "runtime", "handler", "negative order", "outside order",
			} {
				t.Run(scenario, func(t *testing.T) {
					command := newPolicyGeneratorCommand(input.OperationID, handler, 0)
					caller := captured.ExtensionID
					generation := binding.RuntimeInstanceID
					want := "identity does not match"
					switch scenario {
					case "authority":
						command.OperationID = input.LifecycleID
						want = "authority is unavailable"
					case "caller":
						caller = "other"
						want = "does not belong to extension"
					case "extension":
						command.Participant.ExtensionID = "other-generator"
					case "runtime":
						command.Participant.RuntimeID = "successor"
					case "handler":
						command.Participant.HandlerID = "unregistered"
					case "negative order":
						command.Participant.RegistrationOrder = -1
						want = "not captured"
					case "outside order":
						command.Participant.RegistrationOrder = 1
						want = "not captured"
					}
					// Act through the Host invocation boundary.
					_, err := service.InvokePolicyGenerator(ctx, caller, generation, command)
					// Assert rejection before dispatch.
					require.ErrorContains(t, err, want)
				})
			}
			return PolicyCanceled, nil
		},
	)
	// Act through the owning policy operation.
	_, err := service.CompactManualPolicy(t.Context(), modelexecution.ProviderRequest{}, mo.None[string]())
	// Assert no mutation and released operation state.
	require.NoError(t, err)
	require.Empty(t, service.compactionOperations)
}

// TestDeadline checks expiry, late success and earlier caller cancellation with child joins.
func (s *policyGeneratorSuite) TestDeadline() {
	for _, scenario := range []string{"participant", "late success", "caller deadline", "caller cancellation"} {
		s.Run(scenario, func() {
			t := s.T()
			synctest.Test(t, func(t *testing.T) {
				// Arrange bounded generation and an independently failing child cleanup.
				handler := Participant{
					ExtensionID: "policy",
					RuntimeID:   "generation",
					HandlerID:   "generator",
				}
				service, runtime, owner := newManualPolicyOperationFixture(t, HandlerSet{
					Requests:   nil,
					Generators: []Participant{handler},
					Results:    nil,
					Successes:  nil,
					Failures:   nil,
				})
				service.participantTimeout = 2 * time.Second
				caller := t.Context()
				callerCause := errors.New("caller cancellation source")
				want := context.DeadlineExceeded
				if scenario == "caller deadline" {
					var cancel context.CancelFunc
					caller, cancel = context.WithTimeoutCause(caller, time.Second, callerCause)
					defer cancel()
				}
				if scenario == "caller cancellation" {
					var cancel context.CancelCauseFunc
					caller, cancel = context.WithCancelCause(caller)
					defer cancel(nil)
					go func() {
						select {
						case <-time.After(time.Second):
							cancel(callerCause)
						case <-caller.Done():
						}
					}()
					want = context.Canceled
				}
				cleanupCause := errors.New("generator child cleanup source")
				joined := false
				runtime.EXPECT().
					InvokeCompactionGenerator(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(
						func(
							ctx context.Context, target Participant, binding extension.Context, invocation RequestInvocation,
						) (Result, error) {
							release, err := service.BeginCompactionChild(
								target.ExtensionID,
								binding.RuntimeInstanceID,
								invocation.ParentCompactionID.MustGet(),
							)
							require.NoError(t, err)
							go func() {
								<-ctx.Done()
								time.Sleep(time.Second)
								joined = true
								release(cleanupCause)
							}()
							<-ctx.Done()
							if scenario == "late success" {
								return Result{}, nil
							}
							return Result{}, context.Cause(ctx)
						},
					)
				runtime.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, captured PolicyOwner, binding extension.Context, input PolicyInvocation) (PolicyDisposition, error) {
						_, err := service.InvokePolicyGenerator(
							ctx,
							captured.ExtensionID,
							binding.RuntimeInstanceID,
							newPolicyGeneratorCommand(input.OperationID, handler, 0),
						)
						require.True(t, joined)
						return PolicyReplaced, err
					},
				)
				// Act through manual policy ownership under the fake clock.
				result, err := service.CompactManualPolicy(caller, modelexecution.ProviderRequest{}, mo.None[string]())
				// Assert deadline precedence, complete causes and release after child completion.
				require.ErrorIs(t, err, want)
				require.ErrorIs(t, err, cleanupCause)
				if scenario == "caller deadline" || scenario == "caller cancellation" {
					require.ErrorIs(t, err, callerCause)
				}
				require.True(t, joined)
				require.True(t, result.Committed.IsNone())
				require.Empty(t, service.compactionOperations)
			})
		})
	}
}

// newPolicyGeneratorCommand supplies complete original and current generator inputs.
func newPolicyGeneratorCommand(
	operationID string,
	handler Participant,
	order int64,
) extensioncontroller.CompactionGeneratorCommand {
	request := newPolicyRequestCommand(operationID, handler, order)
	return extensioncontroller.CompactionGeneratorCommand{
		OperationID: operationID,
		Participant: request.Participant,
		Original:    request.Original,
		Current:     request.Current,
	}
}
