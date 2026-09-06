//go:build !integration

package programmatic

import (
	"sync"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/stretchr/testify/suite"
)

type ServiceSuite struct {
	suite.Suite
}

type selectionError struct {
	code SelectionCode
}

// Error returns the selection failure message used by service scenarios.
func (e selectionError) Error() string {
	return "safe selection failure: " + string(e.code)
}

// SelectionCode returns the typed selection failure code.
func (e selectionError) SelectionCode() string {
	return string(e.code)
}

// TestServiceSuite runs Programmatic service behavior scenarios.
func TestServiceSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ServiceSuite))
}

// testStateQuery supplies the activity requested by a Host query scenario.
func testStateQuery(t *testing.T, active bool) *MockStateQuery {
	t.Helper()
	query := NewMockStateQuery(gomock.NewController(t))
	query.EXPECT().RunActive().Return(active).AnyTimes()
	return query
}

// testRunOutput supplies generated output mocks with per-test reservation responses.
func testRunOutput(t *testing.T) *MockRunOutput {
	t.Helper()
	output := NewMockRunOutput(gomock.NewController(t))
	var mutex sync.Mutex
	var activeID string
	var activeRunID string
	output.EXPECT().ActiveOperation().DoAndReturn(func() string {
		mutex.Lock()
		defer mutex.Unlock()
		return activeID
	}).AnyTimes()
	output.EXPECT().Reserve(gomock.Any(), gomock.Any()).DoAndReturn(func(id, runID string) bool {
		mutex.Lock()
		defer mutex.Unlock()
		if activeID != "" {
			return false
		}
		activeID, activeRunID = id, runID
		return true
	}).AnyTimes()
	output.EXPECT().CancelPrepared(gomock.Any()).DoAndReturn(func(runID string) {
		mutex.Lock()
		defer mutex.Unlock()
		if activeRunID == runID {
			activeID, activeRunID = "", ""
		}
	}).AnyTimes()
	output.EXPECT().BindProgress(gomock.Any(), gomock.Any()).Return(func() {}).AnyTimes()
	return output
}
