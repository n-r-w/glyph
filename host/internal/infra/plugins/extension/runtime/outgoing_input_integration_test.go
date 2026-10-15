//go:build integration

package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// outgoingSemanticMode selects a received final-input rejection.
	outgoingSemanticMode = "outgoing-semantic-rejection"
	// outgoingProtocolMode selects a missing final-input acknowledgement.
	outgoingProtocolMode = "outgoing-protocol-failure"
	// outgoingSemanticText identifies the full remote semantic source.
	outgoingSemanticText = "outgoing budget rejected\nsource β"
	// outgoingInvocationText identifies an independent remote cancellation-invocation failure.
	outgoingInvocationText = "independent outgoing invocation failed\nsource γ"
)

// TestOutgoingInputRetainsSourcePartition checks semantic rejection and independent invocation failure over gRPC.
func TestOutgoingInputRetainsSourcePartition(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name        string
		mode        string
		cancel      bool
		semantic    bool
		independent bool
		category    modelexecution.FailureCategory
	}{
		{
			name: "semantic", mode: outgoingSemanticMode, cancel: false, semantic: true,
			independent: false, category: modelexecution.FailureCompactionFailed,
		},
		{
			name: "protocol", mode: outgoingProtocolMode, cancel: false, semantic: false,
			independent: false, category: modelexecution.FailureExtensionFailed,
		},
		{
			name: "semantic with owning cancellation", mode: settlementPeerPrefix + "completed-original-first",
			cancel: true, semantic: true, independent: false, category: modelexecution.FailureCompactionFailed,
		},
		{
			name:   "semantic with invocation failure",
			mode:   settlementPeerPrefix + "completed-independent-cancel-cancel-first",
			cancel: true, semantic: true, independent: true, category: modelexecution.FailureExtensionFailed,
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			// Arrange a real final-input handler and channel-controlled owning cancellation.
			peer := startHelperRuntime(t, scenario.mode)
			_, err := peer.Register(t.Context())
			require.NoError(t, err)
			admitted := make(chan struct{})
			if scenario.cancel {
				controller := gomock.NewController(t)
				host := NewMockHostService(controller)
				nested := NewMockHostOperation(controller)
				host.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).Return(nested, nil)
				nested.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
					func(context.Context, *extensionsdk.HostProgressReporter) (*extensionpb.HostCompleted, error) {
						close(admitted)
						return nil, context.Canceled
					})
				nested.EXPECT().Release()
				peer.connection.BindHostService(host)
			}
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			joined := make(chan struct{})
			go func() {
				defer close(joined)
				done <- peer.ValidateOutgoingInput(ctx, "outgoing", runtimeTestContext(), extensioncontext.OutgoingInput{})
			}()
			defer func() { cancel(); <-joined }()

			// Act after the peer has admitted the original invocation.
			if scenario.cancel {
				<-admitted
				cancel()
			}
			err = <-done

			// Assert source annotations retain semantic text and independent current invocation causes.
			require.Error(t, err)
			if scenario.semantic {
				require.ErrorContains(t, err, outgoingSemanticText)
			}
			if scenario.independent {
				require.ErrorContains(t, err, outgoingInvocationText)
			}
			if scenario.cancel {
				require.ErrorIs(t, err, context.Canceled)
			}
			source, present := errors.AsType[*extensioncontext.PolicyOperationError](err)
			require.True(t, present)
			require.Equal(t, scenario.category, source.Category)
			_, classified := errors.AsType[modelexecution.ContextPreparationFailure](err)
			require.False(t, classified)
		})
	}
}
