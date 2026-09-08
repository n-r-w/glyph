//go:build !integration

package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// TestConnectionDiagnosticPreservesIndependentCauses selects complete diagnostics without testing logs.
func TestConnectionDiagnosticPreservesIndependentCauses(t *testing.T) {
	t.Parallel()
	// Arrange pure shutdown and independent protocol causes, including mixed trees.
	protocol := status.Error(codes.FailedPrecondition, "idle extension envelope has invalid lifecycle Ω")
	independent := errors.New("independent connection cleanup failed Ω")
	for index, cause := range []error{
		protocol, independent, errors.Join(context.Canceled, protocol),
		extensionsdk.Fail("INTERNAL", context.Canceled),
		extensionsdk.Reject("BUSY", context.Canceled),
		fmt.Errorf("connection stopped: %w", errors.Join(context.Canceled, independent)),
		extensionsdk.Reject("TARGET_NOT_ACTIVE", errors.Join(context.Canceled, independent)),
	} {
		t.Run(fmt.Sprintf("cause_%d", index), func(t *testing.T) {
			t.Parallel()
			// Act by selecting the original aggregate for nonfatal reporting.
			selected := connectionDiagnostic(cause)
			// Assert selection keeps the exact value and complete original text.
			require.ErrorIs(t, selected, cause)
			require.Equal(t, cause.Error(), selected.Error())
		})
	}
}

// TestConnectionDiagnosticSuppressesPureShutdown keeps expected closure out of diagnostic reporting.
func TestConnectionDiagnosticSuppressesPureShutdown(t *testing.T) {
	t.Parallel()
	// Arrange expected transport and inactive-target shutdown without independent causes.
	for _, cause := range []error{
		nil, context.Canceled, context.DeadlineExceeded, io.EOF,
		status.Error(codes.Canceled, "connection canceled"),
		extensionsdk.Reject("TARGET_NOT_ACTIVE", errors.New("target already stopped")),
		fmt.Errorf("shutdown: %w", errors.Join(context.Canceled, io.EOF)),
	} {
		// Act and assert pure shutdown requires no nonfatal diagnostic.
		require.NoError(t, connectionDiagnostic(cause))
	}
}
