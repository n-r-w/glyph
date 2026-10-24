//go:build integration

package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/internal/errtree"

	operationpb "github.com/n-r-w/glyph/pkg/operation/v1"
	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestAutomaticPreparationLifetime checks joined children, captured generations and cancellation settlement.
func TestAutomaticPreparationLifetime(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		mode        string
		committed   bool
		independent bool
		killOwner   bool
	}{
		{
			name: "before commit", mode: automaticFixtureWaitBeforeCommitMode,
			committed: false, independent: false, killOwner: false,
		},
		{
			name: "before commit with child failure", mode: automaticFixtureWaitBeforeCommitMode,
			committed: false, independent: true, killOwner: false,
		},
		{name: "after commit", mode: automaticFixtureHoldFinalMode, committed: true, independent: false, killOwner: false},
		{
			name: "after commit with child failure", mode: automaticFixtureHoldFinalMode,
			committed: true, independent: true, killOwner: false,
		},
		{
			name: "captured owner disappears", mode: automaticFixtureWaitBeforeCommitMode,
			committed: false, independent: false, killOwner: true,
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			// Arrange coordination and reports before starting the application or policy helpers.
			const childText = "nested cancellation checkpoint"
			const independentText = "automatic child provider failure: complete original source"
			scratch := t.TempDir()
			outgoingPath := filepath.Join(scratch, "outgoing.json")
			pidPath := filepath.Join(scratch, "policy.pid")
			const pidEnvironment = "GLYPH_REFERENCE_CONVERSATION_PID"
			t.Setenv(pidEnvironment, pidPath)
			t.Setenv(automaticFixtureModeEnvironment, scenario.mode)
			t.Setenv(automaticFixtureTargetEnvironment, automaticFixtureTarget)
			t.Setenv(automaticFixtureOutgoingEnvironment, outgoingPath)
			started, stopped := make(chan struct{}), make(chan struct{})
			directory := buildManualPolicyFixture(t)
			paths := testPaths(t, configuredRequestSettings)
			writeProgrammaticCredentials(t, paths)
			base, bodies := newManualCompactionProviderTransport(t)
			transport := NewMockHTTPRoundTripper(gomock.NewController(t))
			transport.EXPECT().
				RoundTrip(gomock.Any()).
				AnyTimes().
				DoAndReturn(func(request *http.Request) (*http.Response, error) {
					response, err := base.RoundTrip(request)
					if err != nil {
						return nil, err
					}
					actual := bodies.snapshot()
					if !strings.Contains(string(actual[len(actual)-1]), childText) {
						return response, nil
					}
					defer close(stopped)
					close(started)
					<-request.Context().Done()
					cause := errors.Join(context.Cause(request.Context()), response.Body.Close())
					if scenario.independent {
						cause = errors.Join(cause, errors.New(independentText))
					}
					return nil, cause
				})
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			completeProgrammaticRequest(t, fixture, userRequest("automatic-cancel-seed", manualOriginalText))
			responses := make(chan *programmaticpb.OpenResponse)
			receiveErrors := make(chan error, 1)
			joined, stop := make(chan struct{}), make(chan struct{})
			defer func() {
				close(stop)
				defer func() { <-joined }()
				if scenario.killOwner {
					closeAutomaticLostOwner(t, fixture)
					return
				}
				fixture.closeOwner(t)
			}()
			go func() {
				defer close(joined)
				for {
					response, err := fixture.stream.Recv()
					if err != nil {
						receiveErrors <- err
						return
					}
					select {
					case responses <- response:
					case <-stop:
						return
					}
				}
			}()
			// Act through ordinary submission while its captured policy owns configured work.
			request := userRequest("automatic-cancel-next", automaticFixtureTarget)
			require.NoError(t, fixture.stream.Send(request))
			waiting := true
			for waiting {
				select {
				case <-started:
					waiting = false
				case err := <-receiveErrors:
					require.NoError(t, err)
				case response := <-responses:
					event := response.GetEvent()
					require.False(
						t,
						event.HasCompleted() || event.HasFailed() || event.HasCanceled(),
						"preparation ended before its child",
					)
				}
			}
			query := new(programmaticpb.OpenRequest)
			query.SetOperationId("automatic-held-history")
			query.SetRequest(new(programmaticpb.ControllerRequest))
			query.GetRequest().SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
			require.NoError(t, fixture.stream.Send(query))
			for {
				response := receiveAutomaticResponse(t, responses, receiveErrors)
				if response.GetOperationId() != query.GetOperationId() {
					continue
				}
				if !response.GetEvent().HasCompleted() {
					continue
				}
				originals, markers := countManualStoredEntries(
					response.GetEvent().GetCompleted().GetSessionEntries().GetEntries(),
				)
				require.Equal(t, 1, originals)
				if scenario.committed {
					require.Equal(t, 1, markers)
				} else {
					require.Zero(t, markers)
				}
				break
			}
			cancellation := new(programmaticpb.OpenRequest)
			cancellation.SetOperationId("automatic-cancel-command")
			cancellation.SetRequest(new(programmaticpb.ControllerRequest))
			cancellation.GetRequest().
				SetCancel(operationpb.CancelOperation_builder{TargetOperationId: new(request.GetOperationId())}.Build())
			if scenario.killOwner {
				encoded, err := os.ReadFile(pidPath)
				require.NoError(t, err)
				pid, err := strconv.Atoi(string(encoded))
				require.NoError(t, err)
				process, err := os.FindProcess(pid)
				require.NoError(t, err)
				require.NoError(t, process.Kill())
			} else {
				require.NoError(t, fixture.stream.Send(cancellation))
			}
			var terminal *programmaticpb.HostEvent
			for terminal == nil {
				response := receiveAutomaticResponse(t, responses, receiveErrors)
				if response.GetOperationId() != request.GetOperationId() {
					continue
				}
				event := response.GetEvent()
				if event.HasFailed() || event.HasCanceled() || event.HasCompleted() {
					terminal = event
				}
			}
			// Assert that Host joins the child, retains current causes and any receipt, and skips provider dispatch.
			select {
			case <-stopped:
			default:
				require.FailNow(t, "automatic preparation child was not joined")
			}
			require.Len(t, bodies.snapshot(), 2)
			if scenario.committed || scenario.independent || scenario.killOwner {
				require.NotNil(t, terminal.GetFailed(), automaticUnexpectedTerminalFormat, terminal)
				require.Contains(t, terminal.GetFailed().GetFailure().GetMessage(), context.Canceled.Error())
				if scenario.independent {
					require.Contains(t, terminal.GetFailed().GetFailure().GetMessage(), independentText)
				}
			} else {
				require.NotNil(t, terminal.GetCanceled(), automaticUnexpectedTerminalFormat, terminal)
				message := terminal.GetCanceled().GetMessage()
				if message != "" {
					const cancellationStep = "wait for cancellation of extension operation"
					const inactiveTarget = "is not active"
					require.Contains(t, message, context.Canceled.Error())
					require.Contains(t, message, cancellationStep)
					require.Contains(t, message, inactiveTarget)
				}
			}
			_, err := os.Stat(outgoingPath)
			if scenario.committed {
				require.NoError(t, err)
			} else {
				require.True(t, os.IsNotExist(err))
			}
			query.SetOperationId("automatic-settled-history")
			require.NoError(t, fixture.stream.Send(query))
			for {
				response := receiveAutomaticResponse(t, responses, receiveErrors)
				if response.GetOperationId() != query.GetOperationId() || !response.GetEvent().HasCompleted() {
					continue
				}
				originals, markers := countManualStoredEntries(
					response.GetEvent().GetCompleted().GetSessionEntries().GetEntries(),
				)
				require.Equal(t, 1, originals)
				if scenario.committed {
					require.Equal(t, 1, markers)
				} else {
					require.Zero(t, markers)
				}
				break
			}
		})
	}
}

// closeAutomaticLostOwner joins application shutdown after intentional policy-peer loss.
func closeAutomaticLostOwner(t *testing.T, fixture *programmaticFixture) {
	t.Helper()
	const childStep = "request configured model:"
	const childFailureKind = "MODEL_FAILED"
	const transportEOF = "error reading from server: EOF"
	const transportReset = "error reading from server: connection reset by peer"
	const shutdownCauseFormat = "unexpected owner-loss shutdown cause: %v"
	require.NoError(t, fixture.stream.CloseSend())
	defer fixture.assertClosed(t)
	shutdownErr := <-fixture.result
	if shutdownErr == nil {
		return
	}
	require.Equal(t, codes.Unavailable, status.Code(shutdownErr))
	require.ErrorContains(t, shutdownErr, childStep)
	require.ErrorContains(t, shutdownErr, childFailureKind)
	require.ErrorContains(t, shutdownErr, context.Canceled.Error())
	require.True(t, errtree.AllLeavesMatch(shutdownErr, func(cause error) bool {
		if errors.Is(cause, context.Canceled) {
			return true
		}
		transport, ok := status.FromError(cause)
		if !ok || transport.Code() != codes.Unavailable {
			return false
		}
		message := transport.Message()
		return message == transportEOF || message == transportReset ||
			(strings.Contains(message, childStep) && strings.Contains(message, childFailureKind) &&
				strings.HasSuffix(message, context.Canceled.Error()))
	}), shutdownCauseFormat, shutdownErr)
}

// receiveAutomaticResponse keeps stream assertions in the test goroutine.
func receiveAutomaticResponse(
	t *testing.T,
	responses <-chan *programmaticpb.OpenResponse,
	failures <-chan error,
) *programmaticpb.OpenResponse {
	t.Helper()
	select {
	case response := <-responses:
		return response
	case err := <-failures:
		require.NoError(t, err)
		return nil
	}
}

// TestAutomaticLostCompletionRetainsReceipt checks uncertain producer completion after durable replacement.
func TestAutomaticLostCompletionRetainsReceipt(t *testing.T) {
	// Arrange an owner that exits after its nested commit acknowledgement.
	t.Setenv(automaticFixtureModeEnvironment, automaticFixtureLoseAfterCommitMode)
	t.Setenv(automaticFixtureTargetEnvironment, automaticFixtureTarget)
	directory := buildManualPolicyFixture(t)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	defer fixture.closeOwner(t)
	completeProgrammaticRequest(t, fixture, userRequest("automatic-lost-seed", manualOriginalText))
	// Act through ordinary preparation while the policy loses completion after commit.
	require.NoError(t, fixture.stream.Send(userRequest("automatic-lost-next", automaticFixtureTarget)))
	terminal := receiveAutomaticTerminal(t, fixture, "automatic-lost-next").GetEvent()
	// Assert the real receipt survives and no ordinary provider request follows the uncertain completion.
	require.NotNil(t, terminal.GetFailed(), automaticUnexpectedTerminalFormat, terminal)
	require.Contains(t, terminal.GetFailed().GetFailure().GetMessage(), "reference-policy")
	require.Contains(t, terminal.GetFailed().GetFailure().GetMessage(), "generation")
	require.Len(t, bodies.snapshot(), 1)
	stored := sendProgrammaticOperation(
		t,
		fixture,
		"automatic-lost-history",
		func(request *programmaticpb.OpenRequest) {
			programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
		},
	).GetSessionEntries().
		GetEntries()
	originals, markers := countManualStoredEntries(stored)
	require.Equal(t, 1, originals)
	require.Equal(t, 1, markers)
}
