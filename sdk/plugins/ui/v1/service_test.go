//go:build !integration

package uiv1

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/internal/operation"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestSendUIResponseSkipsTransportAfterCancellation verifies writer shutdown before blocked send.
func TestSendUIResponseSkipsTransportAfterCancellation(t *testing.T) {
	t.Parallel()

	// Arrange a canceled connection before writer delivery starts.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	sendCalls := 0

	// Act through the SDK send boundary.
	err := sendUIResponse(ctx, new(uiv1.OpenResponse), func(*uiv1.OpenResponse) error {
		sendCalls++
		return nil
	})

	// Assert transport Send is not entered after connection cancellation.
	assert.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, sendCalls)
}

// TestHostOutboundQueueOverflowReportsConnectionFailure verifies SDK producer backpressure ownership.
func TestHostOutboundQueueOverflowReportsConnectionFailure(t *testing.T) {
	t.Parallel()

	// Arrange one SDK Host with a production writer whose transport loop is blocked.
	writer := operation.NewWriter(func(*uiv1.OpenResponse) error { return nil })
	tracker := operation.NewTracker[*uiv1.HostProgress, *uiv1.HostCompleted]()
	failure := make(chan error, 1)
	host := newHost(t.Context(), writer, tracker, func(err error) { failure <- err }, func() {})
	request := new(uiv1.UIRequest)
	request.SetSubmit(uiv1.SubmitCommand_builder{Text: new("work")}.Build())
	var overflowErr error

	// Act by filling the exact production queue without starting its transport loop.
	for index := range 100 {
		_, overflowErr = host.Start(t.Context(), fmt.Sprintf("operation-%d", index), request)
		if overflowErr != nil {
			break
		}
	}

	// Assert queue overflow reaches the connection failure callback with its source cause.
	require.Error(t, overflowErr)
	assert.ErrorIs(t, overflowErr, operation.ErrQueueFull)
	select {
	case reported := <-failure:
		assert.ErrorIs(t, reported, operation.ErrQueueFull)
		assert.ErrorContains(t, reported, operation.ErrQueueFull.Error())
	default:
		t.Fatal("SDK queue overflow did not report connection failure")
	}
	tracker.Close()
	writer.Close()
	host.closeEvents()
}

// TestNestedPayloadValidationRejectsMissingRequiredFields verifies tracker input guards.
func TestNestedPayloadValidationRejectsMissingRequiredFields(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		validate func() error
	}{
		{name: "authorization URL", validate: func() error {
			progress := new(uiv1.HostProgress)
			progress.SetAuthorization(new(uiv1.AuthorizationRequest))
			return validateHostProgressFields(progress)
		}},
		{name: "agent run ID", validate: func() error {
			progress := new(uiv1.HostProgress)
			progress.SetAgentEvent(uiv1.AgentEvent_builder{
				Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START), RunId: nil, Text: nil,
				ToolCallId: nil, ToolName: nil, ProgressChannel: nil, IsError: nil, Outcome: nil,
				ErrorMessage: nil, Availability: nil, ModelContent: nil, ModelResponse: nil,
				ToolCallPreview: nil, FinalToolCall: nil, ToolResultContents: nil,
			}.Build())
			return validateHostProgressFields(progress)
		}},
		{name: "cancellation target state", validate: func() error {
			completed := new(uiv1.HostCompleted)
			completed.SetCancel(new(operationv1.CancelCompleted))
			return validateHostCompletedFields(completed)
		}},
		{name: "model selection", validate: func() error {
			completed := new(uiv1.HostCompleted)
			completed.SetModelSelection(new(uiv1.ModelSelectionChanged))
			return validateHostCompletedFields(completed)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange the case-specific malformed progress or completion payload validator.

			// Act by validating the selected nested payload.
			err := test.validate()

			// Assert validation rejects the payload before tracker delivery.
			require.Error(t, err)
		})
	}
}

// TestStreamStatusPreservesIncomingGRPCStatus verifies transport category and complete text.
func TestStreamStatusPreservesIncomingGRPCStatus(t *testing.T) {
	t.Parallel()
	// Arrange an incoming DataLoss status with complete transport text.
	source := status.Error(codes.DataLoss, "transport decode failed completely")

	// Act by wrapping and mapping the incoming status through streamStatus.
	mapped := streamStatus(fmt.Errorf("receive UI stream: %w", source))

	// Assert the mapped status preserves both the code and complete text.
	assert.Equal(t, codes.DataLoss, status.Code(mapped))
	assert.ErrorContains(t, mapped, "transport decode failed completely")
}

// TestProtocolFaultMapsFailedPrecondition verifies local contract-fault classification.
func TestProtocolFaultMapsFailedPrecondition(t *testing.T) {
	t.Parallel()
	// Arrange a local lifecycle-order contract fault with complete text.
	source := errors.New("running arrived before accepted")

	// Act by mapping the protocol fault through streamStatus.
	mapped := streamStatus(protocolFault(source))

	// Assert the fault becomes FailedPrecondition and retains its text.
	assert.Equal(t, codes.FailedPrecondition, status.Code(mapped))
	assert.ErrorContains(t, mapped, "running arrived before accepted")
}

// TestReceivedErrorsPreserveCategoryTextAndCause verifies terminal SDK error reconstruction.
func TestReceivedErrorsPreserveCategoryTextAndCause(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		kind       operation.EventKind
		code       string
		text       string
		assertCode func(error) string
	}{
		{
			name: "rejected", kind: operation.EventRejected, code: "NOT_READY", text: "Host UI is not ready: startup pending",
			assertCode: func(err error) string {
				var classified *RejectionError
				require.ErrorAs(t, err, &classified)
				return classified.Code()
			},
		},
		{
			name: "failed", kind: operation.EventFailed, code: "MODEL_FAILED", text: "run model: provider socket closed",
			assertCode: func(err error) string {
				var classified *FailureError
				require.ErrorAs(t, err, &classified)
				return classified.Code()
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange one tracked operation and its valid lifecycle prefix.
			tracker := operation.NewTracker[*uiv1.HostProgress, *uiv1.HostCompleted]()
			events, err := tracker.Track("operation")
			require.NoError(t, err)
			if test.kind == operation.EventFailed {
				require.NoError(t, tracker.Handle(operation.Event[*uiv1.HostProgress, *uiv1.HostCompleted]{
					ID: "operation", Kind: operation.EventAccepted,
				}))
				require.NoError(t, tracker.Handle(operation.Event[*uiv1.HostProgress, *uiv1.HostCompleted]{
					ID: "operation", Kind: operation.EventRunning,
				}))
			}
			require.NoError(t, tracker.Handle(operation.Event[*uiv1.HostProgress, *uiv1.HostCompleted]{
				ID: "operation", Kind: test.kind, Code: test.code, Message: test.text,
			}))
			started := &Operation{id: "operation", events: events}

			// Act through the public Wait method.
			_, err = started.Wait(t.Context(), nil)

			// Assert category, complete text, concrete type, Unwrap, and errors.Is.
			require.EqualError(t, err, test.text)
			assert.Equal(t, test.code, test.assertCode(err))
			cause := errors.Unwrap(err)
			require.Error(t, cause)
			assert.Equal(t, test.text, cause.Error())
			assert.ErrorIs(t, err, cause)
		})
	}
}

// TestClassifiedErrorsPreserveCategoryTextAndCause verifies the public SDK error surface.
func TestClassifiedErrorsPreserveCategoryTextAndCause(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		make func(error) error
		as   func(error) string
	}{
		{name: "rejection", make: func(cause error) error { return Reject("NOT_READY", cause) }, as: func(err error) string {
			var classified *RejectionError
			require.ErrorAs(t, err, &classified)
			return classified.Code()
		}},
		{name: "failure", make: func(cause error) error { return Fail("INTERNAL", cause) }, as: func(err error) string {
			var classified *FailureError
			require.ErrorAs(t, err, &classified)
			return classified.Code()
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange one wrapped source failure.
			source := errors.New("socket: original failure")

			// Act through the public classified error constructor.
			err := test.make(source)

			// Assert category, complete text, Unwrap, errors.Is, and errors.As.
			assert.Equal(t, source.Error(), err.Error())
			assert.ErrorIs(t, err, source)
			assert.Equal(t, source, errors.Unwrap(err))
			assert.NotEmpty(t, test.as(err))
		})
	}
}
