//go:build !integration

package presentation

import (
	"testing"

	"go.uber.org/mock/gomock"
)

// displaySnapshot captures the real application publication without a terminal renderer.
func displaySnapshot(t testing.TB, service *Service) Snapshot {
	t.Helper()
	display := NewMockDisplay(gomock.NewController(t))
	var snapshot Snapshot
	display.EXPECT().Publish(gomock.Any()).Do(func(value Snapshot) { snapshot = value })
	previous := service.display
	service.display = display
	service.model.projectionChanged = true
	service.publish()
	service.display = previous
	return snapshot
}
