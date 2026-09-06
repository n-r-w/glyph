//go:build !integration

package ui

import (
	"testing"

	"go.uber.org/mock/gomock"
)

// navigationFailure supplies a classified source failure for client error propagation tests.
func navigationFailure(t *testing.T, code string) error {
	t.Helper()
	failure := NewMockNavigationFailure(gomock.NewController(t))
	failure.EXPECT().NavigationCode().Return(code).AnyTimes()
	failure.EXPECT().Error().Return(code).AnyTimes()
	return failure
}
