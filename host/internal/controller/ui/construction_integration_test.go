//go:build integration

package ui

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/internal/operation"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestExecuteCleansUpInvalidOutput verifies construction failure cancels transport and retains cleanup causes.
func TestExecuteCleansUpInvalidOutput(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"attachment failure", "missing delivery"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange an initialized session with output construction that cannot complete.
			controller := gomock.NewController(t)
			connection := NewMockConnection(controller)
			session := NewMockSession(controller)
			session.EXPECT().Initialize(gomock.Any()).Return(nil)
			source := errors.New("output attachment source")
			cleanup := errors.New("request half-close source")
			var attachedWriter *operation.Writer[*uiv1.OpenRequest]
			if name == "attachment failure" {
				connection.EXPECT().
					AttachOutput(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, nil, source)
			} else {
				writer, err := operation.NewWriter(func(*uiv1.OpenRequest) error { return nil })
				require.NoError(t, err)
				attachedWriter = writer
				connection.EXPECT().
					AttachOutput(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(writer, nil, nil)
				connection.EXPECT().ClearOutput()
			}
			connection.EXPECT().Cancel()
			connection.EXPECT().CloseSend().Return(cleanup)
			service := &Service{source: nil, connection: connection}
			// Act through the public stream owner without activating session work.
			err := service.Execute(t.Context(), session)
			// Assert construction and cleanup errors both reach the caller.
			require.ErrorIs(t, err, cleanup)
			require.ErrorContains(t, err, cleanup.Error())
			if attachedWriter != nil {
				require.ErrorIs(t, attachedWriter.Enqueue(new(uiv1.OpenRequest), nil), operation.ErrClosed)
			}
			if name == "attachment failure" {
				require.ErrorIs(t, err, source)
			} else {
				require.ErrorContains(t, err, "operation delivery is required")
			}
		})
	}
}
