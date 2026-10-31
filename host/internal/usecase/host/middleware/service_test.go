//go:build !integration

package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// TestWireComposition preserves immutable original values across ordered replacements.
func TestWireComposition(t *testing.T) {
	t.Parallel()
	// Arrange two handlers where the second discards the first using the original.
	ctrl := gomock.NewController(t)
	runtime := NewMockRuntime(ctrl)
	contexts := NewMockContextIssuer(ctrl)
	service := New(runtime, contexts, nil)
	service.CommitMiddlewareHandlers(
		[]startup.AcceptedRegistration{{ID: "wire", Path: "", Tools: nil, Handlers: []startup.AcceptedHandler{
			{ID: "a", Kind: startup.RawHandlerKind(26)}, {ID: "b", Kind: startup.RawHandlerKind(26)},
		}}},
	)
	contexts.EXPECT().IssueContext("wire").Return(extension.Context{}, nil).Times(2)
	gomock.InOrder(
		runtime.EXPECT().HandleWire(gomock.Any(), "wire", "a", gomock.Any()).DoAndReturn(
			func(_ context.Context, _, _ string, input Invocation) (Action, error) {
				require.Equal(t, "original", input.OriginalHeaders["X-Test"][0])
				input.OriginalHeaders["X-Test"][0] = "mutated"
				return Action{Kind: Replace, Headers: modelexecution.WireHeaders{"X-Test": {"A"}}, Body: nil}, nil
			}),
		runtime.EXPECT().HandleWire(gomock.Any(), "wire", "b", gomock.Any()).DoAndReturn(
			func(_ context.Context, _, _ string, input Invocation) (Action, error) {
				require.Equal(t, "original", input.OriginalHeaders["X-Test"][0])
				require.Equal(t, "A", input.CurrentHeaders["X-Test"][0])
				input.OriginalHeaders["X-Test"][0] += "B"
				return Action{Kind: Replace, Headers: input.OriginalHeaders, Body: nil}, nil
			}),
	)
	original := modelexecution.WireHeaders{"X-Test": {"original"}}
	// Act through the same neutral callbacks consumed by provider adapters.
	final, err := service.NewWireCallbacks().
		Headers(t.Context(), original, func(modelexecution.WireHeaders) error { return nil })
	// Assert ordered current/original semantics and ownership isolation.
	require.NoError(t, err)
	require.Equal(t, "originalB", final["X-Test"][0])
	require.Equal(t, "original", original["X-Test"][0])
}

// TestWireFailureStopsChain stops later observers and exposes complete handler errors or invalid actions.
func TestWireFailureStopsChain(t *testing.T) {
	t.Parallel()
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "handler_error", true: "invalid_action"}[invalid], func(t *testing.T) {
			t.Parallel()
			// Arrange a failing observer followed by an observer that must not run.
			ctrl := gomock.NewController(t)
			runtime := NewMockRuntime(ctrl)
			contexts := NewMockContextIssuer(ctrl)
			service := New(runtime, contexts, nil)
			service.CommitMiddlewareHandlers(
				[]startup.AcceptedRegistration{{ID: "wire", Path: "", Tools: nil, Handlers: []startup.AcceptedHandler{
					{ID: "a", Kind: startup.RawHandlerKind(28)}, {ID: "b", Kind: startup.RawHandlerKind(28)},
				}}},
			)
			contexts.EXPECT().IssueContext("wire").Return(extension.Context{}, nil)
			source := errors.New("observer source: unexpected EOF")
			action := Action{Kind: Acknowledge, Headers: nil, Body: nil}
			handlerErr := source
			if invalid {
				action.Kind = Replace
				handlerErr = nil
			}
			runtime.EXPECT().HandleWire(gomock.Any(), "wire", "a", gomock.Any()).Return(action, handlerErr)
			// Act on acquired provider metadata.
			err := service.NewWireCallbacks().
				Response(t.Context(), modelexecution.WireResponse{Status: 429, Headers: nil})
			// Assert complete failure text and skipped later observer by strict mock expectations.
			require.Error(t, err)
			require.Contains(t, err.Error(), "a")
			if !invalid {
				require.ErrorIs(t, err, source)
			}
		})
	}
}
