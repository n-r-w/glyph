//go:build !integration

package run

import (
	"context"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// passThroughFinalizer returns a finalizer whose final values equal the given values.
func passThroughFinalizer(t *testing.T) *MockMessageFinalizer {
	t.Helper()
	finalizer := NewMockMessageFinalizer(gomock.NewController(t))
	finalizer.EXPECT().FinalizeModelResponse(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, response model.Response) (model.Response, error) { return response, nil },
	).AnyTimes()
	finalizer.EXPECT().FinalizeToolResult(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, result agent.ToolResult) (agent.ToolResult, error) { return result, nil },
	).AnyTimes()
	return finalizer
}
