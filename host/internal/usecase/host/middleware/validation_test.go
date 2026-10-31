//go:build !integration

package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// TestInvalidWireReplacementStopsLaterHandler validates each replacement before invoking its successor.
func TestInvalidWireReplacementStopsLaterHandler(t *testing.T) {
	t.Parallel()
	// Arrange one malformed reference replacement and a successor forbidden after validation failure.
	ctrl := gomock.NewController(t)
	runtime := NewMockRuntime(ctrl)
	contexts := NewMockContextIssuer(ctrl)
	service := New(runtime, contexts, nil)
	service.CommitMiddlewareHandlers(
		[]startup.AcceptedRegistration{{ID: "wire", Path: "", Tools: nil, Handlers: []startup.AcceptedHandler{
			{
				ID:   "bad",
				Kind: startup.RawHandlerKindProviderBody,
			},
			{ID: "later", Kind: startup.RawHandlerKindProviderBody},
		}}},
	)
	contexts.EXPECT().IssueContext("wire").Return(extension.Context{}, nil).AnyTimes()
	runtime.EXPECT().HandleWire(gomock.Any(), "wire", gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _, id string, _ Invocation) (Action, error) {
			require.Equal(t, "bad", id, "later handler must not see an invalid replacement")
			return Action{Kind: Replace, Headers: nil, Body: []byte(`{"$glyph_replay":"unknown"}`)}, nil
		},
	).AnyTimes()
	source := errors.New("unknown private replay reference")
	// Act with the neutral validation function provided by the owning serializer.
	_, err := service.NewWireCallbacks().Body(t.Context(), []byte(`{}`), func([]byte) error { return source })
	// Assert complete terminal validation cause and handler identity.
	require.ErrorIs(t, err, source)
	require.Contains(t, err.Error(), "bad")
}
