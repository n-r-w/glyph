//go:build integration

package presentation

import (
	"testing"

	"go.uber.org/mock/gomock"
)

// newProjectionService isolates output while input-decoder integration exercises the real transition owner.
func newProjectionService(t *testing.T) *Service {
	t.Helper()
	controller := gomock.NewController(t)
	display := NewMockDisplay(controller)
	display.EXPECT().Publish(gomock.Any()).AnyTimes()
	service := New(NewMockHost(controller), display, NewMockRuntime(controller))
	service.model = newInteraction(newEvent(eventInitialization))
	return service
}
