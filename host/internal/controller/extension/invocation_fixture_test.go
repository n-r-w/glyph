//go:build !integration

package extension

import (
	"context"
	"testing"

	"go.uber.org/mock/gomock"
)

// expectInvocationChild supplies generated child ownership for existing controller action tests.
func expectInvocationChild(t *testing.T, runtime *MockRuntimeOperations, mocks *gomock.Controller) {
	t.Helper()
	runtime.EXPECT().BeginInvocationChild("extension", "runtime", "").DoAndReturn(
		func(string, string, string) (InvocationChild, error) {
			child := NewMockInvocationChild(mocks)
			child.EXPECT().Link(gomock.Any()).DoAndReturn(func(ctx context.Context) (context.Context, func()) {
				return ctx, func() {}
			}).MaxTimes(1)
			child.EXPECT().Fail(gomock.Any()).MaxTimes(1)
			child.EXPECT().Release()
			return child, nil
		},
	).AnyTimes()
}
