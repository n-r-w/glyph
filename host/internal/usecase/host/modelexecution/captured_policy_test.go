//go:build !integration

package modelexecution

import (
	"context"
	"testing"

	"github.com/samber/mo"
	"go.uber.org/mock/gomock"
)

// newTestCapturedPolicies constructs the conversation policy dependency around the supplied captured owner.
func newTestCapturedPolicies(t *testing.T, captured *MockCapturedConversation) *MockConversationPolicies {
	t.Helper()
	policies := NewMockConversationPolicies(gomock.NewController(t))
	policies.EXPECT().CaptureConversation().Return(mo.Some[CapturedConversation](captured), nil).AnyTimes()
	captured.EXPECT().Observe(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	return policies
}

// newTestPreservingCapture supplies an owner that accepts ordinary history.
func newTestPreservingCapture(t *testing.T) *MockCapturedConversation {
	t.Helper()
	captured := NewMockCapturedConversation(gomock.NewController(t))
	captured.EXPECT().
		Prepare(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, input ProviderRequest) (ConversationPreparation, error) {
			return ConversationPreparation{History: input.History, Committed: false}, nil
		}).
		AnyTimes()
	return captured
}

// newTestPreservingPolicies admits an owner that accepts ordinary history and observation.
func newTestPreservingPolicies(t *testing.T) *MockConversationPolicies {
	t.Helper()
	return newTestCapturedPolicies(t, newTestPreservingCapture(t))
}
