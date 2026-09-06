//go:build !integration

package programmatic

import (
	"testing"

	"go.uber.org/mock/gomock"
)

// testConnectionOutput isolates stream input from unsolicited output binding.
func testConnectionOutput(t *testing.T) *MockConnectionOutput {
	t.Helper()
	output := NewMockConnectionOutput(gomock.NewController(t))
	output.EXPECT().BindWriter(gomock.Any()).Return(func() {}).AnyTimes()
	return output
}
